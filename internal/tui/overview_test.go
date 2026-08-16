package tui

import (
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestRenderOverviewSummarizesNodesAndEtcdBySeverity(t *testing.T) {
	model := application.Model{
		Nodes: application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-1", Name: "cp-1", Health: domain.HealthHealthy, Services: domain.ServiceSummary{Healthy: 7, Total: 7, Known: true}},
			{ID: "node-2", Name: "worker-1", Health: domain.HealthUnhealthy, Services: domain.ServiceSummary{Healthy: 7, Total: 7, Known: true}},
		}}},
		Etcd: application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
			{Hostname: "cp-1", MemberID: 1, StatusKnown: true},
		}}},
	}

	rendered := renderOverview(model)

	assert.Contains(t, rendered, "NODES     1/2 healthy, 0 warning, 1 critical")
	assert.Contains(t, rendered, "ETCD      1/1 healthy, 0 warning, 0 critical")
	assert.Contains(t, rendered, "worker-1")
}

func TestRenderOverviewWithNoProblems(t *testing.T) {
	model := application.Model{
		Nodes: application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-1", Name: "cp-1", Health: domain.HealthHealthy, Services: domain.ServiceSummary{Healthy: 7, Total: 7, Known: true}},
		}}},
	}

	rendered := renderOverview(model)

	assert.Contains(t, rendered, "NODES     1/1 healthy, 0 warning, 0 critical")
	assert.NotContains(t, rendered, "!", "no critical diagnoses means no preview line should be emitted")
}
