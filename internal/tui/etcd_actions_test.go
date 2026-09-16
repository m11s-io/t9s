package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func etcdTestModel(t *testing.T, writesEnabled bool, operations *testkit.FakeEtcdOperations) model {
	t.Helper()
	appModel, _ := application.NewModel("prod")
	appModel.WritesEnabled = writesEnabled
	appModel, _ = application.Update(appModel, application.NodesLoaded{
		Generation: appModel.Generation,
		Nodes:      domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl}}},
	})
	appModel, _ = application.Update(appModel, application.EtcdLoaded{
		Generation: appModel.Generation,
		Etcd: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
			{Hostname: "cp-1", MemberID: 1, StatusKnown: true},
			{Hostname: "cp-2", MemberID: 2, StatusKnown: true},
		}},
	})
	appModel, _ = application.Update(appModel, application.SessionOpened{Generation: appModel.Generation, EtcdOperations: operations})
	runner := application.NewRunner(application.Dependencies{})
	root := newModel(t.Context(), false, appModel, runner)
	root.views = root.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
	root.etcd = root.etcd.setState(root.application.Etcd)
	return root
}

func TestEtcdSnapshotKeyWithWritesDisabledIsInert(t *testing.T) {
	root := etcdTestModel(t, false, &testkit.FakeEtcdOperations{})

	updated, cmd := root.Update(keyPress('s'))

	assert.Nil(t, cmd)
	assert.Nil(t, updated.(model).snapshotPrompt)
	assert.Equal(t, application.EtcdSnapshotState{}, updated.(model).application.EtcdSnapshot)
}

func TestEtcdSnapshotKeyOpensPathPrompt(t *testing.T) {
	root := etcdTestModel(t, true, &testkit.FakeEtcdOperations{})

	updated, cmd := root.Update(keyPress('s'))
	require.NotNil(t, cmd)
	updated, _ = updated.(model).Update(cmd())
	rootModel := updated.(model)

	require.NotNil(t, rootModel.snapshotPrompt)
	assert.Contains(t, rootModel.activePrompt(), "SNAPSHOT")
	assert.Equal(t, "cp-1", rootModel.snapshotPrompt.member)
}

func TestEtcdSnapshotPromptEnterSendsConfirmMessage(t *testing.T) {
	root := etcdTestModel(t, true, &testkit.FakeEtcdOperations{})
	updated, cmd := root.Update(keyPress('s'))
	updated, _ = updated.(model).Update(cmd())
	rootModel := updated.(model)
	require.NotNil(t, rootModel.snapshotPrompt)

	rootModel.snapshotPrompt.input.SetValue("/tmp/etcd-backup.db")
	updated, cmd = rootModel.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	rootModel = updated.(model)

	require.Nil(t, rootModel.snapshotPrompt, "Enter must close the path prompt")
	require.NotNil(t, cmd)
	updated, _ = rootModel.Update(cmd())
	rootModel = updated.(model)

	assert.Equal(t, application.Ready, rootModel.application.EtcdSnapshot.Status)
	assert.Equal(t, "/tmp/etcd-backup.db", rootModel.application.EtcdSnapshot.Result.Path)
}

func TestEtcdPromptEscCancels(t *testing.T) {
	root := etcdTestModel(t, true, &testkit.FakeEtcdOperations{})
	updated, cmd := root.Update(keyPress('s'))
	updated, _ = updated.(model).Update(cmd())
	rootModel := updated.(model)
	require.NotNil(t, rootModel.snapshotPrompt)

	updated, _ = rootModel.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	rootModel = updated.(model)

	assert.Nil(t, rootModel.snapshotPrompt)
	assert.Equal(t, application.EtcdSnapshotState{}, rootModel.application.EtcdSnapshot, "cancelling must not leave a phantom in-flight snapshot")
}

func TestEtcdSnapshotPromptEnterInvalidPathShowsNotice(t *testing.T) {
	root := etcdTestModel(t, true, &testkit.FakeEtcdOperations{})
	updated, cmd := root.Update(keyPress('s'))
	updated, _ = updated.(model).Update(cmd())
	rootModel := updated.(model)
	require.NotNil(t, rootModel.snapshotPrompt)

	rootModel.snapshotPrompt.input.SetValue("..")
	updated, _ = rootModel.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	rootModel = updated.(model)

	assert.Nil(t, rootModel.snapshotPrompt)
	assert.Contains(t, rootModel.notice, "snapshot failed", "a rejected path must be surfaced to the operator")
}

