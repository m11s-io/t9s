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

// ResetPreview classifies a node's disks for the reset overlay. Known is
// false when the disks reader returned no inventory for the node, in which
// case the caller must never guess at the wipe scope.
type ResetPreview struct {
	Node       string
	SystemDisk string   // device name of the system disk, "" if none/unknown
	UserDisks  []string // non-system device names
	Known      bool
	Err        string
}

// BuildResetPreview classifies a disk set into system and user disks. It is
// pure so the classification is unit-testable and the reducer stays clock-free.
func BuildResetPreview(node string, set domain.DiskSet) ResetPreview {
	preview := ResetPreview{Node: node}
	if len(set.Disks) == 0 {
		return preview
	}
	preview.Known = true
	for _, disk := range set.Disks {
		if disk.SystemDisk {
			if preview.SystemDisk == "" {
				preview.SystemDisk = disk.DeviceName
			}
			continue
		}
		// Read-only devices (CD-ROMs, ISOs) cannot be wiped, and Talos
		// rejects the whole reset request if any listed user disk is
		// read-only, so never enumerate them as wipe targets.
		if disk.ReadOnly {
			continue
		}
		preview.UserDisks = append(preview.UserDisks, disk.DeviceName)
	}
	return preview
}

// ResetConfirmationToken is the exact string an operator must type to open a
// reset: the single target's name, or "wipe N nodes" for a bulk reset. The
// typed-intent step complements the gated (y/n) confirm that still
// re-evaluates quorum at confirm time.
func ResetConfirmationToken(targets []string) string {
	switch len(targets) {
	case 0:
		return ""
	case 1:
		return targets[0]
	default:
		return fmt.Sprintf("wipe %d nodes", len(targets))
	}
}

// ValidateResetConfirmation reports whether the typed token matches the
// target set. It rejects an empty target list outright.
func ValidateResetConfirmation(targets []string, typed string) error {
	if len(targets) == 0 {
		return fmt.Errorf("no reset targets")
	}
	token := ResetConfirmationToken(targets)
	if strings.TrimSpace(typed) != token {
		return fmt.Errorf("type %q to confirm this reset", token)
	}
	return nil
}

// resetQuorumBlockReason is the reset-specific hard gate. Unlike
// etcdQuorumBlockReason it also refuses a control-plane reset while the etcd
// snapshot is unavailable: a destructive wipe of a member whose quorum impact
// is unknown is never authorized. It reuses the shared assessEtcdQuorum
// arithmetic.
func resetQuorumBlockReason(nodes []domain.NodeSnapshot, etcd EtcdState, targets []string) string {
	if !targetsIncludeControlPlane(nodes, targets) {
		return ""
	}
	assessment := assessEtcdQuorum(etcd, targets)
	if !assessment.known {
		return "refusing: " + assessment.reason
	}
	// A reset is irreversible, so an unknown peer is treated as lost (the
	// pessimistic predicate), matching membership removal rather than the
	// optimistic gate used for reboots.
	if assessment.belowQuorum() {
		return fmt.Sprintf("refusing: would drop etcd to %d/%d — below quorum (need %d)", assessment.remaining, assessment.voters, assessment.floor)
	}
	return ""
}

// resetPreviewFor picks the disk preview for a reset from the currently loaded
// disks view. It returns nil when no target matches the loaded node, so the
// caller treats the inventory as unknown rather than guessing.
func resetPreviewFor(model Model, targets []string) *ResetPreview {
	if model.Disks.Status != Ready {
		return nil
	}
	for _, target := range targets {
		if model.Disks.Node == target {
			preview := BuildResetPreview(target, model.Disks.Value)
			return &preview
		}
	}
	return nil
}

// resetActionRisk computes the advisory warning and quorum hard block for a
// reset. Disk-scope blocking is separate (resetScope). The control-plane
// quorum gate applies to every reset of a control-plane target, graceful or
// not.
func resetActionRisk(nodes []domain.NodeSnapshot, etcd EtcdState, targets []string, options ports.ResetOptions) (string, string) {
	warning := computeActionWarning(nodes, etcd, targets)
	if options.Graceful {
		warning = appendWarning(warning, "node(s) leave etcd before reset")
	}
	return warning, resetQuorumBlockReason(nodes, etcd, targets)
}

