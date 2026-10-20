package talos

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/storage"
)

type nodeStatusClient interface {
	BGPPeers(context.Context, string) ([]*network.BGPPeerStatus, error)
	LVMPhysicalVolumes(context.Context, string) ([]*storage.LVMPhysicalVolumeStatus, error)
	LVMVolumeGroups(context.Context, string) ([]*storage.LVMVolumeGroupStatus, error)
	LVMLogicalVolumes(context.Context, string) ([]*storage.LVMLogicalVolumeStatus, error)
	MDArrays(context.Context, string) ([]*storage.MDArrayStatus, error)
	Volumes(context.Context, string) ([]*block.VolumeStatus, error)
	TrimSchedules(context.Context, string) ([]*block.VolumeTrimSchedule, error)
	FSScrubs(context.Context, string) ([]*block.FSScrubStatus, error)
	SystemDisk(context.Context, string) (*block.SystemDisk, error)
	MachineConfig(context.Context, string) (*talosconfig.MachineConfig, error)
}

type machineryNodeStatusClient struct{ client *talosclient.Client }

func (c machineryNodeStatusClient) BGPPeers(ctx context.Context, node string) ([]*network.BGPPeerStatus, error) {
	list, err := safe.StateList[*network.BGPPeerStatus](talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(network.NamespaceName, network.BGPPeerStatusType, "", resource.VersionUndefined))
	if err != nil {
		return nil, err
	}
	return collectResourceList(list), nil
}

func (c machineryNodeStatusClient) LVMPhysicalVolumes(ctx context.Context, node string) ([]*storage.LVMPhysicalVolumeStatus, error) {
	list, err := safe.StateList[*storage.LVMPhysicalVolumeStatus](talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(storage.NamespaceName, storage.LVMPhysicalVolumeStatusType, "", resource.VersionUndefined))
	if err != nil {
		return nil, err
	}
	return collectResourceList(list), nil
}

func (c machineryNodeStatusClient) LVMVolumeGroups(ctx context.Context, node string) ([]*storage.LVMVolumeGroupStatus, error) {
	list, err := safe.StateList[*storage.LVMVolumeGroupStatus](talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(storage.NamespaceName, storage.LVMVolumeGroupStatusType, "", resource.VersionUndefined))
	if err != nil {
		return nil, err
	}
	return collectResourceList(list), nil
}

func (c machineryNodeStatusClient) LVMLogicalVolumes(ctx context.Context, node string) ([]*storage.LVMLogicalVolumeStatus, error) {
	list, err := safe.StateList[*storage.LVMLogicalVolumeStatus](talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(storage.NamespaceName, storage.LVMLogicalVolumeStatusType, "", resource.VersionUndefined))
	if err != nil {
		return nil, err
	}
	return collectResourceList(list), nil
}

func (c machineryNodeStatusClient) MDArrays(ctx context.Context, node string) ([]*storage.MDArrayStatus, error) {
	list, err := safe.StateList[*storage.MDArrayStatus](talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(storage.NamespaceName, storage.MDArrayStatusType, "", resource.VersionUndefined))
	if err != nil {
		return nil, err
	}
	return collectResourceList(list), nil
}

func (c machineryNodeStatusClient) Volumes(ctx context.Context, node string) ([]*block.VolumeStatus, error) {
	list, err := safe.StateList[*block.VolumeStatus](talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(block.NamespaceName, block.VolumeStatusType, "", resource.VersionUndefined))
	if err != nil {
		return nil, err
	}
	return collectResourceList(list), nil
}

func (c machineryNodeStatusClient) TrimSchedules(ctx context.Context, node string) ([]*block.VolumeTrimSchedule, error) {
	list, err := safe.StateList[*block.VolumeTrimSchedule](talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(block.NamespaceName, block.VolumeTrimScheduleType, "", resource.VersionUndefined))
	if err != nil {
		return nil, err
	}
	return collectResourceList(list), nil
}

