package application

import (
	"context"
	"maps"
)

// MutationActive reports accepted work, independently of any confirmation UI.
// Read-only navigation remains available while that work is running.
func (m Model) MutationActive() bool {
	return len(m.operationTargets) != 0 || m.Upgrade.Active
}

func changesMutationPrompt(message Message) bool {
	switch message.(type) {
	case RequestAction, RequestServiceAction, RequestEtcdAction,
		RequestEtcdSnapshotPrompt, RequestUpgradePrompt, RequestResetPrompt,
		ConfirmPendingAction, ConfirmEtcdAction, ConfirmEtcdSnapshotPrompt,
		CancelPendingAction, CancelEtcdSnapshotPrompt:
		return true
	}
	return false
}

func acceptedOperationTargets(model Model, message Message) map[string]int {
	targets := make(map[string]int)
	if _, action := message.(ConfirmPendingAction); action {
		if model.PendingAction != nil {
			for _, target := range model.PendingAction.Targets {
				targets[target]++
			}
		} else if pending := model.PendingServiceAction; pending != nil {
			targets[pending.Service+"@"+pending.Node] = 1
		}
	} else {
		// Etcd and snapshot operations have one terminal outcome. The backup
		// stage of membership surgery is intermediate, not a completed target.
		targets[""] = 1
	}
	return targets
}

func finishOperationTarget(model Model, message Message) Model {
	if len(model.operationTargets) == 0 {
		return model
	}
	var generation uint64
	var target string
	switch m := message.(type) {
	case ActionSucceeded:
		generation, target = m.Generation, m.Target
	case ActionFailed:
		generation, target = m.Generation, m.Target
	case EtcdSnapshotSucceeded:
		if model.PendingEtcdAction != nil && model.PendingEtcdAction.Stage == EtcdStageOperation {
			return model
		}
		generation = m.Generation
	case EtcdSnapshotFailed:
		generation = m.Generation
	case EtcdActionSucceeded:
		generation = m.Generation
	case EtcdActionFailed:
		generation = m.Generation
	case UpgradeSucceeded:
		generation, target = m.Generation, m.Target
	case UpgradeFailed:
		generation, target = m.Generation, m.Target
	case UpgradeAppliedWithRecoveryWarning:
		generation, target = m.Generation, m.Target
	default:
		return model
	}
	if generation == model.Generation {
		model.operationTargets = maps.Clone(model.operationTargets)
		if model.operationTargets[target] > 1 {
			model.operationTargets[target]--
		} else {
			delete(model.operationTargets, target)
		}
	}
	return model
}

func startsOperation(message Message) bool {
	switch message.(type) {
	case ConfirmPendingAction, ConfirmEtcdAction, ConfirmEtcdSnapshotPrompt:
		return true
	}
	return false
}

// Every accepted mutation owns its result identity and session context.
// Conflicting prompts are blocked until its terminal results have arrived.
func operationEffect(model Model, effect Effect) Effect {
	if effect == nil {
		return nil
	}
	id, owner := model.operationID, model.requests
	return func(ctx context.Context, deps Dependencies) Message {
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(owner.session, cancel)
		if owner.session.Err() != nil {
			cancel()
		}
		if ctx.Err() != nil {
			stop()
			cancel()
			return nil
		}
		message := effect(ctx, deps)
		if effects, ok := message.(EffectBatch); ok {
			stop()
			cancel()
			for i, effect := range effects {
				effects[i] = operationEffect(model, effect)
			}
			return effects
		}
		if started, ok := message.(UpgradeStarted); ok {
			original := started.cancel
			started.cancel = func() {
				stop()
				cancel()
				if original != nil {
					original()
				}
			}
			message = started
		} else {
			stop()
			cancel()
		}
		message, _ = identifyOperation(message, id)
		return message
	}
}

func identifyOperation(message Message, stamp uint64) (Message, uint64) {
	switch m := message.(type) {
	case ActionSucceeded:
		id := stampResultID(&m.OperationID, stamp)
		return m, id
	case ActionFailed:
		id := stampResultID(&m.OperationID, stamp)
		return m, id
	case EtcdSnapshotSucceeded:
		id := stampResultID(&m.OperationID, stamp)
		return m, id
	case EtcdSnapshotFailed:
		id := stampResultID(&m.OperationID, stamp)
		return m, id
	case EtcdActionSucceeded:
		id := stampResultID(&m.OperationID, stamp)
		return m, id
	case EtcdActionFailed:
		id := stampResultID(&m.OperationID, stamp)
		return m, id
	case UpgradeStarted:
		id := stampResultID(&m.OperationID, stamp)
		return m, id
	case UpgradeProgressed:
		id := stampResultID(&m.OperationID, stamp)
		return m, id
	case UpgradeSucceeded:
		id := stampResultID(&m.OperationID, stamp)
		return m, id
	case UpgradeFailed:
		id := stampResultID(&m.OperationID, stamp)
		return m, id
	case UpgradeAppliedWithRecoveryWarning:
		id := stampResultID(&m.OperationID, stamp)
		return m, id
	}
	return message, 0
}
