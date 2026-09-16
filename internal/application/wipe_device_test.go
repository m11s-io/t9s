package application_test

import (
	"context"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wipeTestDisks(node string, disks ...domain.DiskSnapshot) application.DisksState {
	return application.DisksState{Status: application.Ready, Node: node, Value: domain.DiskSet{Disks: disks}}
}

func TestRequestActionWipeDeviceBlocksSystemDisk(t *testing.T) {
	model := resetTestModel(application.EtcdState{}, wipeTestDisks("cp-1",
		domain.DiskSnapshot{DeviceName: "/dev/sda", SystemDisk: true},
		domain.DiskSnapshot{DeviceName: "/dev/sdb"},
	))

	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionWipeDevice,
		Targets:      []string{"cp-1"},
		DeviceWipe:   &ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sda", Method: ports.DeviceWipeFast},
		Confirmation: "/dev/sda",
	})

	require.NotNil(t, model.PendingAction)
	assert.Contains(t, model.PendingAction.Blocked, "system disk")
	_, effect := application.Update(model, application.ConfirmPendingAction{})
	assert.Nil(t, effect, "a blocked system-disk wipe must produce no effect")
}

func TestRequestActionWipeDeviceBlocksUnknownOrReadOnlyDisk(t *testing.T) {
	// Empty inventory: the device is not in it.
	unknown := resetTestModel(application.EtcdState{}, application.DisksState{})
	unknown, _ = application.Update(unknown, application.RequestAction{
		Kind:         application.ActionWipeDevice,
		Targets:      []string{"cp-1"},
		DeviceWipe:   &ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sdb", Method: ports.DeviceWipeFast},
		Confirmation: "/dev/sdb",
	})
	require.NotNil(t, unknown.PendingAction)
	assert.Contains(t, unknown.PendingAction.Blocked, "inventory")

	readonly := resetTestModel(application.EtcdState{}, wipeTestDisks("cp-1",
		domain.DiskSnapshot{DeviceName: "/dev/sr0", ReadOnly: true},
	))
	readonly, _ = application.Update(readonly, application.RequestAction{
		Kind:         application.ActionWipeDevice,
		Targets:      []string{"cp-1"},
		DeviceWipe:   &ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sr0", Method: ports.DeviceWipeFast},
		Confirmation: "/dev/sr0",
	})
	require.NotNil(t, readonly.PendingAction)
	assert.Contains(t, readonly.PendingAction.Blocked, "read-only")
}

func TestRequestActionWipeDeviceOpensPendingAndSkipsEtcdGate(t *testing.T) {
	model := resetTestModel(application.EtcdState{Status: application.Loading}, wipeTestDisks("cp-1",
		domain.DiskSnapshot{DeviceName: "/dev/sda", SystemDisk: true},
		domain.DiskSnapshot{DeviceName: "/dev/sdb"},
	))

	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionWipeDevice,
		Targets:      []string{"cp-1"},
		DeviceWipe:   &ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sdb", Method: ports.DeviceWipeFast},
		Confirmation: "/dev/sdb",
	})

	require.NotNil(t, model.PendingAction)
	assert.Equal(t, application.ActionWipeDevice, model.PendingAction.Kind)
	assert.Equal(t, []string{"cp-1"}, model.PendingAction.Targets)
	assert.Empty(t, model.PendingAction.Blocked, "wiping a data disk does not touch etcd quorum")
}

func TestRequestActionWipeDeviceRequiresTypedDeviceConfirmation(t *testing.T) {
	model := resetTestModel(application.EtcdState{}, wipeTestDisks("cp-1",
		domain.DiskSnapshot{DeviceName: "/dev/sdb"},
	))

	// The typed token tolerates the /dev/ prefix regardless of which form the
	// inventory reports.
	accepted, _ := application.Update(model, application.RequestAction{
		Kind:         application.ActionWipeDevice,
		Targets:      []string{"cp-1"},
		DeviceWipe:   &ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sdb"},
		Confirmation: "sdb",
	})
	require.NotNil(t, accepted.PendingAction)

	rejected, _ := application.Update(model, application.RequestAction{
		Kind:         application.ActionWipeDevice,
		Targets:      []string{"cp-1"},
		DeviceWipe:   &ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sdb"},
		Confirmation: "sdc",
	})
	assert.Nil(t, rejected.PendingAction, "a mismatched device token must not open a pending wipe")
}

func TestBuildActionEffectsWipeDeviceCallsWipeDevice(t *testing.T) {
	var wiped []string
	var resetCalls int
	controller := &testkit.FakeNodeController{
		WipeDeviceFunc: func(_ context.Context, node, device string, method ports.DeviceWipeMethod) error {
			wiped = append(wiped, node+":"+device)
			return nil
		},
		ResetFunc: func(context.Context, string, ports.ResetOptions) error {
			resetCalls++
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: controller})
	pending := application.PendingAction{
		Kind:       application.ActionWipeDevice,
		Targets:    []string{"cp-1"},
		DeviceWipe: &ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sdb", Method: ports.DeviceWipeFast},
	}

	effects := application.BuildActionEffects(model, pending)

	require.Len(t, effects, 1)
	effects[0](t.Context(), application.Dependencies{})
	assert.Equal(t, []string{"cp-1:/dev/sdb"}, wiped)
	assert.Zero(t, resetCalls, "a device wipe must not invoke Reset")

	blocked := pending
	blocked.Blocked = "refusing: example"
	assert.Nil(t, application.BuildActionEffects(model, blocked), "a blocked wipe must not build any effect")
}

func TestActionSucceededWipeDeviceRefreshesDisks(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model.Disks = wipeTestDisks("cp-1", domain.DiskSnapshot{DeviceName: "/dev/sdb"})
	model.PendingAction = &application.PendingAction{
		Kind:       application.ActionWipeDevice,
		Targets:    []string{"cp-1"},
		DeviceWipe: &ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sdb"},
	}

	confirmed, effect := application.Update(model, application.ConfirmPendingAction{})
	require.Nil(t, effect)
	require.Nil(t, confirmed.PendingAction)

	refreshed, effect := application.Update(confirmed, application.ActionSucceeded{Generation: confirmed.Generation, Target: "cp-1"})

	assert.NotNil(t, effect, "a completed device wipe must refresh the disks view")
	assert.Equal(t, application.Loading, refreshed.Disks.Status)
}
