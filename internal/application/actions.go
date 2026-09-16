package application

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/blang/semver/v4"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

func computeActionWarning(nodes []domain.NodeSnapshot, etcd EtcdState, targets []string) string {
	if !targetsIncludeControlPlane(nodes, targets) {
		return ""
	}
	return computeEtcdQuorumWarning(etcd, targets)
}

func targetsIncludeControlPlane(nodes []domain.NodeSnapshot, targets []string) bool {
	for _, target := range targets {
		for _, node := range nodes {
			if node.Target() != target {
				continue
			}
			if node.Role == domain.NodeRoleControl {
				return true
			}
			break
		}
	}
	return false
}

// computeEtcdQuorumWarning is shared by every action that reboots a
// control-plane node (Reboot, Shutdown, Rollback, Upgrade) and by service
// actions that stop or restart the etcd service directly.
// etcdQuorumAssessment is the shared result of evaluating how an action's
// targets affect etcd's voting membership. When known is false, reason
// explains why quorum could not be assessed from the current snapshot.
type etcdQuorumAssessment struct {
	known bool
	// voters is the number of non-learner etcd members.
	voters int
	// remaining counts every voter not positively known healthy as lost, and
	// drives the advisory warning.
	remaining int
	// confirmedRemaining counts only voters positively known healthy as
	// present; it drives the hard gate so a transient status read failure
	// cannot dead-end a possibly quorum-safe action.
	confirmedRemaining int
	floor              int
	reason             string
}

func (a etcdQuorumAssessment) belowQuorum() bool {
	return a.known && a.remaining < a.floor
}

// certainlyBelowQuorum is true only when the action would drop below quorum
// even in the best case where every unknown member is actually healthy. It is
// the hard-gate predicate; belowQuorum() remains the advisory.
func (a etcdQuorumAssessment) certainlyBelowQuorum() bool {
	return a.known && a.confirmedRemaining < a.floor
}

func assessEtcdQuorum(etcd EtcdState, targets []string) etcdQuorumAssessment {
	if etcd.Status != Ready && etcd.Status != Partial {
		return etcdQuorumAssessment{reason: "etcd quorum impact unknown (etcd data unavailable)"}
	}
	// Only voting members count toward quorum; learners never vote and must
	// be excluded from both the floor and the at-risk arithmetic.
	voters := 0
	for _, member := range etcd.Value.Members {
		if !member.IsLearner {
			voters++
		}
	}
	if voters == 0 {
		return etcdQuorumAssessment{reason: "etcd membership unknown"}
	}
	atRisk := 0
	confirmedUnhealthy := 0
	unknown := 0
	for _, member := range etcd.Value.Members {
		if member.IsLearner {
			continue
		}
		if memberMatchesAnyTarget(member, targets) {
			atRisk++
			continue // don't also count this member as already-unhealthy below
		}
		// A member positively reporting errors is confirmed unhealthy; a
		// member whose status read failed is merely unknown. Both reduce the
		// advisory "remaining" (same conservative predicate as
		// evaluateEtcdMemberUnhealthy in health.go), but only confirmed
		// unhealthy voters are allowed to hard-block an action.
		switch {
		case !member.StatusKnown:
			unknown++
		case len(member.Errors) > 0:
			confirmedUnhealthy++
		}
	}

	return etcdQuorumAssessment{
		known:              true,
		voters:             voters,
		remaining:          voters - atRisk - confirmedUnhealthy - unknown,
		confirmedRemaining: voters - atRisk - confirmedUnhealthy,
		floor:              voters/2 + 1,
	}
}

func computeEtcdQuorumWarning(etcd EtcdState, targets []string) string {
	assessment := assessEtcdQuorum(etcd, targets)
	if !assessment.known {
		return "control-plane node(s); " + assessment.reason
	}
	if assessment.belowQuorum() {
		return fmt.Sprintf("control-plane node(s); would drop etcd to %d/%d — below quorum (need %d)", assessment.remaining, assessment.voters, assessment.floor)
	}
	return "control-plane node(s)"
}

