package talos

import (
	"errors"
	"testing"

	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResetRequestForAllModeDefaults(t *testing.T) {
	req := resetRequestFor(ports.ResetOptions{Mode: ports.WipeModeAll, Graceful: true, Reboot: true})

	require.NotNil(t, req)
	assert.Equal(t, machineapi.ResetRequest_ALL, req.Mode)
	assert.True(t, req.Graceful)
	assert.True(t, req.Reboot)
	assert.Empty(t, req.SystemPartitionsToWipe)
	assert.Empty(t, req.UserDisksToWipe)
}

func TestResetRequestForUserDisksMapsModeAndDevices(t *testing.T) {
	req := resetRequestFor(ports.ResetOptions{Mode: ports.WipeModeUserDisks, UserDisks: []string{"sdb", "sdc"}})

	require.NotNil(t, req)
	assert.Equal(t, machineapi.ResetRequest_USER_DISKS, req.Mode)
	assert.Equal(t, []string{"sdb", "sdc"}, req.UserDisksToWipe)
	assert.Empty(t, req.SystemPartitionsToWipe)
}

func TestResetRequestForSystemPartitionsBuildsWipeSpecs(t *testing.T) {
	req := resetRequestFor(ports.ResetOptions{Mode: ports.WipeModeSystemDisk, SystemPartitions: []string{"EFI", "BOOT"}})

	require.NotNil(t, req)
	assert.Equal(t, machineapi.ResetRequest_SYSTEM_DISK, req.Mode)
	require.Len(t, req.SystemPartitionsToWipe, 2)
	assert.Equal(t, &machineapi.ResetPartitionSpec{Label: "EFI", Wipe: true}, req.SystemPartitionsToWipe[0])
	assert.Equal(t, &machineapi.ResetPartitionSpec{Label: "BOOT", Wipe: true}, req.SystemPartitionsToWipe[1])
}

func TestNodeControllerResetSendsRequestAndWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Reset(t.Context(), "cp-1", ports.ResetOptions{Mode: ports.WipeModeAll, Graceful: true, Reboot: true})

	require.NoError(t, err)
	assert.Equal(t, machineapi.ResetRequest_ALL, client.resetReq.Mode)
	assert.True(t, client.resetReq.Graceful)
	assert.True(t, client.resetReq.Reboot)

	failing := &fakeNodeControlClient{resetErr: errors.New("unreachable")}
	err = newNodeController(failing).Reset(t.Context(), "cp-1", ports.ResetOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reset cp-1")
	assert.Contains(t, err.Error(), "unreachable")
}
