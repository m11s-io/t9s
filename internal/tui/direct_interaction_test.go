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

func TestDirectRootInitStartsSplashAndRefreshHeartbeat(t *testing.T) {
	root := newDirectModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
	command := root.Init()
	require.NotNil(t, command)
	batch, ok := command().(tea.BatchMsg)
	require.True(t, ok)
	require.Len(t, batch, 3, "startup includes context discovery, splash timer, and node refresh heartbeat")
	assert.Contains(t, root.View().Content, "Inspect Talos clusters")

	root, _ = updateDirect(root, splashDoneMsg{})
	assert.NotContains(t, root.View().Content, "Inspect Talos clusters")
}

func TestDirectRootSkipsSplashOnKeypress(t *testing.T) {
	root := newDirectModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
	root, _ = updateDirect(root, keyPress('x'))
	assert.NotContains(t, root.View().Content, "Inspect Talos clusters")
}

func TestDirectRootUsesOnlyActiveViewFilterForPalettePrecedence(t *testing.T) {
	root := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker"}})
	root.network.filtering = true // A retained filter in an inactive feature must not block global commands.
	root, _ = updateDirect(root, keyPress(':'))
	assert.True(t, root.palette.active)

	root.palette = root.palette.close()
	root.nodes.filtering = true
	for _, r := range "qrvC:" {
		root, _ = updateDirect(root, keyPress(r))
	}
	assert.False(t, root.palette.active)
	assert.Equal(t, "qrvC:", root.nodes.filter, "active filters treat command and action keys as query text")
}

func TestDirectRootHelpCommandAndChildBack(t *testing.T) {
	root := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker"}})
	root, _ = updateDirect(root, keyPress('?'))
	require.Equal(t, viewHelp, root.views.top().Kind)
	assert.Contains(t, root.View().Content, "HELP")
	root, _ = updateDirect(root, keyPress('q'))
	assert.Equal(t, viewNodes, root.views.top().Kind)

	root, _ = updateDirect(root, keyPress(':'))
	for _, r := range "help" {
		root, _ = updateDirect(root, keyPress(r))
	}
	root, command := updateDirect(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	root, _ = updateDirect(root, command())
	assert.Equal(t, viewHelp, root.views.top().Kind)
}

func TestDirectRootPromptHasPriorityOverPaletteAndGlobalNavigation(t *testing.T) {
	root := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker"}})
	prompt := newSnapshotPromptModel("worker", "worker", "/tmp/snapshot")
	root.etcdPathPrompt = &prompt
	root, _ = updateDirect(root, keyPress(':'))
	assert.False(t, root.palette.active)
	assert.Contains(t, root.etcdPathPrompt.input.Value(), ":", "the active text prompt receives input before global routing")
}

func TestDirectNodeRefreshHeartbeatReschedulesAndPausesOutsideNodeView(t *testing.T) {
	var reads atomic.Int32
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			reads.Add(1)
			return domain.NodeSet{}, nil
		}}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	root := newDirectModel(context.Background(), client, LaunchOptions{})
	root.session, root.sessionCtx = session, context.Background()
	root, command := updateDirect(root, nodeAutoRefreshTickMsg{})
	require.NotNil(t, command, "tick always reschedules the heartbeat")
	assert.Equal(t, application.Loading, root.cluster.nodes.Status)
	root.views = root.views.push(viewFrame{Kind: viewProcesses, Label: "worker > processes"})
	root.cluster.nodes.Status = application.Ready
	root, command = updateDirect(root, nodeAutoRefreshTickMsg{})
	require.NotNil(t, command)
	assert.Equal(t, application.Ready, root.cluster.nodes.Status)
	assert.Equal(t, int32(0), reads.Load(), "paused tick does not issue a node read")
}

func TestDirectNodeRefreshHeartbeatPausesDuringActiveFilterAndReschedules(t *testing.T) {
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	root := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker"}})
	root.session = session
	root.sessionCtx = context.Background()
	root.nodes.filtering = true
	root, command := updateDirect(root, nodeAutoRefreshTickMsg{})
	require.NotNil(t, command)
	assert.Equal(t, application.Ready, root.cluster.nodes.Status)
}
