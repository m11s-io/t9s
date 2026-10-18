package application

import (
	"context"
	"fmt"
)

func buildServiceActionEffect(model Model, pending PendingServiceAction) Effect {
	if pending.Blocked != "" {
		return nil
	}
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
