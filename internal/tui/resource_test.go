package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
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
	root := readyRootModel()
	root.splash = false
	root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	root.services = newServicesModel(application.ServiceState{Status: application.Ready, Value: domain.ServiceSet{Services: services}})
	updated, _ := root.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	root = updated.(model)
	for range 12 {
		updated, _ = root.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		root = updated.(model)
	}

	view := root.View().Content
	assert.Contains(t, view, "cp-1")
	assert.Contains(t, view, "service-12")
	assert.NotContains(t, view, "service-00")
}

func TestResourceStatesRenderInsideContent(t *testing.T) {
	for _, test := range []struct {
		name  string
		state application.ServiceState
		want  string
	}{
		{name: "loading", state: application.ServiceState{Status: application.Loading}, want: "Loading services…"},
		{name: "failed", state: application.ServiceState{Status: application.Failed, Err: "service discovery failed"}, want: "service discovery failed"},
		{name: "empty", state: application.ServiceState{Status: application.Ready}, want: "No services"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := newServicesModel(test.state)
			assert.Contains(t, model.viewSized(contentSize{Width: 80, Height: 5}), test.want)
		})
	}
}
