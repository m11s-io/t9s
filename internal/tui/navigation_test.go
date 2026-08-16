package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewStackNavigationAndBreadcrumbs(t *testing.T) {
	stack := newViewStack(viewFrame{Kind: viewNodes, Label: "nodes"})
	stack = stack.push(viewFrame{Kind: viewNodeDetail, Label: "cp-1"})
	assert.Equal(t, "prod > nodes > cp-1", stack.breadcrumb("prod"))

	stack, popped := stack.pop()
	assert.True(t, popped)
	assert.Equal(t, viewNodes, stack.top().Kind)

	stack, popped = stack.pop()
	assert.False(t, popped)
	assert.Equal(t, viewNodes, stack.top().Kind)

	stack = stack.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	require.Len(t, stack, 1)
	assert.Equal(t, "prod > services", stack.breadcrumb("prod"))
}

func TestK9sKeyPrecedenceAtRootAndChild(t *testing.T) {
	root := readyRootModel()
	root.splash = false

	updated, command := root.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Nil(t, command)
	assert.Equal(t, viewNodes, updated.(model).views.top().Kind)

	root = updated.(model)
	root.views = root.views.push(viewFrame{Kind: viewNodeDetail, Label: "cp-1"})
	updated, command = root.Update(keyPress('q'))
	assert.Nil(t, command)
	assert.Equal(t, viewNodes, updated.(model).views.top().Kind)
}

func TestFilterConsumesQBeforeViewNavigation(t *testing.T) {
	root := readyRootModel()
	root.splash = false
	root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	root.services = newServicesModel(application.ServiceState{Status: application.Ready, Value: domain.ServiceSet{
		Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "etcd"}},
	}})

	updated, _ := root.Update(keyPress('/'))
	updated, command := updated.(model).Update(keyPress('q'))

	assert.Nil(t, command)
	assert.Equal(t, viewServices, updated.(model).views.top().Kind)
	assert.Contains(t, updated.(model).View().Content, "/q")
}

func TestResourceEndpointKeysAndContextualHints(t *testing.T) {
	root := readyRootModel()
	root.splash = false
	root.nodes = newNodesModel(application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{ID: "one", Name: "one"}, {ID: "two", Name: "two"}, {ID: "three", Name: "three"},
	}}})

	updated, _ := root.Update(keyPress('G'))
	assert.Equal(t, "three", updated.(model).nodes.selectedValue().ID)
	updated, _ = updated.(model).Update(keyPress('g'))
	assert.Equal(t, "one", updated.(model).nodes.selectedValue().ID)

	view := ansi.Strip(updated.(model).View().Content)
	assert.Contains(t, view, "<?> Help")
	assert.Contains(t, view, "<:> Command")
	assert.Contains(t, view, "</> Filter")
}