// resetScope resolves the user disks a reset will actually erase and reports
// why the selected mode cannot be honored. Talos never auto-enumerates user
// disks: it wipes only those listed in UserDisksToWipe, so t9s must list them
// or the erase silently does nothing. Enumeration is per node, so the modes
// that erase user disks are single-target only.
func resetScope(targets []string, options ports.ResetOptions, preview *ResetPreview) ([]string, string) {
	switch options.Mode {
	case ports.WipeModeSystemDisk:
		// System partitions are wiped without a user-disk list.
		return nil, ""
	case ports.WipeModeUserDisks:
		if len(targets) != 1 {
			return nil, "refusing: user-disk wipe is single-node only"
		}
		if preview == nil || !preview.Known {
			return nil, "refusing: user disk inventory unknown"
		}
		if len(preview.UserDisks) == 0 {
			return nil, "refusing: no user disks discovered"
		}
		return append([]string(nil), preview.UserDisks...), ""
	default: // WipeModeAll
		if len(targets) != 1 {
			return nil, "refusing: all-disk wipe is single-node only"
		}
		if preview == nil || !preview.Known {
			return nil, "refusing: user disk inventory unknown"
		}
		return append([]string(nil), preview.UserDisks...), ""
	}
}

// resetModeBlockReason re-derives the disk-scope block for an already-open
// pending reset, so a confirm re-evaluates it alongside quorum.
func resetModeBlockReason(targets []string, options ports.ResetOptions, preview *ResetPreview) string {
	_, block := resetScope(targets, options, preview)
	return block
}

// NormalizeDeviceToken trims surrounding whitespace and a leading /dev/ so a
// typed confirmation matches the device whatever form the operator or the
// inventory uses.
func NormalizeDeviceToken(device string) string {
	return strings.TrimPrefix(strings.TrimSpace(device), "/dev/")
}

// ValidateDeviceWipeConfirmation reports whether the typed token names the
// selected device, tolerating the optional /dev/ prefix on either side.
func ValidateDeviceWipeConfirmation(device, typed string) error {
	if NormalizeDeviceToken(device) == "" {
		return fmt.Errorf("no device selected")
	}
	if NormalizeDeviceToken(typed) != NormalizeDeviceToken(device) {
		return fmt.Errorf("type %q to confirm wiping this device", device)
	}
	return nil
}

// DeviceWipeBlockReason is the hard gate for a single-device BlockDeviceWipe.
// It refuses the system disk, read-only devices, and any device that cannot be
// verified against the loaded inventory — a wipe is irreversible, so an
// unknown target is never authorized.
func DeviceWipeBlockReason(disks DisksState, wipe ports.DeviceWipeOptions) string {
	if wipe.Node == "" || wipe.Device == "" {
		return "refusing: no device selected"
	}
	if disks.Status != Ready || disks.Node != wipe.Node {
		return fmt.Sprintf("refusing: disk inventory for %s is not loaded", wipe.Node)
	}
	for _, disk := range disks.Value.Disks {
		if disk.DeviceName != wipe.Device {
			continue
		}
		if disk.SystemDisk {
			return fmt.Sprintf("refusing: %s is the system disk", wipe.Device)
		}
		if disk.ReadOnly {
			return fmt.Sprintf("refusing: %s is read-only", wipe.Device)
		}
		return ""
	}
	return fmt.Sprintf("refusing: %s is not in the current inventory", wipe.Device)
}

