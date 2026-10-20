package tui

import (
	"context"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceRequiresTypedTargetAndCancelNeverCallsController(t *testing.T) {
	var calls atomic.Int32
	controller := &testkit.FakeNodeController{RebootFunc: func(context.Context, string, ports.RebootMode) error {
		calls.Add(1)
		return nil
	}}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1", Role: domain.NodeRoleWorker}}}, nil
			}},
			NodeController: controller,
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: true})
	require.NoError(t, err)
	defer session.Close()

	model := newDirectNodesModel(t, LaunchOptions{WritesEnabled: true}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1", Role: domain.NodeRoleWorker}})
	model.session, model.sessionCtx = session, context.Background()
	model, command := updateDirect(model, keyPress('R'))
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	assert.Contains(t, renderDirectMutationPrompt(model.mutation.prompt), "f force")

	model, _ = updateDirect(model, keyPress('f'))
	require.NotNil(t, model.forcePrompt)
	assert.Contains(t, model.View().Content, "Force can skip graceful shutdown or drain")
	model.forcePrompt.input.SetValue("wrong-node")
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Nil(t, command)
	assert.Equal(t, "target does not match the selected node", model.forcePrompt.err)
	assert.Zero(t, calls.Load())

	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Nil(t, model.forcePrompt)
	require.NotNil(t, model.mutation.prompt, "canceling the force step returns to ordinary confirmation")

	model, _ = updateDirect(model, keyPress('f'))
	model.forcePrompt.input.SetValue("worker-1")
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	assert.True(t, model.mutation.prompt.force)
	assert.Equal(t, "worker-1", model.mutation.prompt.confirm.Token)
	assert.Contains(t, renderDirectMutationPrompt(model.mutation.prompt), "interrupt workloads")

	model, command = updateDirect(model, keyPress('n'))
	assert.Nil(t, command)
	assert.Nil(t, model.mutation.prompt)
	assert.Zero(t, calls.Load(), "canceling final confirmation must not dispatch")
}

func TestForceBulkSelectionIsRefused(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.mutation.prompt = &directMutationPrompt{
		nodeKind:   application.ActionShutdown,
		assessment: application.Assessment{Targets: []string{"worker-1", "worker-2"}},
	}
	model, command := updateDirect(model, keyPress('f'))
	assert.Nil(t, command)
	assert.Nil(t, model.forcePrompt)
	assert.Contains(t, model.view.notice, "bulk force is refused")
}
