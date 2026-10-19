package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestCPUViewShowsSampleIntervalCountsAndUnknownCores(t *testing.T) {
	m := newCPUModel(application.CPUState{Status: application.Partial, Value: domain.CPUSnapshot{Aggregate: domain.CPUUtilization{Known: true, BusyPercent: 30, IOWaitPercent: 10}, Cores: []domain.CPUCoreSnapshot{{ID: 2}}, Interval: time.Second, SampledAt: time.Date(2026, 10, 17, 12, 0, 1, 0, time.UTC), ProcessCountsKnown: true, RunningProcesses: 7, BlockedProcesses: 2, Problems: []string{"CPU2 unavailable"}}})
	rendered := m.viewSized(contentSize{Width: 100, Height: 20})
	assert.Contains(t, rendered, "30.0%")
	assert.Contains(t, rendered, "10.0%")
	assert.Contains(t, rendered, "1s")
	assert.Contains(t, rendered, "2026-10-17T12:00:01Z")
	assert.Contains(t, rendered, "CPU2")
	assert.Contains(t, rendered, "unknown")
	assert.Contains(t, rendered, "Running processes: 7")
	assert.Contains(t, rendered, "Blocked processes: 2")
}

func TestDirectCPUHelpFilterAndBackKeysStayScoped(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1"}})
	model.views = model.views.push(viewFrame{Kind: viewCPU, Label: "cp-1 > cpu", NodeID: "cp-id", NodeTarget: "cp-1"})
	model.cpu = newDirectCPUModel()

	model, _ = updateDirect(model, keyPress('?'))
	assert.Equal(t, viewHelp, model.views.top().Kind)
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, viewCPU, model.views.top().Kind, "Help returns to the CPU view")

	model, _ = updateDirect(model, keyPress('/'))
	model, _ = updateDirect(model, keyPress('r'))
	model, command := updateDirect(model, keyPress('q'))
	assert.Nil(t, command, "q is filter text while the filter is active")
	assert.Equal(t, viewCPU, model.views.top().Kind)
	assert.Contains(t, model.View().Content, "/rq")

	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, viewCPU, model.views.top().Kind, "Escape ends filtering without leaving CPU")
	model, _ = updateDirect(model, keyPress('q'))
	assert.Equal(t, viewNodes, model.views.top().Kind, "q leaves CPU after filtering ends")
}
