package application

import (
	"context"
	"fmt"
	"github.com/blang/semver/v4"
	"strings"

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
func computeEtcdQuorumWarning(etcd EtcdState, targets []string) string {
	if etcd.Status != Ready && etcd.Status != Partial {
		return "control-plane node(s); etcd quorum impact unknown (etcd data unavailable)"
	}
	total := len(etcd.Value.Members)
	if total == 0 {
		return "control-plane node(s); etcd membership unknown"
	}
	atRisk := 0
	alreadyUnhealthy := 0
	for _, member := range etcd.Value.Members {
		isTarget := false
		for _, target := range targets {
			if member.Hostname == target {
				isTarget = true
				break
			}
		}
		if isTarget {
			atRisk++
			continue // don't also count this member as already-unhealthy below
		}
		// Same predicate as evaluateEtcdMemberUnhealthy (health.go): a
		// member already missing from quorum must reduce the "remaining"
		// count the same way an about-to-be-rebooted member does, or a
		// cluster with a pre-existing unhealthy member under-warns on the
		// action that would actually drop it below quorum.
		if !member.StatusKnown || len(member.Errors) > 0 {
			alreadyUnhealthy++
		}
	}
	remaining := total - atRisk - alreadyUnhealthy
	quorumFloor := total/2 + 1
	if remaining < quorumFloor {
		return fmt.Sprintf("control-plane node(s); would drop etcd to %d/%d — below quorum (need %d)", remaining, total, quorumFloor)
	}
	return "control-plane node(s)"
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
		if controller == nil {
			updates <- upgradeStreamResult{Err: fmt.Errorf("node controller is not configured"), Done: true}
			close(updates)
			return started
		}
		stream := controller.UpgradeStream(streamCtx, target, pending.Image)
		if stream == nil {
			updates <- upgradeStreamResult{Err: fmt.Errorf("upgrade stream is not configured"), Done: true}
			close(updates)
			return started
		}
		go forwardUpgradeStream(streamCtx, stream, updates, cancel)
		return started
	}
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
			update := upgradeStreamResult{Event: result.Event, Err: result.Err, Done: result.Done}
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