// pendingActionBlockReason re-evaluates the hard gate for an already-open
// node action against the current snapshot, so a prompt opened while the
// cluster was healthy is still refused if etcd degraded in the meantime.
func pendingActionBlockReason(model Model, pending PendingAction) string {
	if !targetsIncludeControlPlane(model.Nodes.Value.Nodes, pending.Targets) {
		return ""
	}

	return etcdQuorumBlockReason(model.Etcd, pending.Targets)
}

func pendingServiceActionBlockReason(model Model, pending PendingServiceAction) string {
	if pending.Service != "etcd" || pending.Kind == ServiceActionStart {
		return ""
	}

	return etcdQuorumBlockReason(model.Etcd, []string{pending.Node})
}

// etcdQuorumBlockReason is the hard gate. When non-empty the action must be
// refused outright, not merely confirmed behind an advisory warning.
func etcdQuorumBlockReason(etcd EtcdState, targets []string) string {
	assessment := assessEtcdQuorum(etcd, targets)
	if !assessment.certainlyBelowQuorum() {
		return ""
	}

	return fmt.Sprintf("refusing: would drop etcd to %d/%d (need %d)", assessment.confirmedRemaining, assessment.voters, assessment.floor)
}

// ValidateEtcdSnapshotPath rejects paths that cannot name a snapshot file:
// empty/whitespace, directory-like (trailing separator, "." or ".."), or
// the reserved ".part" suffix the adapter uses for its atomic write. It does
// not expand "~" — the shell does not do it for us — and it does not reject
// absolute paths, since backups legitimately target an operator-supplied
// location.
func ValidateEtcdSnapshotPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("snapshot path is required")
	}
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, string(filepath.Separator)) {
		return fmt.Errorf("snapshot path %q must name a file, not a directory", path)
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == ".." || strings.HasSuffix(cleaned, string(filepath.Separator)) {
		return fmt.Errorf("snapshot path %q must name a file", path)
	}
	switch filepath.Base(cleaned) {
	case "", ".", "..", ".part":
		return fmt.Errorf("snapshot path %q has an invalid file name", path)
	}
	return nil
}

// defaultEtcdSnapshotPath builds the prefilled destination for a snapshot. It
// is pure so the reducer stays clock-free and the shape is unit-testable with
// a fixed time.Time. Context and hostname are sanitized to [A-Za-z0-9._-] so
// a hostname containing a separator cannot escape the working directory.
func defaultEtcdSnapshotPath(contextName, memberHostname string, now time.Time) string {
	return fmt.Sprintf("etcd-%s-%s-%s.db", sanitizeSnapshotToken(contextName), sanitizeSnapshotToken(memberHostname), now.UTC().Format("20060102T150405Z"))
}

func sanitizeSnapshotToken(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	token := builder.String()
	for strings.Contains(token, "..") {
		token = strings.ReplaceAll(token, "..", ".")
	}
	if token == "" {
		return "-"
	}
	return token
}

// DefaultEtcdSnapshotPathForTest exposes defaultEtcdSnapshotPath for tests in
// package application_test, which cannot see unexported identifiers.
func DefaultEtcdSnapshotPathForTest(contextName, memberHostname string, now time.Time) string {
	return defaultEtcdSnapshotPath(contextName, memberHostname, now)
}

// memberMatchesAnyTarget reports whether an etcd member corresponds to any
// action target. Node targets are names or addresses (NodeSnapshot.Target()),
// while etcd members expose a hostname, a numeric member ID, and endpoint
// URLs, so all of those forms must be considered or control-plane impact goes
// uncounted.
func memberMatchesAnyTarget(member domain.EtcdMemberSnapshot, targets []string) bool {
	for _, target := range targets {
		if target == "" {
			continue
		}
		if member.Hostname == target {
			return true
		}
		if member.MemberID != 0 && strconv.FormatUint(member.MemberID, 10) == target {
			return true
		}
		for _, raw := range member.ClientURLs {
			if endpointHost(raw) == target {
				return true
			}
		}
		for _, raw := range member.PeerURLs {
			if endpointHost(raw) == target {
				return true
			}
		}
	}

	return false
}

func endpointHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	return parsed.Hostname()
}

// recoveryUncordonEffect makes one opportunistic, idempotent uncordon
// attempt. A failure is not surfaced to the operator: it is usually a
// transient blip, and the unchanged warning already says t9s will retry —
// the next Kubernetes node refresh (on a 30s heartbeat) tries again.
func recoveryUncordonEffect(controller ports.NodeController, target string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if controller == nil {
			return RecoveryUncordonFailed{Generation: generation, Target: target}
		}
		if err := controller.Uncordon(ctx, target); err != nil {
			return RecoveryUncordonFailed{Generation: generation, Target: target}
		}
		return RecoveryUncordonSucceeded{Generation: generation, Target: target}
	}
}

func actionEffect(controller ports.NodeController, pending PendingAction, target string, generation uint64) Effect {
	if pending.Kind == ActionUpgrade {
		return startUpgradeEffect(controller, pending, target, generation)
	}
	return func(ctx context.Context, _ Dependencies) Message {
		if controller == nil {
			return ActionFailed{Generation: generation, Target: target, Err: fmt.Errorf("node controller is not configured")}
		}
		var err error
		switch pending.Kind {
		case ActionReboot:
			err = controller.Reboot(ctx, target, ports.RebootDefault)
		case ActionShutdown:
			err = controller.Shutdown(ctx, target, false)
		case ActionRollback:
			err = controller.Rollback(ctx, target)
		case ActionUpgrade:
			err = fmt.Errorf("upgrade action did not use its stream bridge")
		default:
			err = fmt.Errorf("unsupported action %q", pending.Kind)
		}
		if err != nil {
			return ActionFailed{Generation: generation, Target: target, Err: err}
		}
		return ActionSucceeded{Generation: generation, Target: target}
	}
}

// BuildActionEffects returns one Effect per target in pending, meant to be
// run as independent Bubble Tea commands (tea.Batch) so a slow or
// unreachable target never blocks the rest of a bulk action. Call this
// with the Model as it existed before ConfirmPendingAction clears
// PendingAction — capture *model.PendingAction first, then call
// Update(model, ConfirmPendingAction{}) separately.
func BuildActionEffects(model Model, pending PendingAction) []Effect {
	if pending.Blocked != "" {
		return nil
	}
	if pending.Kind == ActionUpgrade {
		if len(pending.Targets) == 0 {
			return nil
		}
		return []Effect{actionEffect(model.nodeController, pending, pending.Targets[0], model.Generation)}
	}
	effects := make([]Effect, 0, len(pending.Targets))
	for _, target := range pending.Targets {
		effects = append(effects, actionEffect(model.nodeController, pending, target, model.Generation))
	}
	return effects
}
func UpgradeMinorWarning(running, image string) string {
	run, err := semver.Parse(strings.TrimPrefix(running, "v"))
	if err != nil {
		return ""
	}
	colon := strings.LastIndex(image, ":")
	if colon < 0 {
		return ""
	}
	target, err := semver.Parse(strings.TrimPrefix(image[colon+1:], "v"))
	if err != nil {
		return ""
	}
	if target.Major == run.Major && target.Minor > run.Minor+1 {
		return "skips intermediate Talos minor releases"
	}
	return ""
}
func UpgradeActionWarning(nodes []domain.NodeSnapshot, etcd EtcdState, targets []string, image string) string {
	warning := computeActionWarning(nodes, etcd, targets)
	if len(targets) == 0 {
		return warning
	}
	for _, node := range nodes {
		for _, target := range targets {
			if node.Target() == target {
				if minor := UpgradeMinorWarning(node.Version, image); minor != "" {
					if warning != "" {
						return warning + "; " + minor
					}
					return minor
				}
			}
		}
	}
	return warning
}

