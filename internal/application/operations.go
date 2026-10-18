package application

import "context"

func startsOperation(message Message) bool {
	switch message.(type) {
	case ConfirmPendingAction, ConfirmEtcdAction, ConfirmEtcdSnapshotPrompt:
		return true
	}
	return false
}

// Every accepted mutation owns its result identity and session context. A new
// prompt can replace its display, but cannot misattribute a late completion.
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
