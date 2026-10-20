package tui

import (
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestRenderBGPPeersShowsPeerStateAndPartialNodes(t *testing.T) {
	got := renderBGPPeers(directNodeStatusState{
		Status: directPartial,
		Err:    "some status resources are unavailable",
		Value: domain.NodeStatusSet{Statuses: []domain.NodeStatusSnapshot{
			{Node: "worker-1", BGPKnown: true, BGPPeers: []domain.BGPPeerSnapshot{{Peer: "192.0.2.8", PeerASN: 64512, State: "ESTABLISHED", BFDState: "up", Received: 19, Advertised: 7}}},
			{Node: "worker-2", BGPKnown: false},
		}},
	})

	assert.Contains(t, got, "worker-1")
	assert.Contains(t, got, "192.0.2.8")
	assert.Contains(t, got, "ESTABLISHED")
	assert.Contains(t, got, "BGP status unknown on 1 node")
	assert.NotContains(t, got, "BGP peer status partial:")
}

func TestBGPPaletteOpensDedicatedViewAndBackReturnsToNodes(t *testing.T) {
	model := newModel(t.Context(), nil, LaunchOptions{})
	model.splash = false
	model, _ = runDirectPalette(t, model, "bgp")
	assert.Equal(t, viewBGPPeers, model.views.top().Kind)
	assert.Contains(t, model.View().Content, "BGP peer status loading")

	model, _ = updateDirect(model, keyPress('q'))
	assert.Equal(t, viewNodes, model.views.top().Kind)
}

func TestDirectHealthInputUsesLoadedNodeStatusSnapshots(t *testing.T) {
	model := directHealthModel()
	request, _ := model.requests.begin("cluster-node-status", model.generation())
	model, _ = updateDirect(model, directNodeStatusesLoaded{request: request.stamp, values: domain.NodeStatusSet{Statuses: []domain.NodeStatusSnapshot{{
		Node: "cp-1", BGPKnown: true, BGPPeers: []domain.BGPPeerSnapshot{{ID: "peer", Peer: "192.0.2.2", State: "ACTIVE"}},
	}}}})

	input := directHealthInput(model.cluster)

	assert.True(t, input.NodeStatusesKnown)
	assert.Len(t, input.NodeStatuses.Statuses, 1)
	var rules []string
	for _, diagnosis := range application.EvaluateObservations(input) {
		rules = append(rules, diagnosis.RuleID)
	}
	assert.Contains(t, rules, "bgp-peer-not-established")
}

func TestRenderNodeDetailSeparatesIsolationConfigFromSandboxdAndShowsSystemVolume(t *testing.T) {
	got := renderNodeDetailWithStatus(
		domain.NodeSnapshot{Name: "worker-1", ID: "node-id"},
		domain.NodeStatusSnapshot{
			WorkloadIsolationKnown:   true,
			WorkloadIsolationEnabled: false,
			SystemDiskKnown:          true,
			SystemDiskPath:           "/dev/vda",
			VolumesKnown:             true,
			Volumes:                  []domain.VolumeSnapshot{{ID: "EPHEMERAL", System: true, Phase: "ready", Location: "/dev/vda3", MountLocation: "/var", TrimEnabled: true, ScrubEnabled: true}},
		},
		true,
		"Running (healthy)",
	)

	assert.Contains(t, got, "WORKLOAD ISOLATION")
	assert.Contains(t, got, "disabled")
	assert.Contains(t, got, "SANDBOXD SERVICE")
	assert.Contains(t, got, "Running (healthy)")
	assert.Contains(t, got, "SYSTEM DISK")
	assert.Contains(t, got, "/dev/vda")
	assert.Contains(t, got, "SYSTEM VOLUME")
	assert.Contains(t, got, "EPHEMERAL")
}

func TestRenderDiskDetailIncludesMatchingLVMMDAndVolumeStatus(t *testing.T) {
	got := renderDiskDetailWithStatus(domain.DiskSnapshot{DeviceName: "/dev/sdb"}, domain.NodeStatusSnapshot{
		LVMPhysicalVolumesKnown: true,
		LVMPhysicalVolumes:      []domain.LVMPhysicalVolumeSnapshot{{Device: "/dev/sdb", VGName: "data", PrettySize: "1 TiB", Missing: ""}},
		LVMVolumeGroupsKnown:    true,
		LVMVolumeGroups:         []domain.LVMVolumeGroupSnapshot{{Name: "data", MissingPVCount: "0"}},
		LVMLogicalVolumesKnown:  true,
		LVMLogicalVolumes:       []domain.LVMLogicalVolumeSnapshot{{Name: "archive", Path: "/dev/data/archive", VGName: "data", Layout: "linear", Active: "active", Size: "1 TiB"}},
		MDKnown:                 true,
		MDArrays:                []domain.MDArraySnapshot{{ID: "md0", Name: "mirror", Device: "/dev/md0", Level: "raid1", Status: "ready", Members: []string{"/dev/sdb", "/dev/sdc"}, RaidDevices: 2}},
		VolumesKnown:            true,
		Volumes:                 []domain.VolumeSnapshot{{ID: "archive", Phase: "ready", Location: "/dev/sdb", Filesystem: "xfs", MountLocation: "/mnt/archive", TrimEnabled: true}},
	}, true)

	assert.Contains(t, got, "LVM PHYSICAL VOLUME")
	assert.Contains(t, got, "LVM VOLUME GROUP")
	assert.Contains(t, got, "LVM LOGICAL VOLUME")
	assert.Contains(t, got, "MD RAID")
	assert.Contains(t, got, "VOLUME")
	assert.Contains(t, got, "/mnt/archive")
}

func TestOverviewRollsUpBGPWarningsAndUnknownGroups(t *testing.T) {
	input := application.HealthInput{
		NodeStatusesKnown: true,
		NodeStatuses: domain.NodeStatusSet{Statuses: []domain.NodeStatusSnapshot{{
			Node: "worker-1", BGPKnown: true,
			BGPPeers:                []domain.BGPPeerSnapshot{{ID: "peer-1", Peer: "192.0.2.8", State: "ACTIVE"}},
			LVMPhysicalVolumesKnown: false,
			LVMVolumeGroupsKnown:    true,
			LVMLogicalVolumesKnown:  true,
			MDKnown:                 true,
			VolumesKnown:            true, TrimKnown: true, ScrubKnown: true,
		}}},
	}

	got := renderOverview(input)

	assert.Contains(t, got, "BGP       1 resources, 1 warning, 0 critical, 0 unknown")
	assert.Contains(t, got, "STORAGE   unknown on some nodes")
}

func TestOverviewRollsUpIsolationAndSandboxdSeparately(t *testing.T) {
	unhealthy := false
	input := application.HealthInput{
		NodeStatusesKnown: true,
		NodeStatuses:      domain.NodeStatusSet{Statuses: []domain.NodeStatusSnapshot{{Node: "worker-1", WorkloadIsolationKnown: true, WorkloadIsolationEnabled: false}}},
		ServicesKnown:     true,
		Services:          domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "worker-1", Name: "sandboxd", State: "Failed", Healthy: &unhealthy}}},
	}

	got := renderOverview(input)

	assert.Contains(t, got, "SYSTEM    isolation 0 enabled, 1 disabled, 0 unknown; sandboxd 1 services, 1 unhealthy, 0 unknown")
}
