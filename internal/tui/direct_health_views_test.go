package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func directHealthModel() directModel {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.cluster.contextName = "prod"
	model.cluster.contexts = []domain.ClusterContext{{Name: "prod", Cluster: "production", Endpoints: []string{"10.0.0.1"}}}
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{ID: "node-1", Name: "cp-1", Health: domain.HealthHealthy, Services: domain.ServiceSummary{Healthy: 7, Total: 7, Known: true}, Version: "v1.2"},
		{ID: "node-2", Name: "worker-1", Health: domain.HealthUnhealthy, Stage: "maintenance", Problem: "rebooting", Services: domain.ServiceSummary{Known: true}, Version: "v1.2"},
	}}}
	model.cluster.etcd = directEtcdState{Status: directPartial, Err: "Some etcd members are unavailable", Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1", MemberID: 1, StatusKnown: true},
		{Hostname: "cp-2", MemberID: 2, StatusKnown: false},
	}}}
	model.nodes = model.nodes.normalizeSelection(model.cluster.nodes.Value.Nodes, 0)
	model.etcd = model.etcd.normalizeSelection(model.cluster.etcd.Value.Members, 0)
	return model
}

func TestDirectOverviewProjectsKnownPartialHealthWithoutFacade(t *testing.T) {
	model := directHealthModel()
	model, _ = runDirectPalette(t, model, "ov")

	content := ansi.Strip(model.View().Content)
	assert.Equal(t, viewOverview, model.views.top().Kind)
	assert.Contains(t, content, "NODES     1/2 healthy, 0 warning, 1 critical")
	assert.Contains(t, content, "ETCD      1/2 healthy, 0 warning, 0 critical")
	assert.Contains(t, content, "worker-1")
	assert.Contains(t, content, "Degraded")
	assert.Contains(t, content, "ETCD PARTIAL")
}

func TestDirectProblemsFilterAndDrillIntoDiagnosedStableNode(t *testing.T) {
	model := directHealthModel()
	model, _ = runDirectPalette(t, model, "problems")
	assert.Equal(t, viewProblems, model.views.top().Kind)
	assert.Contains(t, ansi.Strip(model.View().Content), "cp-2")
	assert.Contains(t, ansi.Strip(model.View().Content), "unknown")
	assert.Contains(t, ansi.Strip(model.View().Content), "ETCD PARTIAL")

	model, _ = updateDirect(model, keyPress('/'))
	for _, key := range []rune("worker") {
		model, _ = updateDirect(model, keyPress(key))
	}
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, ansi.Strip(model.View().Content), "/worker")
	assert.NotContains(t, ansi.Strip(model.View().Content), "cp-2")

	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model.nodes = model.nodes.startFilter("cp-1", model.cluster.nodes.Value.Nodes)
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, viewNodeDetail, model.views.top().Kind)
	assert.Equal(t, "node-2", model.nodes.selectedID)
	assert.Contains(t, ansi.Strip(model.View().Content), "worker-1")
}

func TestDirectProblemsEtcdDiagnosisSelectsExactMember(t *testing.T) {
	model := directHealthModel()
	model, _ = runDirectPalette(t, model, "problems")
	for _, key := range []rune{'j', 'j'} {
		model, _ = updateDirect(model, keyPress(key))
	}
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, viewEtcd, model.views.top().Kind)
	assert.Equal(t, etcdMemberKey(model.cluster.etcd.Value.Members[1]), model.etcd.selectedKey)
	assert.Contains(t, ansi.Strip(model.View().Content), "cp-2")
}

