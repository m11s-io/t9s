package tui

import (
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestSyncLogBatchLeavesUnrelatedScreensUntouched(t *testing.T) {
	m := model{
		application: application.Model{
			Logs:  application.LogState{Lines: []string{"new log"}},
			Dmesg: application.DmesgState{Lines: []string{"new dmesg"}},
			Nodes: application.NodeState{Status: application.Ready},
		},
		nodes:    nodesModel{selectedID: "node"},
		logs:     newLogsModel(application.LogState{}),
		dmesg:    newDmesgModel(application.DmesgState{Lines: []string{"old dmesg"}}),
		problems: newProblemsModel(),
	}
	m = m.syncScreens(application.ServiceLogBatchLoaded{})
	assert.Equal(t, []string{"new log"}, m.logs.state.Lines)
	assert.Equal(t, []string{"old dmesg"}, m.dmesg.state.Lines)
	assert.Equal(t, "node", m.nodes.selectedID)
	assert.Empty(t, m.problems.filter)
}

func TestSyncNodesUpdatesHealthWithoutSyncingServices(t *testing.T) {
	m := model{
		application: application.Model{
			Nodes:    application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "node", Health: domain.HealthUnhealthy}}}},
			Services: application.ServiceState{Status: application.Ready},
		},
		nodes:    newNodesModel(),
		services: newServicesModel(),
		problems: newProblemsModel(),
	}
	m = m.syncScreens(application.NodesLoaded{})
	assert.Equal(t, "node", m.nodes.selectedValue(m.application.Nodes.Value.Nodes).ID)
	assert.Len(t, application.EvaluateObservations(legacyHealthInput(m.application)), 1)
	assert.Empty(t, m.services.selectedKey)
}

func TestSyncDiskActionSuccessUpdatesDiskLoadingStatusOnly(t *testing.T) {
	m := model{
		application: application.Model{Disks: application.DisksState{Status: application.Loading}},
		disks:       newDisksModel(application.DisksState{Status: application.Ready}),
		problems:    newProblemsModel(),
	}
	m = m.syncScreens(application.ActionSucceeded{})
	assert.Equal(t, application.Loading, m.disks.state.Status)
	assert.Empty(t, m.problems.filter)
}
