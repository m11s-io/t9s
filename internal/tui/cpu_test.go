package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
func TestCPUNodeKeyFilterRefreshHelpAndExit(t *testing.T) {
	root := readyRootModel()
	root.splash = false
	root, command := updateRoot(root, keyPress('u'))
	require.NotNil(t, command)
	assert.Equal(t, "cp-1", root.application.CPU.Node)
	assert.False(t, root.application.WritesEnabled)
	root, command = updateRoot(root, keyPress('r'))
	require.NotNil(t, command)
	assert.Equal(t, "cp-1", root.application.CPU.Node)
	root, _ = updateRoot(root, keyPress('?'))
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Equal(t, "cp-1", root.application.CPU.Node, "Help must retain CPU view")
	root, _ = updateRoot(root, keyPress('/'))
	root, _ = updateRoot(root, keyPress('r'))
	root, command = updateRoot(root, keyPress('q'))
	assert.Nil(t, command, "filter entry must not refresh or exit")
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Equal(t, "cp-1", root.application.CPU.Node)
	root, _ = updateRoot(root, keyPress('q'))
	assert.Equal(t, application.CPUState{}, root.application.CPU)
}
