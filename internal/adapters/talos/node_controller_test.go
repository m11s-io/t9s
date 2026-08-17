package talos

import (
	"context"
	"errors"
	"testing"

	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeNodeControlClient struct {
	rebootReq       machineapi.RebootRequest
	rebootErr       error
	shutdownReq     machineapi.ShutdownRequest
	shutdownErr     error
	rollbackErr     error
	upgradeReq      talosclient.UpgradeOptions
	upgradeErr      error
	currentImage    string
	currentImageErr error
}

func (c *fakeNodeControlClient) Reboot(ctx context.Context, opts ...talosclient.RebootMode) error {
	for _, opt := range opts {
		opt(&c.rebootReq)
	}
	return c.rebootErr
}

func (c *fakeNodeControlClient) Shutdown(ctx context.Context, opts ...talosclient.ShutdownOption) error {
	for _, opt := range opts {
		opt(&c.shutdownReq)
	}
	return c.shutdownErr
}

func (c *fakeNodeControlClient) Rollback(ctx context.Context) error {
	return c.rollbackErr
}

func (c *fakeNodeControlClient) Upgrade(ctx context.Context, opts ...talosclient.UpgradeOption) error {
	for _, opt := range opts {
		opt(&c.upgradeReq)
	}
	return c.upgradeErr
}

func (c *fakeNodeControlClient) CurrentInstallImage(ctx context.Context) (string, error) {
	return c.currentImage, c.currentImageErr
}

func TestNodeControllerRebootDefaultModeSendsNoModeOverride(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Reboot(t.Context(), "cp-1", ports.RebootDefault)

	require.NoError(t, err)
	assert.Equal(t, machineapi.RebootRequest_DEFAULT, client.rebootReq.Mode)
}

func TestNodeControllerRebootPowercycleSetsMode(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Reboot(t.Context(), "cp-1", ports.RebootPowercycle)

	require.NoError(t, err)
	assert.Equal(t, machineapi.RebootRequest_POWERCYCLE, client.rebootReq.Mode)
}

func TestNodeControllerRebootWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{rebootErr: errors.New("unreachable")}
	controller := newNodeController(client)

	err := controller.Reboot(t.Context(), "cp-1", ports.RebootDefault)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
	assert.Contains(t, err.Error(), "unreachable")
}

func TestNodeControllerShutdownForceFalseLeavesForceUnset(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Shutdown(t.Context(), "cp-1", false)

	require.NoError(t, err)
	assert.False(t, client.shutdownReq.Force)
}

func TestNodeControllerShutdownForceTrueSetsForce(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Shutdown(t.Context(), "cp-1", true)

	require.NoError(t, err)
	assert.True(t, client.shutdownReq.Force)
}

func TestNodeControllerShutdownWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{shutdownErr: errors.New("unreachable")}
	controller := newNodeController(client)

	err := controller.Shutdown(t.Context(), "cp-1", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
}

func TestNodeControllerRollbackWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{rollbackErr: errors.New("no previous version")}
	controller := newNodeController(client)

	err := controller.Rollback(t.Context(), "cp-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
	assert.Contains(t, err.Error(), "no previous version")
}

func TestNodeControllerRollbackSucceeds(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Rollback(t.Context(), "cp-1")

	require.NoError(t, err)
}

func TestNodeControllerUpgradeSendsImage(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Upgrade(t.Context(), "cp-1", "ghcr.io/siderolabs/installer:v1.13.3")

	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.3", client.upgradeReq.Request.Image)
}

func TestNodeControllerUpgradeWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{upgradeErr: errors.New("incompatible image")}
	controller := newNodeController(client)

	err := controller.Upgrade(t.Context(), "cp-1", "bad:image")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
	assert.Contains(t, err.Error(), "incompatible image")
}

func TestNodeControllerCurrentInstallImageReturnsImage(t *testing.T) {
	client := &fakeNodeControlClient{currentImage: "ghcr.io/siderolabs/installer:v1.13.2"}
	controller := newNodeController(client)

	image, err := controller.CurrentInstallImage(t.Context(), "cp-1")

	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.2", image)
}

func TestNodeControllerCurrentInstallImageWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{currentImageErr: errors.New("resource not found")}
	controller := newNodeController(client)

	_, err := controller.CurrentInstallImage(t.Context(), "cp-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
}
