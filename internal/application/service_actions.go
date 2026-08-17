package application

import (
	"context"
	"fmt"
)

// BuildServiceActionEffect returns the single Effect for a confirmed service
// action. Unlike node actions, service actions are never bulk — there is at
// most one PendingServiceAction at a time — so this returns one Effect, not
// a slice, and reuses ActionSucceeded/ActionFailed with a synthesized
// "service@node" target, matching the label format services.go/logs.go
// already use for the same node+service pairing.
func BuildServiceActionEffect(model Model, pending PendingServiceAction) Effect {
	target := pending.Service + "@" + pending.Node
	generation := model.Generation
	controller := model.serviceController
	return func(ctx context.Context, _ Dependencies) Message {
		if controller == nil {
			return ActionFailed{Generation: generation, Target: target, Err: fmt.Errorf("service controller is not configured")}
		}
		var err error
		switch pending.Kind {
		case ServiceActionStart:
			err = controller.Start(ctx, pending.Node, pending.Service)
		case ServiceActionStop:
			err = controller.Stop(ctx, pending.Node, pending.Service)
		case ServiceActionRestart:
			err = controller.Restart(ctx, pending.Node, pending.Service)
		default:
			err = fmt.Errorf("unsupported service action %q", pending.Kind)
		}
		if err != nil {
			return ActionFailed{Generation: generation, Target: target, Err: err}
		}
		return ActionSucceeded{Generation: generation, Target: target}
	}
}
