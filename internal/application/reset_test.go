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

func resetTestModel(etcd application.EtcdState, disks application.DisksState) application.Model {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model.Nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl},
		{ID: "n2", Name: "worker-1", Role: domain.NodeRoleWorker},
	}}}
	model.Etcd = etcd
	model.Disks = disks
	return model
}

func TestRequestActionResetRequiresTypedTargetConfirmation(t *testing.T) {
	model := resetTestModel(application.EtcdState{}, application.DisksState{})

	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"cp-1"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeAll},
		Confirmation: "cp-1",
	})
	require.NotNil(t, model.PendingAction)
	assert.Equal(t, application.ActionReset, model.PendingAction.Kind)

	wrong := resetTestModel(application.EtcdState{}, application.DisksState{})
	wrong, _ = application.Update(wrong, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"cp-1"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeAll},
		Confirmation: "cp-2",
	})
	assert.Nil(t, wrong.PendingAction, "a mismatched typed token must not open a pending reset")
}

func TestRequestActionResetBulkTokenIsWipeCount(t *testing.T) {
	model := resetTestModel(application.EtcdState{}, application.DisksState{})
	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"cp-1", "cp-2", "cp-3"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeAll},
		Confirmation: "wipe 3 nodes",
	})
	require.NotNil(t, model.PendingAction)

	rejected := resetTestModel(application.EtcdState{}, application.DisksState{})
	rejected, _ = application.Update(rejected, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"cp-1", "cp-2", "cp-3"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeAll},
		Confirmation: "wipe 3",
	})
	assert.Nil(t, rejected.PendingAction)
}

func TestRequestActionResetControlPlaneBlockedWhenQuorumUnknown(t *testing.T) {
	model := resetTestModel(application.EtcdState{Status: application.Loading}, application.DisksState{})
	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"cp-1"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeAll},
		Confirmation: "cp-1",
	})
	require.NotNil(t, model.PendingAction)
	assert.Contains(t, model.PendingAction.Blocked, "unknown")

	confirmed, effect := application.Update(model, application.ConfirmPendingAction{})
	assert.Nil(t, effect, "a blocked reset confirm must produce no effect")
	require.NotNil(t, confirmed.PendingAction, "the blocked reset prompt must remain until cancelled")
	assert.NotEmpty(t, confirmed.PendingAction.Blocked)
}

func TestRequestActionResetWorkerNotQuorumBlocked(t *testing.T) {
	disks := application.DisksState{Status: application.Ready, Node: "worker-1", Value: domain.DiskSet{Disks: []domain.DiskSnapshot{
		{DeviceName: "sda", SystemDisk: true},
		{DeviceName: "sdb"},
	}}}
	model := resetTestModel(application.EtcdState{Status: application.Loading}, disks)
	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"worker-1"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeAll},
		Confirmation: "worker-1",
	})
	require.NotNil(t, model.PendingAction)
	assert.Empty(t, model.PendingAction.Blocked)
	assert.Empty(t, model.PendingAction.Warning)
}

func TestRequestActionResetUserDisksBlockedWhenInventoryUnknown(t *testing.T) {
	model := resetTestModel(application.EtcdState{}, application.DisksState{})
	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"worker-1"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeUserDisks},
		Confirmation: "worker-1",
	})
	require.NotNil(t, model.PendingAction)
	assert.Contains(t, model.PendingAction.Blocked, "inventory")
}

func TestRequestActionResetUserDisksAreListedForWipe(t *testing.T) {
	model := resetTestModel(application.EtcdState{}, application.DisksState{})
	preview := application.ResetPreview{Node: "worker-1", Known: true, SystemDisk: "sda", UserDisks: []string{"sdb", "sdc"}}

	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"worker-1"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeUserDisks},
		Preview:      map[string]application.ResetPreview{"worker-1": preview},
		Confirmation: "worker-1",
	})

	require.NotNil(t, model.PendingAction)
	assert.Empty(t, model.PendingAction.Blocked)
	assert.Equal(t, []string{"sdb", "sdc"}, model.PendingAction.Reset.UserDisks, "Talos wipes only listed user disks, so the overlay preview must populate them")
}

