package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/table"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type processesModel struct {
	state     application.ProcessesState
	filter    string
	filtering bool
	table     table.Model
}

func newProcessesModel(state application.ProcessesState) processesModel {
	return (processesModel{state: state, table: table.New()}).normalizeSelection(0)
}

func (m processesModel) setState(state application.ProcessesState) processesModel {
	previousIndex := m.table.Cursor()
	m.state = state
	return m.normalizeSelection(previousIndex)
}

func (m processesModel) startFilter(value string) processesModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(m.table.Cursor())
}

func (m processesModel) update(message tea.KeyPressMsg) processesModel {
	key := message.Keystroke()
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			return m.normalizeSelection(m.table.Cursor())
		case "enter":
			m.filtering = false
			return m
		case "backspace":
			m.filter = trimLastRune(m.filter)
			return m.normalizeSelection(m.table.Cursor())
		}
		if text := printableText(message.Text); text != "" {
			m.filter += text
			return m.normalizeSelection(m.table.Cursor())
		}
		return m
	}

	switch key {
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m = m.normalizeSelection(m.table.Cursor())
	case "up", "k":
		m.table.MoveUp(1)
	case "down", "j":
		m.table.MoveDown(1)
	case "g":
		m.table.GotoTop()
	case "G":
		m.table.GotoBottom()
	}
	return m
}

func (m processesModel) normalizeSelection(closest int) processesModel {
	processes := m.visibleProcesses()
	m.table.SetRows(make([]table.Row, len(processes)))
	m.table.SetCursor(min(max(closest, 0), max(0, len(processes)-1)))
	return m
}

func (m processesModel) selected() (domain.ProcessSnapshot, bool) {
	processes := m.visibleProcesses()
	cursor := m.table.Cursor()
	if len(processes) == 0 || cursor < 0 || cursor >= len(processes) {
		return domain.ProcessSnapshot{}, false
	}
	return processes[cursor], true
}

func (m processesModel) selectedValue() domain.ProcessSnapshot {
	process, _ := m.selected()
	return process
}

func (m processesModel) visibleProcesses() []domain.ProcessSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.state.Value.Processes
	}
	filtered := make([]domain.ProcessSnapshot, 0, len(m.state.Value.Processes))
	for _, process := range m.state.Value.Processes {
		values := []string{process.Command, process.State, process.Executable}
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), query) {
				filtered = append(filtered, process)
				break
			}
		}
	}
	return filtered
}

func (m processesModel) view(width int) string {
	contents := renderProcessTable(width, m.visibleProcesses(), m.table.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m processesModel) viewSized(size contentSize) string {
	if m.state.Status == application.Failed {
		return fallback(m.state.Err)
	}
	processes := m.visibleProcesses()
	if len(processes) == 0 {
		if m.state.Status == application.Loading || m.state.Status == application.Idle {
			return "Loading processes…"
		}
		return "No processes"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(processes), m.table.Cursor(), rowCapacity)
	return renderProcessTable(size.Width, processes[start:end], m.table.Cursor()-start)
}

const defaultProcessesWidth = 120

type processColumn struct {
	header   string
	minWidth int
	value    func(domain.ProcessSnapshot) string
	grow     bool
}

var processColumns = []processColumn{
	{header: "PID", minWidth: 8, value: func(p domain.ProcessSnapshot) string { return fmt.Sprintf("%d", p.PID) }},
	{header: "STATE", minWidth: 10, value: func(p domain.ProcessSnapshot) string { return fallback(p.State) }},
	{header: "CPU", minWidth: 8, value: func(p domain.ProcessSnapshot) string { return fmt.Sprintf("%.1fs", p.CPUTime) }},
	{header: "MEM", minWidth: 10, value: func(p domain.ProcessSnapshot) string { return formatBytes(int64(p.ResidentMemory)) }},
	{header: "COMMAND", minWidth: 20, grow: true, value: func(p domain.ProcessSnapshot) string { return fallback(p.Command) }},
}

func renderProcessTable(width int, processes []domain.ProcessSnapshot, selectedIndex int) string {
	widths := processColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeProcessCells(&header, processHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, process := range processes {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeProcessCells(&row, processRowValues(process), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func processColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultProcessesWidth
	}
	widths := make([]int, len(processColumns))
	used := selectionWidth + columnSpacing*(len(processColumns)-1)
	growIndex := 0
	for index, column := range processColumns {
		widths[index] = column.minWidth
		used += column.minWidth
		if column.grow {
			growIndex = index
		}
	}
	if remaining := width - used; remaining > 0 {
		widths[growIndex] += remaining
	}
	return widths
}

func processHeaders() []string {
	headers := make([]string, len(processColumns))
	for index, column := range processColumns {
		headers[index] = column.header
	}
	return headers
}

func processRowValues(process domain.ProcessSnapshot) []string {
	values := make([]string, len(processColumns))
	for index, column := range processColumns {
		values[index] = column.value(process)
	}
	return values
}

func writeProcessCells(output *strings.Builder, values []string, widths []int) {
	for index, value := range values {
		if index > 0 {
			output.WriteByte(' ')
		}
		cell := ansi.Truncate(value, widths[index], "…")
		output.WriteString(cell)
		if padding := widths[index] - ansi.StringWidth(cell); padding > 0 && index < len(values)-1 {
			output.WriteString(strings.Repeat(" ", padding))
		}
	}
}
