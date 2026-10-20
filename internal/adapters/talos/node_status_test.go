package talos

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeNodeStatusClient struct {
	peers   []*network.BGPPeerStatus
	peerErr error
	pvs     []*storage.LVMPhysicalVolumeStatus
	groups  []*storage.LVMVolumeGroupStatus
	logical []*storage.LVMLogicalVolumeStatus
	arrays  []*storage.MDArrayStatus
	volumes []*block.VolumeStatus
	trims   []*block.VolumeTrimSchedule
	scrubs  []*block.FSScrubStatus
	disk    *block.SystemDisk
	config  *talosconfig.MachineConfig
}

func (f fakeNodeStatusClient) BGPPeers(context.Context, string) ([]*network.BGPPeerStatus, error) {
	return f.peers, f.peerErr
}
func (f fakeNodeStatusClient) LVMPhysicalVolumes(context.Context, string) ([]*storage.LVMPhysicalVolumeStatus, error) {
	return f.pvs, nil
}
func (f fakeNodeStatusClient) LVMVolumeGroups(context.Context, string) ([]*storage.LVMVolumeGroupStatus, error) {
	return f.groups, nil
}
func (f fakeNodeStatusClient) LVMLogicalVolumes(context.Context, string) ([]*storage.LVMLogicalVolumeStatus, error) {
	return f.logical, nil
}
func (f fakeNodeStatusClient) MDArrays(context.Context, string) ([]*storage.MDArrayStatus, error) {
	return f.arrays, nil
}
func (f fakeNodeStatusClient) Volumes(context.Context, string) ([]*block.VolumeStatus, error) {
	return f.volumes, nil
}
func (f fakeNodeStatusClient) TrimSchedules(context.Context, string) ([]*block.VolumeTrimSchedule, error) {
	return f.trims, nil
}
func (f fakeNodeStatusClient) FSScrubs(context.Context, string) ([]*block.FSScrubStatus, error) {
	return f.scrubs, nil
}
func (f fakeNodeStatusClient) SystemDisk(context.Context, string) (*block.SystemDisk, error) {
	return f.disk, nil
}
func (f fakeNodeStatusClient) MachineConfig(context.Context, string) (*talosconfig.MachineConfig, error) {
	return f.config, nil
}

func TestNodeStatusReaderMapsTalosResourcesAndSecurityProfile(t *testing.T) {
	provider, err := configloader.NewFromBytes([]byte("apiVersion: v1alpha1\nkind: SecurityProfileConfig\nworkloadIsolation: true\n"))
	require.NoError(t, err)
	cfg := talosconfig.NewMachineConfig(provider)

	peer := network.NewBGPPeerStatus(network.NamespaceName, "192.0.2.8")
	peer.TypedSpec().Peer = "192.0.2.8"
	peer.TypedSpec().LocalASN = 64512
	peer.TypedSpec().PeerASN = 64513
	peer.TypedSpec().State = nethelpers.BGPSessionStateEstablished
	peer.TypedSpec().Received = 11
	peer.TypedSpec().Advertised = 4

	pv := storage.NewLVMPhysicalVolumeStatus(storage.NamespaceName, "/dev/sdb")
	pv.TypedSpec().Device = "/dev/sdb"
	pv.TypedSpec().VGName = "data"
	pv.TypedSpec().PrettySize = "1 TiB"

	array := storage.NewMDArrayStatus(storage.NamespaceName, "md0")
	array.TypedSpec().Device = "/dev/md0"
	array.TypedSpec().Status = storage.MDArrayPhaseReady
	array.TypedSpec().Level = storage.MDLevelRAID1
	array.TypedSpec().RaidDevices = 2

	volume := block.NewVolumeStatus(block.NamespaceName, "EPHEMERAL")
	volume.Metadata().Labels().Set(block.SystemVolumeLabel, "")
	volume.TypedSpec().Phase = block.VolumePhaseReady
	volume.TypedSpec().Location = "/dev/vda3"
	volume.TypedSpec().MountLocation = "/var"
	volume.TypedSpec().TrimEnabled = true
	volume.TypedSpec().ScrubEnabled = true

	trim := block.NewVolumeTrimSchedule(block.NamespaceName, "EPHEMERAL")
	trim.TypedSpec().Filesystem = block.FilesystemTypeXFS
	trim.TypedSpec().NextTrim = time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC)

	scrub := block.NewFSScrubStatus("/var")
	scrub.TypedSpec().Mountpoint = "/var"
	scrub.TypedSpec().Status = "success"

	disk := block.NewSystemDisk(block.NamespaceName, block.SystemDiskID)
	disk.TypedSpec().DiskID = "vda"
	disk.TypedSpec().DevPath = "/dev/vda"

	reader := newNodeStatusReader(fakeNodeStatusClient{
		peers: []*network.BGPPeerStatus{peer}, pvs: []*storage.LVMPhysicalVolumeStatus{pv}, arrays: []*storage.MDArrayStatus{array},
		volumes: []*block.VolumeStatus{volume}, trims: []*block.VolumeTrimSchedule{trim}, scrubs: []*block.FSScrubStatus{scrub}, disk: disk, config: cfg,
	})

	got, err := reader.List(context.Background(), "worker-1")

	require.NoError(t, err)
	assert.Equal(t, "worker-1", got.Node)
	assert.True(t, got.BGPKnown)
	assert.Equal(t, "ESTABLISHED", got.BGPPeers[0].State)
	assert.Equal(t, "1 TiB", got.LVMPhysicalVolumes[0].PrettySize)
	assert.True(t, got.MDKnown)
	assert.Equal(t, "ready", got.MDArrays[0].Status)
	assert.True(t, got.Volumes[0].System)
	assert.Equal(t, "xfs", got.TrimSchedules[0].Filesystem)
	assert.Equal(t, "/var", got.FSScrubs[0].Mountpoint)
	assert.Equal(t, "/dev/vda", got.SystemDiskPath)
	assert.True(t, got.WorkloadIsolationKnown)
	assert.True(t, got.WorkloadIsolationEnabled)
}

func TestNodeStatusReaderKeepsSuccessfulGroupsWhenBGPReadFails(t *testing.T) {
	reader := newNodeStatusReader(fakeNodeStatusClient{
		peerErr: errors.New("unsupported resource kind"),
		arrays:  []*storage.MDArrayStatus{storage.NewMDArrayStatus(storage.NamespaceName, "md0")},
	})

	got, err := reader.List(context.Background(), "worker-2")

	assert.Error(t, err)
	assert.False(t, got.BGPKnown)
	assert.True(t, got.MDKnown)
	assert.Equal(t, "md0", got.MDArrays[0].ID)
	assert.False(t, got.WorkloadIsolationKnown)
}
