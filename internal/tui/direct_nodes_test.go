package tui

import (
	"context"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectNodesFilterUsesCurrentSharedSnapshot(t *testing.T) {
	model := newModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
	model.splash = false
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{ID: "cp-id", Name: "control-plane"},
		{ID: "worker-id", Name: "worker-1"},
	}}}
	request, _ := model.requests.begin("cluster-nodes-services", model.generation())
	model, _ = updateDirect(model, directNodeServicesLoaded{request: request.stamp, nodes: model.cluster.nodes.Value})

	model, _ = updateDirect(model, keyPress('/'))
	for _, character := range "worker" {
		model, _ = updateDirect(model, keyPress(character))
	}

	content := ansi.Strip(model.View().Content)
	assert.Contains(t, content, "worker-1")
	assert.NotContains(t, content, "control-plane")
	assert.Contains(t, content, "/worker", "the active filter query stays visible in the footer")

	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, ansi.Strip(model.View().Content), "/worker", "the accepted filter remains visible")
}

func TestDirectNodesShowsUnmatchedFilterQuery(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{
		{ID: "worker-id", Name: "worker-1"},
	})
	model, _ = updateDirect(model, keyPress('/'))
	for _, character := range "missing" {
		model, _ = updateDirect(model, keyPress(character))
	}

	content := ansi.Strip(model.View().Content)
	assert.Contains(t, content, "No nodes")
	assert.Contains(t, content, "/missing", "the query remains visible when it matches no rows")
}

func TestDirectNodeDetailPaletteRoutesContextsAndNodes(t *testing.T) {
	for _, value := range []string{"ctx", "nodes"} {
		t.Run(value, func(t *testing.T) {
			model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker-1"}})
			model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
			require.Equal(t, viewNodeDetail, model.views.top().Kind)

			model, _ = updateDirect(model, keyPress(':'))
			require.True(t, model.palette.active, "the command palette opens from node detail")
			for _, character := range value {
				model, _ = updateDirect(model, keyPress(character))
			}
			model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
			require.NotNil(t, command)
			model, _ = updateDirect(model, command())

			if value == "ctx" {
				assert.True(t, model.contexts.active)
			} else {
				assert.Equal(t, viewNodes, model.views.top().Kind)
			}
		})
	}
}

func TestDirectNodesEnterShowsSelectedNodeDetailAndEscapeReturns(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{
		{ID: "cp-id", Name: "control-plane"},
		{ID: "worker-id", Name: "worker-1", Kubernetes: domain.KubernetesUnknown},
	})
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyDown})
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})

	require.Nil(t, command)
	detail := model.View().Content
	assert.Contains(t, detail, "\x1b[38;2;0;255;255m", "detail content should use the same blue as other resource rows")
	assert.Contains(t, ansi.Strip(detail), "│ NAME", "the first field should identify the selected node inside its frame")
	assert.NotContains(t, ansi.Strip(detail), "NODE DETAIL")
	assert.Contains(t, ansi.Strip(detail), "NAME             worker-1")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.NotContains(t, ansi.Strip(model.View().Content), "NAME             worker-1", "Escape from node detail returns to nodes")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model, _ = updateDirect(model, keyPress('q'))
	assert.NotContains(t, ansi.Strip(model.View().Content), "NAME             worker-1")
	assert.Equal(t, viewNodes, model.views.top().Kind, "q from node detail returns to nodes")
}

func TestDirectNodeDetailBreadcrumbIncludesContextAndNodes(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}})
	model.cluster.contextName = "prod"
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, "prod > nodes > cp-1", model.views.breadcrumb(model.cluster.contextName))
	assert.Contains(t, ansi.Strip(model.View().Content), "<cp-1>", "the footer renders the active breadcrumb segment")
}

func TestDirectNodesMarksRowsByStableIDWhenWritesEnabled(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{WritesEnabled: true}, []domain.NodeSnapshot{
		{ID: "cp-id", Name: "control-plane"},
		{ID: "worker-id", Name: "worker-1"},
	})
	model, _ = updateDirect(model, keyPress(' '))

	assert.True(t, model.nodes.isMarked("cp-id"))
	assert.False(t, model.nodes.isMarked("worker-id"))
}

