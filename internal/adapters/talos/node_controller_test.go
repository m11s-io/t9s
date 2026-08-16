package talos

import (
	"context"
	"errors"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeNodeControlClient struct {
	rebootReq    machineapi.RebootRequest
	rebootErr    error
	shutdownReq  machineapi.ShutdownRequest
	shutdownErr  error
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