func startUpgradeEffect(controller ports.NodeController, pending PendingAction, target string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		streamCtx, cancel := context.WithCancel(ctx)
		updates := make(chan upgradeStreamResult, 1)
		started := UpgradeStarted{Generation: generation, Target: target, results: updates, cancel: cancel}
		if strings.TrimSpace(target) == "" {
			updates <- upgradeStreamResult{Err: fmt.Errorf("upgrade target is required"), Done: true}
			close(updates)
			return started
		}
		if strings.TrimSpace(pending.Image) == "" {
			updates <- upgradeStreamResult{Err: fmt.Errorf("upgrade image is required"), Done: true}
			close(updates)
			return started
		}
		go startUpgradeStream(streamCtx, controller, target, pending.Image, updates, cancel)
		return started
	}
}

func startUpgradeStream(ctx context.Context, controller ports.NodeController, target, image string, updates chan<- upgradeStreamResult, cancel context.CancelFunc) {
	if controller == nil {
		updates <- upgradeStreamResult{Err: fmt.Errorf("node controller is not configured"), Done: true}
		close(updates)
		cancel()
		return
	}
	stream := controller.UpgradeStream(ctx, target, image)
	if stream == nil {
		updates <- upgradeStreamResult{Err: fmt.Errorf("upgrade stream is not configured"), Done: true}
		close(updates)
		cancel()
		return
	}
	forwardUpgradeStream(ctx, stream, updates, cancel)
}

func forwardUpgradeStream(ctx context.Context, stream ports.UpgradeStream, updates chan<- upgradeStreamResult, cancel context.CancelFunc) {
	defer close(updates)
	defer cancel()
	defer stream.Cancel()
	for {
		select {
		case <-ctx.Done():
			sendUpgradeTerminal(updates, upgradeStreamResult{Err: ctx.Err(), Done: true})
			return
		case result, ok := <-stream.Results():
			if !ok {
				return
			}
			update := upgradeStreamResult{Event: result.Event, Err: result.Err, Outcome: result.Outcome, Warning: result.Warning, Done: result.Done}
			if update.Event == nil && update.Err == nil && !update.Done {
				update.Err = fmt.Errorf("upgrade stream returned an empty result")
				update.Done = true
			}
			select {
			case updates <- update:
			case <-ctx.Done():
				sendUpgradeTerminal(updates, upgradeStreamResult{Err: ctx.Err(), Done: true})
				return
			}
			if update.Done || update.Err != nil {
				return
			}
		}
	}
}

func readUpgradeUpdate(updates <-chan upgradeStreamResult, generation uint64, target string) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		select {
		case <-ctx.Done():
			return UpgradeFailed{Generation: generation, Target: target, Err: ctx.Err()}
		case update, ok := <-updates:
			if !ok {
				return UpgradeFailed{Generation: generation, Target: target, Err: fmt.Errorf("upgrade stream closed without a terminal result")}
			}
			if update.Err != nil {
				return UpgradeFailed{Generation: generation, Target: target, Err: update.Err}
			}
			if update.Done {
				if update.Outcome == ports.UpgradeOutcomeAppliedWithRecoveryWarning {
					return UpgradeAppliedWithRecoveryWarning{Generation: generation, Target: target, Warning: update.Warning}
				}
				return UpgradeSucceeded{Generation: generation, Target: target}
			}
			if update.Event == nil {
				return UpgradeFailed{Generation: generation, Target: target, Err: fmt.Errorf("upgrade stream returned an empty result")}
			}
			return UpgradeProgressed{Generation: generation, Target: target, Event: *update.Event}
		}
	}
}

func sendUpgradeTerminal(updates chan<- upgradeStreamResult, result upgradeStreamResult) {
	select {
	case updates <- result:
	default:
	}
}
