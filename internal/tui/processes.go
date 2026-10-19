package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/domain"
)

type processesModel struct {
	status    directLoadStatus
	values    domain.ProcessSet
	node      string
	err       string
	filter    string
	filtering bool
	control   listControl
}

func newDirectProcessesModel() processesModel { return processesModel{status: directLoading} }

func (m processesModel) setResult(node string, values domain.ProcessSet, err error) processesModel {
	selectedPID, hadSelection := m.selectedDirect()
	previousIndex := m.control.Cursor()
	m.node, m.values, m.err, m.status = node, values, "", directReady
	if err != nil {
		m.status = directFailed
		m.err = "Process data is unavailable"
		if len(values.Processes) > 0 {
			m.status, m.err = directPartial, "Some process data is unavailable"
		}
	}
	m = m.normalizeSelection(previousIndex)
	if hadSelection {
		for index, process := range m.visibleProcesses() {
			if process.PID == selectedPID {
				m.control.SetCursor(index)
				break
			}
		}
	}
	return m
}

func (m processesModel) selectedDirect() (int32, bool) {
	process, ok := m.selected()
	return process.PID, ok
}

func (m processesModel) startFilter(value string) processesModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(m.control.Cursor())
}

func (m processesModel) update(message tea.KeyPressMsg) processesModel {
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			return m.normalizeSelection(m.control.Cursor())
		case "enter":
			m.filtering = false
			return m
		case "backspace":
			m.filter = trimLastRune(m.filter)
			return m.normalizeSelection(m.control.Cursor())
		}
		if text := printableText(message.Text); text != "" {
			m.filter += text
			return m.normalizeSelection(m.control.Cursor())
		}
		return m
	}

	switch key {
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m = m.normalizeSelection(m.control.Cursor())
	case "up", "k":
		m.control.MoveUp(1)
	case "down", "j":
		m.control.MoveDown(1)
	case "g":
		m.control.GotoTop()
	case "G":
		m.control.GotoBottom()
	}
	return m
}

func (m processesModel) normalizeSelection(closest int) processesModel {
	processes := m.visibleProcesses()
	m.control.SetCount(len(processes))
	m.control.SetCursor(min(max(closest, 0), max(0, len(processes)-1)))
	return m
}

func (m processesModel) selected() (domain.ProcessSnapshot, bool) {
	processes := m.visibleProcesses()
	cursor := m.control.Cursor()
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
		return m.values.Processes
	}
	filtered := make([]domain.ProcessSnapshot, 0, len(m.values.Processes))
	for _, process := range m.values.Processes {
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
	contents := renderProcessTable(width, m.visibleProcesses(), m.control.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m processesModel) viewSized(size contentSize) string {
	if m.status == directFailed {
		return fallback(m.err)
	}
	processes := m.visibleProcesses()
	if len(processes) == 0 {
		if m.status == directLoading {
			return "Loading processes…"
		}
		if m.status == directFailed {
			return fallback(m.err)
		}
		if m.status == directPartial {
			return fallback(m.err)
		}
		return "No processes"
	}
	warningRows := 0
	if m.status == directPartial {
		warningRows = 1
	}
	rowCapacity := max(0, size.Height-1-warningRows)
	start, end := resourceWindow(len(processes), m.control.Cursor(), rowCapacity)
	content := renderProcessTable(size.Width, processes[start:end], m.control.Cursor()-start)
	if warningRows > 0 {
		content = "PROCESSES PARTIAL: " + fallback(m.err) + "\n" + content
	}
	return content
}

const defaultProcessesWidth = 120

var processColumns = []tableColumn[domain.ProcessSnapshot]{
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
	writeTableCells(&header, processHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, process := range processes {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeTableCells(&row, processRowValues(process), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func processColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultProcessesWidth
	}
	return calculateColumnWidths(width, processColumns)
}

func processHeaders() []string {
	return tableHeaders(processColumns)
}

func processRowValues(process domain.ProcessSnapshot) []string {
	return tableRowValues(process, processColumns)
}
