package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func processesTestState() application.ProcessesState {
	return application.ProcessesState{
		Status: application.Ready,
		Node:   "cp-1",
		Value: domain.ProcessSet{Processes: []domain.ProcessSnapshot{
			{PID: 1, State: "Running", CPUTime: 12.3, ResidentMemory: 2097152, Command: "init"},
			{PID: 42, State: "Sleeping", CPUTime: 0.4, ResidentMemory: 1048576, Command: "kubelet"},
		}},
	}
}

func renderProcesses(width int, state application.ProcessesState) string {
	return newProcessesModel(state).view(width)
}

func TestProcessesRenderSemanticColumns(t *testing.T) {
	rendered := renderProcesses(120, processesTestState())
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t,
		[]string{"PID", "STATE", "CPU", "MEM", "COMMAND"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, ansi.Strip(lines[1]), "1")
	assert.Contains(t, ansi.Strip(lines[1]), "Running")
	assert.Contains(t, ansi.Strip(lines[1]), "12.3s")
	assert.Contains(t, ansi.Strip(lines[1]), "2.0 MiB")
	assert.Contains(t, ansi.Strip(lines[1]), "init")
}

func TestProcessesRenderEmptyState(t *testing.T) {
	model := newProcessesModel(application.ProcessesState{Status: application.Ready})
	rendered := model.viewSized(contentSize{Width: 80, Height: 10})
	assert.Equal(t, "No processes", rendered)
}

func TestProcessesFilterMatchesCommand(t *testing.T) {
	processes := newProcessesModel(processesTestState())
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('k'), keyPress('u'), keyPress('b'), keyPress('e'),
		{Code: tea.KeyEnter},
	} {
		processes = processes.update(message)
	}

	rendered := processes.view(120)

	assert.Contains(t, rendered, "kubelet")
	assert.NotContains(t, rendered, "init")
}