func (c machineryNodeStatusClient) FSScrubs(ctx context.Context, node string) ([]*block.FSScrubStatus, error) {
	list, err := safe.StateList[*block.FSScrubStatus](talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(block.NamespaceName, block.FSScrubStatusType, "", resource.VersionUndefined))
	if err != nil {
		return nil, err
	}
	return collectResourceList(list), nil
}

func (c machineryNodeStatusClient) SystemDisk(ctx context.Context, node string) (*block.SystemDisk, error) {
	return safe.StateGet[*block.SystemDisk](talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(block.NamespaceName, block.SystemDiskType, block.SystemDiskID, resource.VersionUndefined))
}

func (c machineryNodeStatusClient) MachineConfig(ctx context.Context, node string) (*talosconfig.MachineConfig, error) {
	return safe.StateGet[*talosconfig.MachineConfig](talosclient.WithNode(ctx, node), c.client.COSI,
		resource.NewMetadata(talosconfig.NamespaceName, talosconfig.MachineConfigType, talosconfig.ActiveID, resource.VersionUndefined))
}

type nodeStatusReader struct{ client nodeStatusClient }

var _ ports.NodeStatusReader = (*nodeStatusReader)(nil)

func newNodeStatusReader(client nodeStatusClient) ports.NodeStatusReader {
	return &nodeStatusReader{client: client}
}

