package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/table"

	"github.com/m11s-io/t9s/internal/domain"
)

type problemsModel struct {
	diagnoses []domain.Diagnosis
	filter    string
	filtering bool
	table     table.Model
}

func newProblemsModel(diagnoses []domain.Diagnosis) problemsModel {
	return (problemsModel{diagnoses: diagnoses, table: table.New()}).normalizeSelection(0)
}

func (m problemsModel) setDiagnoses(diagnoses []domain.Diagnosis) problemsModel {
	previousIndex := m.table.Cursor()
	m.diagnoses = diagnoses
	return m.normalizeSelection(previousIndex)
}

func (m problemsModel) startFilter(value string) problemsModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(m.table.Cursor())
}

func (m problemsModel) update(message tea.KeyPressMsg) problemsModel {
	key := message.Keystroke()
	// Terminals using the Kitty keyboard protocol (e.g. Ghostty) report
	// Shift+g as the base lowercase key plus a separate Shift modifier, so
	// Keystroke() yields "shift+g" instead of "G". message.Text is always
	// correctly-cased regardless of protocol; normalize once here so the
	// "G" case below matches under both encodings.
	if key == "shift+g" || message.Text == "G" {
		key = "G"
	}
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

func (m problemsModel) normalizeSelection(closest int) problemsModel {
	diagnoses := m.visibleDiagnoses()
	m.table.SetRows(make([]table.Row, len(diagnoses)))
	m.table.SetCursor(min(max(closest, 0), max(0, len(diagnoses)-1)))
	return m
}

func (m problemsModel) selected() (domain.Diagnosis, bool) {
	diagnoses := m.visibleDiagnoses()
	cursor := m.table.Cursor()
	if len(diagnoses) == 0 || cursor < 0 || cursor >= len(diagnoses) {
		return domain.Diagnosis{}, false
	}
	return diagnoses[cursor], true
}

func (m problemsModel) selectedValue() domain.Diagnosis {
	diagnosis, _ := m.selected()
	return diagnosis
}

func (m problemsModel) visibleDiagnoses() []domain.Diagnosis {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.diagnoses
	}
	filtered := make([]domain.Diagnosis, 0, len(m.diagnoses))
	for _, diagnosis := range m.diagnoses {
		values := []string{diagnosis.ResourceName, diagnosis.ResourceKind, diagnosis.Summary}
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), query) {
				filtered = append(filtered, diagnosis)
				break
			}
		}
	}
	return filtered
}

func (m problemsModel) view(width int) string {
	contents := renderProblemTable(width, m.visibleDiagnoses(), m.table.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m problemsModel) viewSized(size contentSize) string {
	diagnoses := m.visibleDiagnoses()
	if len(diagnoses) == 0 {
		return "No problems"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(diagnoses), m.table.Cursor(), rowCapacity)
	return renderProblemTable(size.Width, diagnoses[start:end], m.table.Cursor()-start)
}

const defaultProblemsWidth = 120

type problemColumn struct {
	header   string
	minWidth int
	value    func(domain.Diagnosis) string
	grow     bool
}

var problemColumns = []problemColumn{
	{header: "SEVERITY", minWidth: 8, value: func(d domain.Diagnosis) string { return d.Severity.String() }},
	{header: "KIND", minWidth: 12, value: func(d domain.Diagnosis) string { return fallback(d.ResourceKind) }},
	{header: "RESOURCE", minWidth: 16, value: func(d domain.Diagnosis) string { return fallback(d.ResourceName) }},
	{header: "SUMMARY", minWidth: 20, grow: true, value: func(d domain.Diagnosis) string { return fallback(d.Summary) }},
}

func renderProblemTable(width int, diagnoses []domain.Diagnosis, selectedIndex int) string {
	widths := problemColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeProblemCells(&header, problemHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, diagnosis := range diagnoses {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeProblemCells(&row, problemRowValues(diagnosis), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func problemColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultProblemsWidth
	}
	widths := make([]int, len(problemColumns))
	used := selectionWidth + columnSpacing*(len(problemColumns)-1)
	growIndex := 0
	for index, column := range problemColumns {
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

func problemHeaders() []string {
	headers := make([]string, len(problemColumns))
	for index, column := range problemColumns {
		headers[index] = column.header
	}
	return headers
}

func problemRowValues(diagnosis domain.Diagnosis) []string {
	values := make([]string, len(problemColumns))
	for index, column := range problemColumns {
		values[index] = column.value(diagnosis)
	}
	return values
}

func writeProblemCells(output *strings.Builder, values []string, widths []int) {
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