// resetPreviewFrom picks the disk preview for a reset: the one the overlay
// loaded when present, otherwise the currently loaded :disks view. Only a
// single target can be previewed.
func resetPreviewFrom(model Model, targets []string, previews map[string]ResetPreview) *ResetPreview {
	if len(targets) != 1 {
		return nil
	}
	if previews != nil {
		if preview, ok := previews[targets[0]]; ok {
			copied := preview
			return &copied
		}
	}
	return resetPreviewFor(model, targets)
}

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
	if pending.Kind == ActionReset {
		if pending.Reset != nil {
			if block := resetModeBlockReason(pending.Targets, *pending.Reset, pending.ResetPreview); block != "" {
				return block
			}
		}
		return resetQuorumBlockReason(model.Nodes.Value.Nodes, model.Etcd, pending.Targets)
	}
	if pending.Kind == ActionWipeDevice {
		// Re-evaluate against the current inventory: a device that became the
		// system disk or vanished after the prompt opened must be refused. This
		// is deliberately not quorum-gated: wiping a data disk never drops etcd.
		if pending.DeviceWipe == nil {
			return "refusing: no device selected"
		}
		return DeviceWipeBlockReason(model.Disks, *pending.DeviceWipe)
	}
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

// etcdLeadershipWarning is the advisory for a leadership forfeit. A forfeit is
// quorum-neutral — the member keeps voting — so it is never hard-blocked; the
// only risk it warns about is that no healthy follower is known to take the
// leadership it is giving up. A target that is not the current leader (or whose
// status is unknown) has nothing to forfeit and never warns.
func etcdLeadershipWarning(etcd EtcdState, memberHostname string) string {
	if etcd.Status != Ready && etcd.Status != Partial {
		return ""
	}
	targetIsLeader := false
	for _, member := range etcd.Value.Members {
		if member.Hostname == memberHostname && member.StatusKnown && member.IsLeader {
			targetIsLeader = true
			break
		}
	}
	if !targetIsLeader {
		return ""
	}
	for _, member := range etcd.Value.Members {
		if member.IsLearner || member.Hostname == memberHostname {
			continue
		}
		if member.StatusKnown && len(member.Errors) == 0 {
			return ""
		}
	}
	return "no healthy follower is known to take leadership"
}

// isDestructiveEtcdMembership reports whether a kind changes cluster
// membership and therefore requires a snapshot of the target first.
func isDestructiveEtcdMembership(kind EtcdActionKind) bool {
	return kind == EtcdActionRemoveMember || kind == EtcdActionLeaveCluster
}

// etcdMembershipAssessment classifies a single-member removal/leave against
// the current snapshot. Unlike assessEtcdQuorum (which takes node-name
// targets), it resolves the member by numeric ID and/or hostname.
type etcdMembershipAssessment struct {
	found   bool
	voter   bool
	healthy bool
	// target is the matched member's own hostname (or decimal ID), so the
	// quorum assessment counts the member the caller actually matched rather
	// than an echoed caller string.
	target string
	reason string
}

// assessEtcdMembership locates the member being changed. found is false when
// etcd data is unavailable (Loading/Failed/Idle) or the member is absent from
// the last snapshot; either way the caller cannot compute impact.
func assessEtcdMembership(etcd EtcdState, memberID uint64, memberHostname string) etcdMembershipAssessment {
	if etcd.Status != Ready && etcd.Status != Partial {
		return etcdMembershipAssessment{reason: "etcd membership unknown (etcd data unavailable)"}
	}
	for _, member := range etcd.Value.Members {
		if memberMatchesMember(member, memberID, memberHostname) {
			return etcdMembershipAssessment{
				found:   true,
				voter:   !member.IsLearner,
				healthy: member.StatusKnown && len(member.Errors) == 0,
				target:  membershipTargetForMember(member),
			}
		}
	}

	return etcdMembershipAssessment{reason: "member is not in the current etcd snapshot"}
}

func memberMatchesMember(member domain.EtcdMemberSnapshot, memberID uint64, memberHostname string) bool {
	if memberHostname != "" && member.Hostname == memberHostname {
		return true
	}

	return memberID != 0 && member.MemberID == memberID
}

// membershipTargetForMember is the target string fed to assessEtcdQuorum for a
// matched member: its hostname when present, otherwise the decimal member ID
// (matching memberMatchesAnyTarget's numeric comparison).
func membershipTargetForMember(member domain.EtcdMemberSnapshot) string {
	if strings.TrimSpace(member.Hostname) != "" {
		return member.Hostname
	}
	if member.MemberID != 0 {
		return strconv.FormatUint(member.MemberID, 10)
	}

	return ""
}

