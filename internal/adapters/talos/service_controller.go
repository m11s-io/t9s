package talos

import (
	"context"
	"fmt"

	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

type serviceControlClient interface {
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error
}

type machineryServiceControlClient struct{ client *talosclient.Client }

func (c machineryServiceControlClient) Start(ctx context.Context, id string) error {
	_, err := c.client.ServiceStart(ctx, id)
	return err
}

func (c machineryServiceControlClient) Stop(ctx context.Context, id string) error {
	_, err := c.client.ServiceStop(ctx, id)
	return err
}

func (c machineryServiceControlClient) Restart(ctx context.Context, id string) error {
	_, err := c.client.ServiceRestart(ctx, id)
	return err
}

type serviceController struct{ client serviceControlClient }

func newServiceController(client serviceControlClient) ports.ServiceController {
	return &serviceController{client: client}
}

func (c *serviceController) Start(ctx context.Context, node, service string) error {
	if err := c.client.Start(talosclient.WithNode(ctx, node), service); err != nil {
		return fmt.Errorf("start %s@%s: %w", service, node, err)
	}
	return nil
}

func (c *serviceController) Stop(ctx context.Context, node, service string) error {
	if err := c.client.Stop(talosclient.WithNode(ctx, node), service); err != nil {
		return fmt.Errorf("stop %s@%s: %w", service, node, err)
	}
	return nil
}

func (c *serviceController) Restart(ctx context.Context, node, service string) error {
	if err := c.client.Restart(talosclient.WithNode(ctx, node), service); err != nil {
		return fmt.Errorf("restart %s@%s: %w", service, node, err)
	}
	return nil
}