func TestDirectProblemsKeepsFilteredDiagnosisTargetAcrossRefreshReorder(t *testing.T) {
	model := directHealthModel()
	model.cluster.nodes.Value.Nodes = append(model.cluster.nodes.Value.Nodes,
		domain.NodeSnapshot{ID: "node-3", Name: "worker-2", Health: domain.HealthUnhealthy})
	model, _ = runDirectPalette(t, model, "problems")
	model, _ = updateDirect(model, keyPress('/'))
	for _, key := range []rune("worker-2") {
		model, _ = updateDirect(model, keyPress(key))
	}
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})

	request, _ := model.requests.begin("cluster-nodes-services", model.generation())
	refreshed := []domain.NodeSnapshot{
		{ID: "node-3", Name: "worker-2", Health: domain.HealthUnhealthy},
		{ID: "node-1", Name: "cp-1", Health: domain.HealthHealthy},
		{ID: "node-2", Name: "worker-1", Health: domain.HealthUnhealthy},
	}
	model, _ = updateDirect(model, directNodeServicesLoaded{request: request.stamp, nodes: domain.NodeSet{Nodes: refreshed}})
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, viewNodeDetail, model.views.top().Kind)
	assert.Equal(t, "node-3", model.nodes.selectedID)
	assert.Contains(t, ansi.Strip(model.View().Content), "worker-2")
}

func TestDirectMetadataUsesSharedHealthProjectionForReadyUnhealthyNode(t *testing.T) {
	model := directHealthModel()
	header := ansi.Strip(model.View().Content)
	assert.Contains(t, header, "Degraded")
	assert.Contains(t, header, "1/2")
	assert.Contains(t, header, "Talos Rev: v1.2")
}

func TestDirectProblemsBottomSelectionVisibleWithPartialOrUnavailableNotice(t *testing.T) {
	for _, status := range []directLoadStatus{directPartial, directFailed} {
		t.Run(string(status), func(t *testing.T) {
			model := newDirectModel(context.Background(), nil, LaunchOptions{})
			model.splash = false
			model.width, model.height = 120, 16
			nodes := make([]domain.NodeSnapshot, 10)
			for index := range nodes {
				nodes[index] = domain.NodeSnapshot{
					ID: fmt.Sprintf("node-%02d", index), Name: fmt.Sprintf("worker-%02d", index), Health: domain.HealthUnhealthy,
				}
			}
			model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: nodes}}
			model.cluster.etcd = directEtcdState{Status: status, Err: "Etcd data is unavailable"}
			model, _ = runDirectPalette(t, model, "problems")
			for range 9 {
				model, _ = updateDirect(model, keyPress('j'))
			}

			layout := layoutShell(model.width, model.height)
			lines := strings.Split(model.View().Content, "\n")
			visibleContent := strings.Join(lines[layout.HeaderHeight:layout.HeaderHeight+layout.ContentHeight], "\n")
			assert.Equal(t, 9, model.problems.control.Cursor())
			assert.Contains(t, visibleContent, "worker-09", "the selected diagnosis must remain in the actual content viewport")

			model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
			assert.Equal(t, "node-09", model.nodes.selectedID)
		})
	}
}

func TestDirectProblemsSelectionResetsAcrossContextsWithSameDiagnosisIdentity(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	nodes := []domain.NodeSnapshot{
		{ID: "node-a", Name: "worker-a", Health: domain.HealthUnhealthy},
		{ID: "node-b", Name: "worker-b", Health: domain.HealthUnhealthy},
	}
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: nodes}}
	model.cluster.etcd = directEtcdState{Status: directReady}
	model, _ = runDirectPalette(t, model, "problems")
	model, _ = updateDirect(model, keyPress('j'))
	assert.Equal(t, "node\x00node-b\x00node-readiness", model.problems.selectedKey)

	model.beginSessionOpen("staging")
	assert.Empty(t, model.problems.selectedKey)
	assert.Equal(t, 0, model.problems.control.Cursor())
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: nodes}}
	model.cluster.etcd = directEtcdState{Status: directReady}
	model, _ = runDirectPalette(t, model, "problems")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, viewNodeDetail, model.views.top().Kind)
	assert.Equal(t, "node-a", model.nodes.selectedID)
}
