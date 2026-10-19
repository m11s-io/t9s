package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectServicesUseSharedSnapshotAndPreserveSelection(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.width, model.height = 120, 40
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{
		{Node: "node-a", Name: "etcd"}, {Node: "node-a", Name: "kubelet"}, {Node: "node-b", Name: "machined"},
	}}}
	model.services = model.services.normalizeSelection(model.cluster.services.Value.Services, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyDown})
	assert.Equal(t, serviceKey(domain.ServiceSnapshot{Node: "node-a", Name: "kubelet"}), model.services.selectedKey)

	model.cluster.services.Value.Services = []domain.ServiceSnapshot{{Node: "node-a", Name: "etcd"}, {Node: "node-a", Name: "kubelet"}, {Node: "node-c", Name: "new"}}
	model.services = model.services.normalizeSelection(model.cluster.services.Value.Services, model.services.control.Cursor())
	assert.Equal(t, serviceKey(domain.ServiceSnapshot{Node: "node-a", Name: "kubelet"}), model.services.selectedKey)
	assert.Contains(t, model.View().Content, "kubelet")
}

func TestDirectServicesFilterPromptsAndDetailPaletteNavigation(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.width, model.height = 100, 28
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "node-a", Name: "etcd"}}}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	for _, key := range []tea.KeyPressMsg{keyPress('/'), keyPress('m'), {Code: tea.KeyEnter}} {
		model, _ = updateDirect(model, key)
		if key.String() == "m" {
			assert.Contains(t, model.View().Content, "/m")
			assert.True(t, model.services.filtering)
		}
	}
	assert.Contains(t, model.View().Content, "/m")
	assert.Contains(t, model.View().Content, "No services")
	assert.True(t, model.services.filtering == false)

	model.services.filter = ""
	model.services.filtering = false
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, ansi.Strip(model.View().Content), "SERVICE          etcd")
	assert.NotContains(t, model.View().Content, "SERVICE DETAIL")
	model, _ = updateDirect(model, keyPress(':'))
	assert.True(t, model.palette.active)
	for _, key := range []tea.KeyPressMsg{keyPress('s'), keyPress('v'), keyPress('c'), {Code: tea.KeyEnter}} {
		var command tea.Cmd
		model, command = updateDirect(model, key)
		if key.String() == "enter" {
			require.NotNil(t, command)
			model, _ = updateDirect(model, command())
		}
	}
	assert.Equal(t, viewServices, model.views.top().Kind)
	assert.Contains(t, model.View().Content, "SERVICE")
}

func TestDirectServicesEscapeReturnsFromDetailAndRoot(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "node-a", Name: "node-a"}}}}
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{
		{Node: "node-a", Name: "etcd", State: "Running"},
		{Node: "node-a", Name: "kubelet", State: "Running"},
	}}}
	model.services = model.services.normalizeSelection(model.cluster.services.Value.Services, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})

	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, ansi.Strip(model.View().Content), "SERVICE          etcd")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Equal(t, viewServices, model.views.top().Kind)
	assert.Contains(t, model.View().Content, "etcd", "Escape from detail returns to the rendered service list")

	model, _ = updateDirect(model, keyPress('/'))
	model, _ = updateDirect(model, keyPress('e'))
	model, _ = updateDirect(model, keyPress('t'))
	model, _ = updateDirect(model, keyPress('c'))
	model, _ = updateDirect(model, keyPress('d'))
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, "etcd", model.services.filter)
	assert.NotContains(t, model.View().Content, "kubelet")

	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Equal(t, viewServices, model.views.top().Kind, "Escape first clears an accepted filter")
	assert.Empty(t, model.services.filter)
	assert.Contains(t, model.View().Content, "kubelet", "the cleared service list is rendered")

	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Equal(t, viewServices, model.views.top().Kind, "Escape at the services root keeps the list open")
	assert.Contains(t, ansi.Strip(model.View().Content), "services(all)[2]", "the services header remains visible")
	assert.Contains(t, model.View().Content, "etcd")
	assert.Contains(t, model.View().Content, "kubelet")
}

func TestDirectServicesAliasRoutesFromPalette(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model, _ = updateDirect(model, keyPress(':'))
	var command tea.Cmd
	for _, key := range []tea.KeyPressMsg{keyPress('s'), keyPress('v'), keyPress('c'), {Code: tea.KeyEnter}} {
		model, command = updateDirect(model, key)
	}
	require.NotNil(t, command)
	message := command()
	model, _ = updateDirect(model, message)
	assert.Equal(t, viewServices, model.views.top().Kind)
}

func TestDirectServicesRefreshUsesSharedReadWithoutLegacyReducer(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.session = &application.Session{}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	model, command := updateDirect(model, keyPress('r'))
	assert.NotNil(t, command)
	assert.Contains(t, model.requests.slots, "cluster-nodes-services")
}

func TestDirectServiceDetailKeepsOpenedIdentityAcrossFilteredRefreshReorder(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	services := []domain.ServiceSnapshot{
		{Node: "node-a", Name: "alpha", State: "Running"},
		{Node: "node-b", Name: "beta-one", State: "Running"},
		{Node: "node-c", Name: "beta-two", State: "Running"},
	}
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: services}}
	model.services = newServicesModel().normalizeSelection(services, 0)
	model.services = model.services.startFilter("beta", services)
	model.services.filtering = false
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})

	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, ansi.Strip(model.View().Content), "SERVICE          beta-one")

	request, _ := model.requests.begin("cluster-nodes-services", model.generation())
	reordered := domain.ServiceSet{Services: []domain.ServiceSnapshot{services[0], services[2], services[1]}}
	model, _ = updateDirect(model, directNodeServicesLoaded{request: request.stamp, services: reordered})
	assert.Contains(t, ansi.Strip(model.View().Content), "SERVICE          beta-one")
	assert.NotContains(t, ansi.Strip(model.View().Content), "SERVICE          beta-two")
	assert.Contains(t, model.views.breadcrumb(model.cluster.contextName), "beta-one@node-b")
}

func TestDirectServiceDetailShowsUnavailableWhenOpenedServiceDisappears(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	services := []domain.ServiceSnapshot{{Node: "node-b", Name: "beta-one", State: "Running"}, {Node: "node-c", Name: "beta-two", State: "Running"}}
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: services}}
	model.services = newServicesModel().normalizeSelection(services, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})

	request, _ := model.requests.begin("cluster-nodes-services", model.generation())
	model, _ = updateDirect(model, directNodeServicesLoaded{request: request.stamp, services: domain.ServiceSet{Services: []domain.ServiceSnapshot{services[1]}}})
	assert.Contains(t, ansi.Strip(model.View().Content), "SERVICE          unavailable")
	assert.NotContains(t, ansi.Strip(model.View().Content), "SERVICE          beta-two")
}
