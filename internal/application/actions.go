package application

import (
	"context"
	"fmt"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

func computeActionWarning(nodes []domain.NodeSnapshot, etcd EtcdState, targets []string) string {
	controlPlane := false
	for _, target := range targets {
		for _, node := range nodes {
			if node.Target() != target {
				continue
			}
			if node.Role == domain.NodeRoleControl {
				controlPlane = true
			}
			break
		}
	}
	if !controlPlane {
		return ""
	}
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

func actionEffect(controller ports.NodeController, kind ActionKind, target string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if controller == nil {
			return ActionFailed{Generation: generation, Target: target, Err: fmt.Errorf("node controller is not configured")}
		}
		var err error
		switch kind {
		case ActionReboot:
			err = controller.Reboot(ctx, target, ports.RebootDefault)
		case ActionShutdown:
			err = controller.Shutdown(ctx, target, false)
		default:
			err = fmt.Errorf("unsupported action %q", kind)
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
	effects := make([]Effect, 0, len(pending.Targets))
	for _, target := range pending.Targets {
		effects = append(effects, actionEffect(model.nodeController, pending.Kind, target, model.Generation))
	}
	return effects
}