// etcdMembershipBlockReason is the membership-specific hard gate. It composes
// the quorum assessment with membership facts the generic gate cannot see:
// an unknown snapshot, an absent member, and a forced Remove of a member that
// is still healthy (which must go through graceful leave instead). Learners
// do not vote, so removing one is never quorum-blocked.
func etcdMembershipBlockReason(etcd EtcdState, kind EtcdActionKind, memberID uint64, memberHostname string) string {
	assessment := assessEtcdMembership(etcd, memberID, memberHostname)
	if !assessment.found {
		return "refusing: " + assessment.reason
	}
	if kind == EtcdActionRemoveMember && assessment.healthy {
		return "refusing: member is healthy — use leave (L)"
	}
	if assessment.target == "" {
		return "refusing: cannot identify the member to change"
	}
	quorum := assessEtcdQuorum(etcd, []string{assessment.target})
	if !quorum.known {
		return "refusing: etcd quorum impact unknown"
	}
	// Irreversible membership surgery uses the pessimistic predicate: an
	// unknown peer is treated as lost, not as optimistically healthy. A read
	// blip must not authorize a removal that could strand the cluster.
	if quorum.belowQuorum() {
		return fmt.Sprintf("refusing: would drop etcd to %d/%d — below quorum (need %d)", quorum.remaining, quorum.voters, quorum.floor)
	}

	return ""
}

// etcdMembershipWarning is the advisory text for a membership change that is
// allowed but leaves no fault tolerance. A learner target is quorum-neutral
// and never warns. The block reason is authoritative; this is display only.
func etcdMembershipWarning(etcd EtcdState, _ EtcdActionKind, memberID uint64, memberHostname string) string {
	assessment := assessEtcdMembership(etcd, memberID, memberHostname)
	if !assessment.found || !assessment.voter {
		return ""
	}
	if assessment.target == "" {
		return ""
	}
	quorum := assessEtcdQuorum(etcd, []string{assessment.target})
	if !quorum.known {
		return quorum.reason
	}
	if quorum.remaining <= quorum.floor {
		return fmt.Sprintf("would drop etcd to %d/%d — no fault tolerance (need %d)", quorum.remaining, quorum.voters, quorum.floor)
	}

	return ""
}

// etcdSnapshotNodeFor picks a source member for the mandatory pre-removal
// snapshot: a healthy voter other than the target, so the snapshot is taken
// from a live node whenever one exists. When no other healthy voter is known
// it falls back to the target's own hostname (a local snapshot is still
// valid; the caller surfaces the degraded source in the prompt).
func etcdSnapshotNodeFor(etcd EtcdState, memberID uint64, memberHostname string) string {
	if etcd.Status == Ready || etcd.Status == Partial {
		for _, member := range etcd.Value.Members {
			if member.IsLearner || member.Hostname == "" {
				continue
			}
			if memberMatchesMember(member, memberID, memberHostname) {
				continue
			}
			if member.StatusKnown && len(member.Errors) == 0 {
				return member.Hostname
			}
		}
	}

	return memberHostname
}

// firstOtherControlPlaneHostname returns a control-plane hostname other than
// exclude, used to address a forced removal when the snapshot source is the
// (possibly dead) target itself.
func firstOtherControlPlaneHostname(nodes []domain.NodeSnapshot, exclude string) string {
	for _, hostname := range controlPlaneHostnames(nodes) {
		if hostname != exclude {
			return hostname
		}
	}

	return ""
}

// appendWarning joins a new advisory onto an existing one.
func appendWarning(existing, extra string) string {
	if existing == "" {
		return extra
	}

	return existing + "; " + extra
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
		case ActionReset:
			if pending.Reset == nil {
				err = fmt.Errorf("reset options are not configured")
			} else {
				err = controller.Reset(ctx, target, *pending.Reset)
			}
		case ActionWipeDevice:
			if pending.DeviceWipe == nil {
				err = fmt.Errorf("device wipe options are not configured")
			} else {
				err = controller.WipeDevice(ctx, pending.DeviceWipe.Node, pending.DeviceWipe.Device, pending.DeviceWipe.Method)
			}
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