func TestDirectNodesRefreshKeyReloadsSnapshotAndKeepsSelection(t *testing.T) {
	var reads atomic.Int32
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				reads.Add(1)
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}, {ID: "cp-id", Name: "control-plane"}}}, nil
			}},
			ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
				return domain.ServiceSet{}, nil
			}},
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	defer session.Close()

	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{
		{ID: "cp-id", Name: "control-plane"}, {ID: "worker-id", Name: "worker-1"},
	})
	model.session = session
	model.sessionCtx = context.Background()
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyDown})
	model, command := updateDirect(model, keyPress('r'))
	model, _ = applyDirectCommand(t, model, command)

	assert.Equal(t, int32(1), reads.Load())
	assert.Equal(t, "worker-id", model.nodes.selectedID)
	assert.Equal(t, "worker-id", model.nodes.selectedValue(model.cluster.nodes.Value.Nodes).ID)
}

func TestDirectNodeFocusResolvesNameAndAddressToStableIDOnce(t *testing.T) {
	for _, focus := range []string{"worker-1", "10.0.0.2"} {
		t.Run(focus, func(t *testing.T) {
			model := newModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{NodeFocus: focus})
			model.splash = false
			first := domain.NodeSet{Nodes: []domain.NodeSnapshot{
				{ID: "cp-id", Name: "control-plane", Addresses: []string{"10.0.0.1"}},
				{ID: "worker-id", Name: "worker-1", Addresses: []string{"10.0.0.2"}},
			}}
			request, _ := model.requests.begin("cluster-nodes-services", model.generation())
			model, _ = updateDirect(model, directNodeServicesLoaded{request: request.stamp, nodes: first})
			require.Equal(t, "worker-id", model.nodes.selectedID)
			require.Equal(t, viewNodeDetail, model.views.top().Kind, "a matching initial NodeFocus opens node detail")
			assert.Contains(t, ansi.Strip(model.View().Content), "NAME             worker-1")

			model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
			require.Equal(t, viewNodes, model.views.top().Kind, "Escape returns to the node list")

			model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyUp})
			refreshed := domain.NodeSet{Nodes: []domain.NodeSnapshot{first.Nodes[1], first.Nodes[0]}}
			request, _ = model.requests.begin("cluster-nodes-services", model.generation())
			model, _ = updateDirect(model, directNodeServicesLoaded{request: request.stamp, nodes: refreshed})
			assert.Equal(t, "cp-id", model.nodes.selectedID, "a later refresh must preserve the user's current selection")
			assert.Equal(t, viewNodes, model.views.top().Kind, "a later refresh must not reopen matched NodeFocus")
		})
	}
}

func TestDirectNodeFocusWithNoMatchLeavesNodesShowingNormally(t *testing.T) {
	model := newModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{NodeFocus: "missing-node"})
	model.splash = false
	request, _ := model.requests.begin("cluster-nodes-services", model.generation())
	model, _ = updateDirect(model, directNodeServicesLoaded{request: request.stamp, nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}}}})

	assert.Equal(t, viewNodes, model.views.top().Kind)
	assert.Contains(t, ansi.Strip(model.View().Content), "worker-1")
	assert.NotContains(t, ansi.Strip(model.View().Content), "KUBERNETES")
}

func TestDirectNodeFocusAcceptsStableID(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{NodeFocus: "worker-id"}, []domain.NodeSnapshot{
		{ID: "cp-id", Name: "control-plane"},
		{ID: "worker-id", Name: "worker-1"},
	})
	assert.Equal(t, "worker-id", model.nodes.selectedID)
}

func updateDirect(model model, message tea.Msg) (model, tea.Cmd) {
	updated, command := model.Update(message)
	return modelFromTea(updated), command
}

func modelFromTea(value tea.Model) model { return value.(model) }

func newDirectNodesModel(t *testing.T, options LaunchOptions, nodes []domain.NodeSnapshot) model {
	t.Helper()
	model := newModel(context.Background(), application.NewClient(application.Dependencies{}), options)
	model.splash = false
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: nodes}}
	request, _ := model.requests.begin("cluster-nodes-services", model.generation())
	model, _ = updateDirect(model, directNodeServicesLoaded{request: request.stamp, nodes: model.cluster.nodes.Value})
	return model
}
