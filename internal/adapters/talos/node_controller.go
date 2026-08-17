package talos

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
)

type nodeControlClient interface {
	Reboot(ctx context.Context, opts ...talosclient.RebootMode) error
	Shutdown(ctx context.Context, opts ...talosclient.ShutdownOption) error
	Rollback(ctx context.Context) error
	Upgrade(ctx context.Context, opts ...talosclient.UpgradeOption) error
	CurrentInstallImage(ctx context.Context) (string, error)
}

type machineryNodeControlClient struct{ client *talosclient.Client }

func (c machineryNodeControlClient) Reboot(ctx context.Context, opts ...talosclient.RebootMode) error {
	return c.client.Reboot(ctx, opts...)
}

func (c machineryNodeControlClient) Shutdown(ctx context.Context, opts ...talosclient.ShutdownOption) error {
	return c.client.Shutdown(ctx, opts...)
}

func (c machineryNodeControlClient) Rollback(ctx context.Context) error {
	return c.client.Rollback(ctx)
}

func (c machineryNodeControlClient) Upgrade(ctx context.Context, opts ...talosclient.UpgradeOption) error {
	_, err := c.client.UpgradeWithOptions(ctx, opts...)
	return err
}

func (c machineryNodeControlClient) CurrentInstallImage(ctx context.Context) (string, error) {
	cfg, err := safe.StateGet[*talosconfig.MachineConfig](
		ctx, c.client.COSI,
		resource.NewMetadata(talosconfig.NamespaceName, talosconfig.MachineConfigType, talosconfig.ActiveID, resource.VersionUndefined),
	)
	if err != nil {
		return "", err
	}
	return cfg.Provider().Machine().Install().Image(), nil
}

type nodeController struct{ client nodeControlClient }

func newNodeController(client nodeControlClient) ports.NodeController {
	return &nodeController{client: client}
}

func (c *nodeController) Reboot(ctx context.Context, target string, mode ports.RebootMode) error {
	var opts []talosclient.RebootMode
	if mode == ports.RebootPowercycle {
		opts = append(opts, talosclient.WithPowerCycle)
	}
	if err := c.client.Reboot(talosclient.WithNode(ctx, target), opts...); err != nil {
		return fmt.Errorf("reboot %s: %w", target, err)
	}
	return nil
}

func (c *nodeController) Shutdown(ctx context.Context, target string, force bool) error {
	var opts []talosclient.ShutdownOption
	if force {
		opts = append(opts, talosclient.WithShutdownForce(true))
	}
	if err := c.client.Shutdown(talosclient.WithNode(ctx, target), opts...); err != nil {
		return fmt.Errorf("shutdown %s: %w", target, err)
	}
	return nil
}

func (c *nodeController) Rollback(ctx context.Context, target string) error {
	if err := c.client.Rollback(talosclient.WithNode(ctx, target)); err != nil {
		return fmt.Errorf("rollback %s: %w", target, err)
	}
	return nil
}

func (c *nodeController) Upgrade(ctx context.Context, target, image string) error {
	if err := c.client.Upgrade(talosclient.WithNode(ctx, target), talosclient.WithUpgradeImage(image)); err != nil {
		return fmt.Errorf("upgrade %s: %w", target, err)
	}
	return nil
}

func (c *nodeController) CurrentInstallImage(ctx context.Context, target string) (string, error) {
	image, err := c.client.CurrentInstallImage(talosclient.WithNode(ctx, target))
	if err != nil {
		return "", fmt.Errorf("current install image %s: %w", target, err)
	}
	return image, nil
}
