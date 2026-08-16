package tui

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextCommandShowsSortedSanitizedDomainContexts(t *testing.T) {
	root := commandRootWithContexts(t)
	root = enterCommand(t, root, "contexts")

	view := root.View().Content
	assert.Less(t, indexOf(t, view, "dev"), indexOf(t, view, "prod(*)"))
	assert.Contains(t, view, "development")
	assert.Contains(t, view, "production")
}

func TestContextCommandReplacesAFullResourceContentRegion(t *testing.T) {
	root := commandRootWithContexts(t)
	services := make([]domain.ServiceSnapshot, 30)
	for index := range services {
		services[index] = domain.ServiceSnapshot{Node: "cp-1", Name: fmt.Sprintf("service-%02d", index)}
	}
	root.application.Services = application.ServiceState{
		Status: application.Ready,
		Value:  domain.ServiceSet{Services: services},
	}
	root.services = newServicesModel(root.application.Services)
	root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	root.width, root.height, root.splash = 80, 24, false

	root = enterCommand(t, root, "ctx")

	view := root.View().Content
	assert.Contains(t, view, "development")
	assert.Contains(t, view, "production")
	assert.NotContains(t, view, "service-00", "the selector owns the content region while active")
	assert.Contains(t, view, "contexts(all)[2]")
	assert.Contains(t, view, "NAME")
	assert.Contains(t, view, "CLUSTER")
	assert.Contains(t, view, "ENDPOINTS")
	assert.Contains(t, view, "NODES")
	assert.Contains(t, view, "prod(*)")
	assert.NotContains(t, view, "<l> Logs")
	assert.NotContains(t, view, "<d> Detail")
}

func TestContextScreenMarksTheApplicationContextNotTheConfigDefault(t *testing.T) {
	contexts := newContextsModel([]domain.ClusterContext{
		{Name: "mgmt", Current: true}, {Name: "test"},
	}, "test")

	view := contexts.view()
	assert.Contains(t, view, "test(*)")
	assert.NotContains(t, view, "mgmt(*)")
}

func TestContextSelectionEmitsSelectContext(t *testing.T) {
	root := commandRootWithContexts(t)
	root = enterCommand(t, root, "ctx")

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyUp})
	_, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	require.NotNil(t, command)
	message := command()
	assert.Equal(t, applicationMessage{message: application.SelectContext{Name: "dev"}}, message)
}

func TestContextSelectionClampsAtFirstAndLastRow(t *testing.T) {
	root := commandRootWithContexts(t)
	root = enterCommand(t, root, "ctx")
	for range 5 {
		root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyUp})
	}
	_, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	assert.Equal(t, applicationMessage{message: application.SelectContext{Name: "dev"}}, command(),
		"cursor must clamp at the first row (index 0, \"dev\"), not go negative")

	threeContexts := newModel(t.Context(), false, application.Model{
		Route:       application.RouteNodes,
		ContextName: "alpha",
		Contexts: []domain.ClusterContext{
			{Name: "alpha", Cluster: "a", Current: true},
			{Name: "beta", Cluster: "b"},
			{Name: "gamma", Cluster: "c"},
		},
	}, nil)
	threeContexts = enterCommand(t, threeContexts, "ctx")
	for range 5 {
		threeContexts, _ = updateRoot(threeContexts, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	_, command = updateRoot(threeContexts, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	assert.Equal(t, applicationMessage{message: application.SelectContext{Name: "gamma"}}, command(),
		"cursor must clamp at the last row (index 2, \"gamma\"), not overflow")
}

func TestContextEscapePreservesCurrentContext(t *testing.T) {
	root := commandRootWithContexts(t)
	root = enterCommand(t, root, "contexts")
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyUp})

	root, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})

	assert.Nil(t, command)
	assert.Equal(t, "prod", root.application.ContextName)
	assert.NotContains(t, root.View().Content, "contexts(all)")
}

func TestControlCQuitsWhileContextOverlayIsActive(t *testing.T) {
	root := commandRootWithContexts(t)
	root = enterCommand(t, root, "contexts")

	_, command := updateRoot(root, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	require.NotNil(t, command)
	assert.IsType(t, tea.QuitMsg{}, command())
}

func TestControlCQuitsWhileCommandPaletteIsActive(t *testing.T) {
	root := commandRootWithContexts(t)
	root, _ = updateRoot(root, keyPress(':'))

	_, command := updateRoot(root, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	require.NotNil(t, command)
	assert.IsType(t, tea.QuitMsg{}, command())
}

func commandRootWithContexts(t *testing.T) model {
	t.Helper()
	return newModel(t.Context(), false, application.Model{
		Route:       application.RouteNodes,
		ContextName: "prod",
		Contexts: []domain.ClusterContext{
			{Name: "prod", Cluster: "production", Current: true},
			{Name: "dev", Cluster: "development"},
		},
	}, nil)
}

func enterCommand(t *testing.T, root model, value string) model {
	t.Helper()
	root, _ = updateRoot(root, keyPress(':'))
	for _, character := range value {
		root, _ = updateRoot(root, keyPress(character))
	}
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	return root
}

func TestEventsCommandShowsEventTable(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Events: application.EventState{
		Status: application.Ready,
		Value: domain.EventSet{Events: []domain.EventSnapshot{
			{Node: "cp-1", Kind: "Task", Message: "install START", ObservedAt: time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)},
		}},
	}}, nil)
	root.splash = false

	root = enterCommand(t, root, "events")

	assert.Equal(t, viewEvents, root.views.top().Kind)
	assert.Contains(t, root.View().Content, "install START")
}

func TestEtcdCommandShowsEtcdTable(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Etcd: application.EtcdState{
		Status: application.Ready,
		Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
			{Hostname: "cp-1", MemberID: 1, IsLeader: true, StatusKnown: true, DBSize: 1024, RaftIndex: 10},
		}},
	}}, nil)
	root.splash = false

	root = enterCommand(t, root, "etcd")

	assert.Equal(t, viewEtcd, root.views.top().Kind)
	assert.Contains(t, root.View().Content, "cp-1")
	assert.Contains(t, root.View().Content, "Leader")
}

func indexOf(t *testing.T, value, fragment string) int {
	t.Helper()
	index := -1
	for position := range value {
		if len(value[position:]) >= len(fragment) && value[position:position+len(fragment)] == fragment {
			index = position
			break
		}
	}
	require.NotEqual(t, -1, index)
	return index
}
