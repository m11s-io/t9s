package tui

import (
	"context"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func etcdMembershipTestModel(t *testing.T, writesEnabled bool, operations *testkit.FakeEtcdOperations, members []domain.EtcdMemberSnapshot) model {
	t.Helper()
	appModel, _ := application.NewModel("prod")
	appModel.WritesEnabled = writesEnabled
	appModel, _ = application.Update(appModel, application.NodesLoaded{
		Generation: appModel.Generation,
		Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl},
			{ID: "n2", Name: "cp-2", Role: domain.NodeRoleControl},
			{ID: "n3", Name: "cp-3", Role: domain.NodeRoleControl},
		}},
	})
	appModel, _ = application.Update(appModel, application.EtcdLoaded{Generation: appModel.Generation, Etcd: domain.EtcdSet{Members: members}})
	appModel, _ = application.Update(appModel, application.SessionOpened{Generation: appModel.Generation, EtcdOperations: operations})
	runner := application.NewRunner(application.Dependencies{})
	root := newModel(t.Context(), false, appModel, runner)
	root.views = root.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
	root.etcd = root.etcd.setState(root.application.Etcd)
	return root
}

func threeVoterMembers() []domain.EtcdMemberSnapshot {
	return []domain.EtcdMemberSnapshot{
		{MemberID: 1, Hostname: "cp-1"}, // dead target
		{MemberID: 2, Hostname: "cp-2", StatusKnown: true},
		{MemberID: 3, Hostname: "cp-3", StatusKnown: true},
	}
}

func TestEtcdRemoveKeyWithWritesDisabledIsInert(t *testing.T) {
	root := etcdMembershipTestModel(t, false, &testkit.FakeEtcdOperations{}, threeVoterMembers())

	updated, cmd := root.Update(keyPress('R'))

	assert.Nil(t, cmd)
	assert.Nil(t, updated.(model).application.PendingEtcdAction)
}

func TestEtcdRemoveKeyOpensConfirmPromptWithSnapshotPath(t *testing.T) {
	root := etcdMembershipTestModel(t, true, &testkit.FakeEtcdOperations{}, threeVoterMembers())

	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingEtcdAction)
	assert.Equal(t, application.EtcdActionRemoveMember, rootModel.application.PendingEtcdAction.Kind)
	assert.Contains(t, rootModel.activePrompt(), "(y/n)")
	assert.Contains(t, rootModel.activePrompt(), "snapshot to")
}

func TestEtcdLeaveKeyHandlesKittyShiftEncoding(t *testing.T) {
	root := etcdMembershipTestModel(t, true, &testkit.FakeEtcdOperations{}, []domain.EtcdMemberSnapshot{
		{MemberID: 1, Hostname: "cp-1", StatusKnown: true},
		{MemberID: 2, Hostname: "cp-2", StatusKnown: true},
		{MemberID: 3, Hostname: "cp-3", StatusKnown: true},
	})

	updated, _ := root.Update(shiftKeyPress('L'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingEtcdAction)
	assert.Equal(t, application.EtcdActionLeaveCluster, rootModel.application.PendingEtcdAction.Kind)
	assert.Equal(t, "cp-1", rootModel.application.PendingEtcdAction.Node)
	assert.Contains(t, rootModel.activePrompt(), "(y/n)")
}

func TestEtcdRemoveConfirmRunsSnapshotBeforeMembership(t *testing.T) {
	removeCalled := false
	operations := &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			return domain.EtcdSnapshotResult{Node: node, Path: path}, nil
		},
		RemoveMemberByIDFunc: func(context.Context, string, uint64) error {
			removeCalled = true
			return nil
		},
	}
	root := etcdMembershipTestModel(t, true, operations, threeVoterMembers())
	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)
	require.NotNil(t, rootModel.application.PendingEtcdAction)

	updated, cmd := rootModel.Update(keyPress('y'))
	rootModel = updated.(model)
	require.NotNil(t, cmd, "confirming a removal schedules the pre-removal snapshot")
	require.NotNil(t, rootModel.application.PendingEtcdAction)
	assert.Equal(t, application.EtcdStageSnapshot, rootModel.application.PendingEtcdAction.Stage)

	updated, _ = rootModel.Update(cmd())
	rootModel = updated.(model)
	assert.Equal(t, application.EtcdStageOperation, rootModel.application.PendingEtcdAction.Stage)
	assert.False(t, removeCalled, "the removal must wait for a successful snapshot")
}

func TestEtcdConfirmBlockedRemovalDoesNotEmitEffects(t *testing.T) {
	root := etcdMembershipTestModel(t, true, &testkit.FakeEtcdOperations{}, threeVoterMembers())
	root.application.PendingEtcdAction = &application.PendingEtcdAction{
		Kind:    application.EtcdActionRemoveMember,
		Node:    "cp-2",
		Blocked: "refusing: example",
	}

	updated, cmd := root.Update(keyPress('y'))
	rootModel := updated.(model)

	assert.Nil(t, cmd)
	require.NotNil(t, rootModel.application.PendingEtcdAction)
}

func TestEtcdActionHintsShowMembershipKeysOnlyWhenWritesEnabled(t *testing.T) {
	disabledKeys := hintKeys(actionHints(viewEtcd, false))
	enabledKeys := hintKeys(actionHints(viewEtcd, true))

	assert.NotContains(t, disabledKeys, "R")
	assert.NotContains(t, disabledKeys, "L")
	assert.Contains(t, enabledKeys, "R")
	assert.Contains(t, enabledKeys, "L")
}