func (r *nodeStatusReader) List(ctx context.Context, node string) (domain.NodeStatusSnapshot, error) {
	status := domain.NodeStatusSnapshot{Node: node}
	var readErrs []error
	addErr := func(kind string, err error) {
		if err != nil {
			readErrs = append(readErrs, fmt.Errorf("list %s: %w", kind, err))
		}
	}

	peers, err := r.client.BGPPeers(ctx, node)
	status.BGPKnown = err == nil
	addErr("BGP peers", err)
	for _, peer := range peers {
		spec := peer.TypedSpec()
		status.BGPPeers = append(status.BGPPeers, domain.BGPPeerSnapshot{
			ID: peer.Metadata().ID(), Instance: spec.Instance, Peer: spec.Peer, LocalASN: spec.LocalASN, PeerASN: spec.PeerASN,
			State: spec.State.String(), Since: spec.Since, Received: spec.Received, Advertised: spec.Advertised,
			Accepted: spec.Accepted, BFDState: spec.BFDState,
		})
	}

	physical, err := r.client.LVMPhysicalVolumes(ctx, node)
	status.LVMPhysicalVolumesKnown = err == nil
	addErr("LVM physical volumes", err)
	for _, value := range physical {
		spec := value.TypedSpec()
		status.LVMPhysicalVolumes = append(status.LVMPhysicalVolumes, domain.LVMPhysicalVolumeSnapshot{Device: spec.Device, VGName: spec.VGName, Missing: spec.Missing, PrettySize: spec.PrettySize, PrettyFree: spec.PrettyFree})
	}

	groups, err := r.client.LVMVolumeGroups(ctx, node)
	status.LVMVolumeGroupsKnown = err == nil
	addErr("LVM volume groups", err)
	for _, value := range groups {
		spec := value.TypedSpec()
		status.LVMVolumeGroups = append(status.LVMVolumeGroups, domain.LVMVolumeGroupSnapshot{Name: spec.Name, Partial: spec.Partial, MissingPVCount: spec.MissingPVCount, PrettySize: spec.PrettySize, PrettyFree: spec.PrettyFree})
	}

	logical, err := r.client.LVMLogicalVolumes(ctx, node)
	status.LVMLogicalVolumesKnown = err == nil
	addErr("LVM logical volumes", err)
	for _, value := range logical {
		spec := value.TypedSpec()
		status.LVMLogicalVolumes = append(status.LVMLogicalVolumes, domain.LVMLogicalVolumeSnapshot{Name: spec.Name, Path: spec.Path, VGName: spec.VGName, Layout: spec.Layout, Active: spec.Active, Size: spec.Size})
	}

	arrays, err := r.client.MDArrays(ctx, node)
	status.MDKnown = err == nil
	addErr("MD arrays", err)
	for _, value := range arrays {
		spec := value.TypedSpec()
		status.MDArrays = append(status.MDArrays, domain.MDArraySnapshot{ID: value.Metadata().ID(), Name: spec.Name, Device: spec.Device, Level: spec.Level.String(), Status: spec.Status.String(), SyncAction: spec.SyncAction, ArrayState: spec.ArrayState, RaidDevices: spec.RaidDevices, Members: append([]string(nil), spec.Members...), Error: spec.Error})
	}

	volumes, err := r.client.Volumes(ctx, node)
	status.VolumesKnown = err == nil
	addErr("volumes", err)
	for _, value := range volumes {
		spec := value.TypedSpec()
		_, system := value.Metadata().Labels().Get(block.SystemVolumeLabel)
		status.Volumes = append(status.Volumes, domain.VolumeSnapshot{ID: value.Metadata().ID(), System: system, Phase: spec.Phase.String(), Type: spec.Type.String(), Location: spec.Location, ParentLocation: spec.ParentLocation, MountLocation: spec.MountLocation, Filesystem: spec.Filesystem.String(), SizeBytes: spec.Size, Error: spec.ErrorMessage, TrimEnabled: spec.TrimEnabled, TrimInterval: spec.TrimInterval, ScrubEnabled: spec.ScrubEnabled, ScrubInterval: spec.ScrubInterval})
	}

	trims, err := r.client.TrimSchedules(ctx, node)
	status.TrimKnown = err == nil
	addErr("volume trim schedules", err)
	for _, value := range trims {
		spec := value.TypedSpec()
		status.TrimSchedules = append(status.TrimSchedules, domain.TrimScheduleSnapshot{ID: value.Metadata().ID(), Filesystem: spec.Filesystem.String(), Interval: spec.Interval, NextTrim: spec.NextTrim})
	}

	scrubs, err := r.client.FSScrubs(ctx, node)
	status.ScrubKnown = err == nil
	addErr("filesystem scrub status", err)
	for _, value := range scrubs {
		spec := value.TypedSpec()
		status.FSScrubs = append(status.FSScrubs, domain.FSScrubSnapshot{ID: value.Metadata().ID(), Mountpoint: spec.Mountpoint, Interval: spec.Interval, Time: spec.Time, Duration: spec.Duration, Status: spec.Status})
	}

	systemDisk, err := r.client.SystemDisk(ctx, node)
	if err == nil && systemDisk == nil {
		err = errors.New("system disk resource is empty")
	}
	status.SystemDiskKnown = err == nil
	addErr("system disk", err)
	if systemDisk != nil {
		status.SystemDiskID = systemDisk.TypedSpec().DiskID
		status.SystemDiskPath = systemDisk.TypedSpec().DevPath
	}

	config, err := r.client.MachineConfig(ctx, node)
	if err == nil && config == nil {
		err = errors.New("active machine config is empty")
	}
	status.WorkloadIsolationKnown = err == nil
	addErr("active machine config", err)
	if config != nil {
		security := config.Provider().SecurityProfileConfig()
		if security != nil {
			status.WorkloadIsolationEnabled = security.WorkloadIsolation()
		}
	}

	sort.SliceStable(status.BGPPeers, func(i, j int) bool { return status.BGPPeers[i].Peer < status.BGPPeers[j].Peer })
	sort.SliceStable(status.MDArrays, func(i, j int) bool { return status.MDArrays[i].Device < status.MDArrays[j].Device })
	sort.SliceStable(status.Volumes, func(i, j int) bool { return status.Volumes[i].ID < status.Volumes[j].ID })

	return status, errors.Join(readErrs...)
}

func collectResourceList[T safe.ListedResource](values safe.List[T]) []T {
	result := make([]T, 0)
	for value := range values.All() {
		result = append(result, value)
	}
	return result
}
