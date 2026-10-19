package tui

import (
	"context"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestResourceWindowKeepsSelectionVisible(t *testing.T) {
	start, end := resourceWindow(20, 15, 5)
	assert.Equal(t, 11, start)
	assert.Equal(t, 16, end)

	start, end = resourceWindow(3, 2, 5)
	assert.Equal(t, 0, start)
	assert.Equal(t, 3, end)
}

func TestServicesShellKeepsMovedSelectionVisible(t *testing.T) {
	services := make([]domain.ServiceSnapshot, 20)
	for index := range services {
		services[index] = domain.ServiceSnapshot{Node: "cp-1", Name: fmt.Sprintf("service-%02d", index)}
	}
	root := newDirectModel(context.Background(), nil, LaunchOptions{})
	root.splash = false
	root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	root.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: services}}
	root.services = newServicesModel().normalizeSelection(services, 0)
	root, _ = updateDirect(root, tea.WindowSizeMsg{Width: 80, Height: 8})
	for range 12 {
		root, _ = updateDirect(root, tea.KeyPressMsg{Code: tea.KeyDown})
	}

	view := root.View().Content
	assert.Contains(t, view, "cp-1")
	assert.Contains(t, view, "service-12")
	assert.NotContains(t, view, "service-00")
}

func TestResourceStatesRenderInsideContent(t *testing.T) {
	for _, test := range []struct {
		name    string
		state   domain.ServiceSet
		loading bool
		failed  bool
		err     string
		want    string
	}{
		{name: "loading", loading: true, want: "Loading services…"},
		{name: "failed", failed: true, err: "service discovery failed", want: "service discovery failed"},
		{name: "empty", want: "No services"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := newServicesModel().normalizeSelection(test.state.Services, 0)
			assert.Contains(t, model.viewSized(contentSize{Width: 80, Height: 5}, test.state, test.loading, test.failed, test.err), test.want)
		})
	}
}
