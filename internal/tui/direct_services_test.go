package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectServicesUseSharedSnapshotAndPreserveSelection(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
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
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
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
	assert.Contains(t, model.View().Content, "SERVICE DETAIL")
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

func TestDirectServicesAliasRoutesFromPalette(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
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
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.session = &application.Session{}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	model, command := updateDirect(model, keyPress('r'))
	assert.NotNil(t, command)
	assert.Contains(t, model.requests.slots, "cluster-nodes-services")
}
