package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writesEnabledTestModel wires a real application.Runner (not nil) because
// the confirm ("y") path's tea.Cmd closures call m.runner.Run(...) when
// invoked by the test — a nil runner would panic there even though it's
// harmless for the R/X/cancel paths, which never reach m.runner.Run.
func writesEnabledTestModel(t *testing.T, controller *testkit.FakeNodeController) model {
	t.Helper()
	appModel, _ := application.NewModel("prod")
	appModel.WritesEnabled = true
	appModel, _ = application.Update(appModel, application.NodesLoaded{
		Generation: appModel.Generation,
		Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl},
			{ID: "n2", Name: "worker-1", Role: domain.NodeRoleWorker},
		}},
	})
	appModel, _ = application.Update(appModel, application.SessionOpened{Generation: appModel.Generation, NodeController: controller})
	runner := application.NewRunner(application.Dependencies{})
	root := newModel(t.Context(), false, appModel, runner)
	root.nodes = root.nodes.setState(root.application.Nodes)
	return root
}

func TestRebootKeyWithWritesDisabledIsInert(t *testing.T) {
	appModel, _ := application.NewModel("prod")
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))

	updated, _ := root.Update(keyPress('R'))

	assert.Nil(t, updated.(model).application.PendingAction)
}

func TestRebootKeyWithWritesEnabledOpensConfirmPrompt(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{})

	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingAction)
	assert.Equal(t, application.ActionReboot, rootModel.application.PendingAction.Kind)
	assert.Contains(t, rootModel.activePrompt(), "cp-1")
	assert.Contains(t, rootModel.activePrompt(), "(y/n)")
}

func TestNonYKeyCancelsPendingAction(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{})
	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)

	updated, _ = rootModel.Update(keyPress('n'))
	rootModel = updated.(model)

	assert.Nil(t, rootModel.application.PendingAction)
}

func TestConfirmingRebootRunsControllerAndReportsSuccess(t *testing.T) {
	controller := &testkit.FakeNodeController{
		RebootFunc: func(_ context.Context, _ string, _ ports.RebootMode) error { return nil },
	}
	root := writesEnabledTestModel(t, controller)
	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)

	updated, cmd := rootModel.Update(keyPress('y'))
	rootModel = updated.(model)
	require.NotNil(t, cmd)
	msg := cmd()
	updated, _ = rootModel.Update(msg)
	rootModel = updated.(model)

	assert.Nil(t, rootModel.application.PendingAction)
	assert.Contains(t, rootModel.notice, "1/1 succeeded")
}

func TestConfirmingRebootReportsFailureWithTargetAndError(t *testing.T) {
	controller := &testkit.FakeNodeController{
		RebootFunc: func(_ context.Context, _ string, _ ports.RebootMode) error { return errors.New("connection refused") },
	}
	root := writesEnabledTestModel(t, controller)
	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)

	updated, cmd := rootModel.Update(keyPress('y'))
	rootModel = updated.(model)
	msg := cmd()
	updated, _ = rootModel.Update(msg)
	rootModel = updated.(model)

	assert.Contains(t, rootModel.notice, "cp-1")
	assert.Contains(t, rootModel.notice, "connection refused")
}