func TestRequestActionResetAllListsUserDisks(t *testing.T) {
	model := resetTestModel(application.EtcdState{}, application.DisksState{})
	preview := application.ResetPreview{Node: "worker-1", Known: true, SystemDisk: "sda", UserDisks: []string{"sdb"}}

	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"worker-1"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeAll},
		Preview:      map[string]application.ResetPreview{"worker-1": preview},
		Confirmation: "worker-1",
	})

	require.NotNil(t, model.PendingAction)
	assert.Empty(t, model.PendingAction.Blocked)
	assert.Equal(t, []string{"sdb"}, model.PendingAction.Reset.UserDisks)
}

func TestRequestActionResetUserDisksBlockedForBulk(t *testing.T) {
	model := resetTestModel(application.EtcdState{}, application.DisksState{})
	preview := application.ResetPreview{Node: "worker-1", Known: true, UserDisks: []string{"sdb"}}

	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"worker-1", "worker-2"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeUserDisks},
		Preview:      map[string]application.ResetPreview{"worker-1": preview},
		Confirmation: "wipe 2 nodes",
	})

	require.NotNil(t, model.PendingAction)
	assert.Contains(t, model.PendingAction.Blocked, "single-node")
}

func TestRequestActionResetTrimsConfirmationWhitespace(t *testing.T) {
	model := resetTestModel(application.EtcdState{}, application.DisksState{})
	preview := application.ResetPreview{Node: "worker-1", Known: true, UserDisks: []string{"sdb"}}

	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"worker-1"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeAll},
		Preview:      map[string]application.ResetPreview{"worker-1": preview},
		Confirmation: "  worker-1  ",
	})

	require.NotNil(t, model.PendingAction, "a valid token with surrounding whitespace must not be silently dropped")
}

func TestConfirmPendingActionResetReevaluatesQuorum(t *testing.T) {
	model := resetTestModel(application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1", StatusKnown: true},
		{Hostname: "cp-2", StatusKnown: true},
		{Hostname: "cp-3", StatusKnown: true},
	}}}, application.DisksState{})
	model, _ = application.Update(model, application.RequestAction{
		Kind:         application.ActionReset,
		Targets:      []string{"cp-1"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeAll},
		Preview:      map[string]application.ResetPreview{"cp-1": {Node: "cp-1", Known: true, SystemDisk: "sda"}},
		Confirmation: "cp-1",
	})
	require.NotNil(t, model.PendingAction)
	require.Empty(t, model.PendingAction.Blocked)

	model.Etcd = application.EtcdState{Status: application.Loading}
	confirmed, effect := application.Update(model, application.ConfirmPendingAction{})

	assert.Nil(t, effect)
	require.NotNil(t, confirmed.PendingAction)
	assert.NotEmpty(t, confirmed.PendingAction.Blocked)
}

func TestBuildActionEffectsResetFansOutPerTarget(t *testing.T) {
	var calls []string
	controller := &testkit.FakeNodeController{
		ResetFunc: func(_ context.Context, target string, _ ports.ResetOptions) error {
			calls = append(calls, target)
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: controller})
	pending := application.PendingAction{
		Kind:    application.ActionReset,
		Targets: []string{"cp-1", "cp-2"},
		Reset:   &ports.ResetOptions{Mode: ports.WipeModeAll, Graceful: true, Reboot: true},
	}

	effects := application.BuildActionEffects(model, pending)

	require.Len(t, effects, 2)
	for _, effect := range effects {
		effect(t.Context(), application.Dependencies{})
	}
	assert.Equal(t, []string{"cp-1", "cp-2"}, calls)
}
