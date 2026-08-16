package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func healthTestModel() application.Model {
	return application.Model{
		Nodes: application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-1", Name: "cp-1", Health: domain.HealthHealthy, Services: domain.ServiceSummary{Healthy: 7, Total: 7, Known: true}},
			{ID: "node-2", Name: "worker-1", Health: domain.HealthUnhealthy, Services: domain.ServiceSummary{Healthy: 7, Total: 7, Known: true}},
		}}},
	}
}

func TestOverviewCommandShowsSeverityBreakdown(t *testing.T) {
	root := newModel(t.Context(), false, healthTestModel(), nil)
	root.splash = false

	root = enterCommand(t, root, "overview")

	assert.Equal(t, viewOverview, root.views.top().Kind)
	assert.Contains(t, root.View().Content, "worker-1")
}

func TestProblemsCommandShowsProblemTableAndDrillsIntoNode(t *testing.T) {
	root := newModel(t.Context(), false, healthTestModel(), nil)
	root.splash = false

	root = enterCommand(t, root, "problems")

	assert.Equal(t, viewProblems, root.views.top().Kind)
	assert.Contains(t, root.View().Content, "worker-1")

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, viewNodeDetail, root.views.top().Kind)
	assert.Contains(t, root.View().Content, "NODE DETAIL")
	assert.Contains(t, root.View().Content, "worker-1", "drilling in must select the diagnosed node, not whatever :nodes had selected before")
}
