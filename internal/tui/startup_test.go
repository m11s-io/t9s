package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func BenchmarkInitialView(b *testing.B) {
	factory := &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		panic("initial View opened a Talos session")
	}}
	runner := application.NewRunner(application.Dependencies{SessionFactory: factory})

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		applicationModel, _ := application.NewModel("")
		terminal := New(applicationModel, runner)
		terminal, _ = terminal.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		_ = terminal.View()
	}
}

func TestFoundationFlowRejectsOldGenerationAfterContextSwitch(t *testing.T) {
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return []domain.ClusterContext{
			{Name: "prod", Cluster: "production", Current: true},
			{Name: "dev", Cluster: "development"},
		}, nil
	}}
	factory := &testkit.FakeSessionFactory{OpenFunc: func(_ context.Context, contextName string) (ports.Session, error) {
		nodes := map[string]domain.NodeSet{
			"prod": {Nodes: []domain.NodeSnapshot{{
				ID: "prod-worker", Name: "prod-worker", Role: domain.NodeRoleWorker,
			}}},
			"dev": {Nodes: []domain.NodeSnapshot{{
				ID: "dev-worker", Name: "dev-worker", Role: domain.NodeRoleWorker,
			}}},
		}
		return &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{
			ListFunc: func(context.Context) (domain.NodeSet, error) {
				return nodes[contextName], nil
			},
		}}, nil
	}}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: catalog,
		SessionFactory: factory,
	})
	t.Cleanup(func() { require.NoError(t, runner.Close()) })

	applicationModel, _ := application.NewModel("")
	terminal := New(applicationModel, runner).(model)
	terminal, _ = updateRoot(terminal, tea.WindowSizeMsg{Width: 120, Height: 40})

	startup := terminal.Init()().(tea.BatchMsg)
	terminal, command := updateRoot(terminal, startup[0]()) // Start -> ContextsLoaded
	require.NotNil(t, command)
	terminal, command = updateRoot(terminal, command()) // SessionOpened
	require.NotNil(t, command)
	terminal, _ = updateRoot(terminal, command()) // NodesLoaded
	terminal, _ = updateRoot(terminal, splashDoneMsg{})
	oldGeneration := terminal.application.Generation
	require.Contains(t, terminal.View().Content, "prod-worker")

	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('w'), keyPress('o'), keyPress('r'), keyPress('k'), keyPress('e'), keyPress('r'),
		{Code: tea.KeyEnter},
	} {
		terminal, _ = updateRoot(terminal, message)
	}
	assert.Contains(t, terminal.View().Content, "/worker")

	terminal = enterCommand(t, terminal, "ctx")
	terminal, _ = updateRoot(terminal, tea.KeyPressMsg{Code: tea.KeyUp})
	terminal, command = updateRoot(terminal, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	terminal, command = updateRoot(terminal, command()) // SelectContext -> open session
	require.NotNil(t, command)
	terminal, command = updateRoot(terminal, command()) // SessionOpened
	require.NotNil(t, command)
	terminal, _ = updateRoot(terminal, command()) // NodesLoaded

	terminal, _ = updateRoot(terminal, applicationMessage{message: application.NodesLoaded{
		Generation: oldGeneration,
		Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{{
			ID: "stale-prod-worker", Name: "stale-prod-worker", Role: domain.NodeRoleWorker,
		}}},
	}})

	view := terminal.View().Content
	assert.Contains(t, view, "Context:")
	assert.Contains(t, view, "dev [RO]")
	assert.Contains(t, view, "dev-worker")
	assert.NotContains(t, view, "prod-worker")
	assert.NotContains(t, view, "stale-prod-worker")
}