func TestEtcdDefragKeyWithWritesDisabledIsInert(t *testing.T) {
	root := etcdTestModel(t, false, &testkit.FakeEtcdOperations{})

	updated, cmd := root.Update(keyPress('d'))

	assert.Nil(t, cmd)
	assert.Nil(t, updated.(model).application.PendingEtcdAction)
}

func TestEtcdDefragKeyOpensConfirmPrompt(t *testing.T) {
	root := etcdTestModel(t, true, &testkit.FakeEtcdOperations{})

	updated, _ := root.Update(keyPress('d'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingEtcdAction)
	assert.Equal(t, application.EtcdActionDefragment, rootModel.application.PendingEtcdAction.Kind)
	assert.Contains(t, rootModel.activePrompt(), "(y/n)")
}

func TestEtcdDefragKeyConfirmedCallsOperations(t *testing.T) {
	var defragged []string
	operations := &testkit.FakeEtcdOperations{
		DefragmentFunc: func(_ context.Context, node string) error {
			defragged = append(defragged, node)
			return nil
		},
	}
	root := etcdTestModel(t, true, operations)
	updated, _ := root.Update(keyPress('d'))
	rootModel := updated.(model)
	require.NotNil(t, rootModel.application.PendingEtcdAction)

	updated, cmd := rootModel.Update(keyPress('y'))
	require.NotNil(t, cmd)
	cmd()

	assert.Nil(t, updated.(model).application.PendingEtcdAction)
	assert.Equal(t, []string{"cp-1"}, defragged)
}

func TestEtcdDefragTargetsSelectedMemberNotFirstControlPlane(t *testing.T) {
	var defragged []string
	operations := &testkit.FakeEtcdOperations{
		DefragmentFunc: func(_ context.Context, node string) error {
			defragged = append(defragged, node)
			return nil
		},
	}
	root := etcdTestModel(t, true, operations)
	// Move the :etcd selection from cp-1 to cp-2; a handler that always took
	// the first control-plane node would still pass against cp-1.
	root.etcd = root.etcd.update(keyPress('j'))

	updated, _ := root.Update(keyPress('d'))
	rootModel := updated.(model)
	require.NotNil(t, rootModel.application.PendingEtcdAction)
	assert.Equal(t, "cp-2", rootModel.application.PendingEtcdAction.Node)

	updated, cmd := rootModel.Update(keyPress('y'))
	require.NotNil(t, cmd)
	cmd()

	assert.Equal(t, []string{"cp-2"}, defragged)
}

func TestEtcdDisarmAlarmKeyOpensConfirmPrompt(t *testing.T) {
	root := etcdTestModel(t, true, &testkit.FakeEtcdOperations{})

	updated, _ := root.Update(shiftKeyPress('A'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingEtcdAction)
	assert.Equal(t, application.EtcdActionDisarmAlarms, rootModel.application.PendingEtcdAction.Kind)
	assert.Contains(t, rootModel.activePrompt(), "(y/n)")
}

func TestEtcdDisarmAlarmKeyWithWritesDisabledIsInert(t *testing.T) {
	root := etcdTestModel(t, false, &testkit.FakeEtcdOperations{})

	updated, _ := root.Update(shiftKeyPress('A'))

	assert.Nil(t, updated.(model).application.PendingEtcdAction)
}

func TestEtcdConfirmBlockedActionDoesNotEmitEffects(t *testing.T) {
	root := etcdTestModel(t, true, &testkit.FakeEtcdOperations{})
	root.application.PendingEtcdAction = &application.PendingEtcdAction{
		Kind:    application.EtcdActionDefragment,
		Node:    "cp-1",
		Blocked: "refusing: example",
	}

	updated, cmd := root.Update(keyPress('y'))
	rootModel := updated.(model)

	assert.Nil(t, cmd, "a blocked etcd confirm must produce no effect command")
	require.NotNil(t, rootModel.application.PendingEtcdAction, "the blocked prompt must remain until cancelled")
}

func TestEtcdActionHintsShowWriteKeysOnlyWhenWritesEnabled(t *testing.T) {
	disabledKeys := hintKeys(actionHints(viewEtcd, false))
	enabledKeys := hintKeys(actionHints(viewEtcd, true))

	assert.NotContains(t, disabledKeys, "s")
	assert.NotContains(t, disabledKeys, "d")
	assert.NotContains(t, disabledKeys, "A")
	assert.Contains(t, enabledKeys, "s")
	assert.Contains(t, enabledKeys, "d")
	assert.Contains(t, enabledKeys, "A")
}

func hintKeys(hints []actionHint) []string {
	keys := make([]string, 0, len(hints))
	for _, hint := range hints {
		keys = append(keys, hint.Key)
	}
	return keys
}
