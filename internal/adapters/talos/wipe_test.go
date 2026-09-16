package talos

import (
	"errors"
	"testing"

	"github.com/m11s-io/t9s/internal/ports"
	storageapi "github.com/siderolabs/talos/pkg/machinery/api/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Talos server resolves BlockDeviceWipeDescriptor.Device via
// safe.StateGetByID[*block.Device] and then joins it to /dev/<id> (see
// internal/app/storaged/server.go), so the request must carry the bare device
// ID with no /dev/ prefix even though the disk inventory reports /dev/<id>.
func TestNodeControllerWipeDeviceFastDefault(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.WipeDevice(t.Context(), "worker-1", "/dev/sdb", ports.DeviceWipeFast)

	require.NoError(t, err)
	require.NotNil(t, client.wipeDeviceReq)
	require.Len(t, client.wipeDeviceReq.Devices, 1)
	desc := client.wipeDeviceReq.Devices[0]
	assert.Equal(t, "sdb", desc.Device, "the server expects the bare device ID without /dev/")
	assert.Equal(t, storageapi.BlockDeviceWipeDescriptor_FAST, desc.Method)
	assert.False(t, desc.SkipVolumeCheck, "the server must be allowed to refuse an in-use device")
}

func TestNodeControllerWipeDeviceZeroesAndStripsDevPrefix(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.WipeDevice(t.Context(), "worker-1", "/dev/sda5", ports.DeviceWipeZeroes)

	require.NoError(t, err)
	require.NotNil(t, client.wipeDeviceReq)
	require.Len(t, client.wipeDeviceReq.Devices, 1)
	desc := client.wipeDeviceReq.Devices[0]
	assert.Equal(t, "sda5", desc.Device)
	assert.Equal(t, storageapi.BlockDeviceWipeDescriptor_ZEROES, desc.Method)
}

func TestNodeControllerWipeDeviceWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{wipeDeviceErr: errors.New("block device is mounted or in use")}
	controller := newNodeController(client)

	err := controller.WipeDevice(t.Context(), "worker-1", "/dev/sdb", ports.DeviceWipeFast)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sdb")
	assert.Contains(t, err.Error(), "worker-1")
	assert.Contains(t, err.Error(), "block device is mounted or in use")
}
