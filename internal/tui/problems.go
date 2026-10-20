package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/domain"
)

type problemsModel struct {
	changes     tableChanges
	filter      string
	filtering   bool
	selectedKey string
	control     listControl
}

func newProblemsModel() problemsModel { return problemsModel{} }

func (m problemsModel) startFilter(value string, all []domain.Diagnosis) problemsModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(m.control.Cursor(), all)
}

func (m problemsModel) update(message tea.KeyPressMsg, all []domain.Diagnosis) problemsModel {
	m = m.normalizeSelection(m.control.Cursor(), all)
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			return m.normalizeSelection(m.control.Cursor(), all)
		case "enter":
			m.filtering = false
			return m
		case "backspace":
			m.filter = trimLastRune(m.filter)
			return m.normalizeSelection(m.control.Cursor(), all)
		}
		if text := printableText(message.Text); text != "" {
			m.filter += text
			return m.normalizeSelection(m.control.Cursor(), all)
		}
		return m
	}

	switch key {
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m = m.normalizeSelection(m.control.Cursor(), all)
	case "up", "k":
		m.control.MoveUp(1)
		m = m.rememberSelection(all)
	case "down", "j":
		m.control.MoveDown(1)
		m = m.rememberSelection(all)
	case "g":
		m.control.GotoTop()
		m = m.rememberSelection(all)
	case "G":
		m.control.GotoBottom()
		m = m.rememberSelection(all)
	}
	return m
}

func (m problemsModel) normalizeSelection(closest int, all []domain.Diagnosis) problemsModel {
	diagnoses := m.visibleDiagnoses(all)
	m.control.SetCount(len(diagnoses))
	if len(diagnoses) == 0 {
		m.control.SetCursor(0)
		m.selectedKey = ""
		return m
	}
	for index, diagnosis := range diagnoses {
		if diagnosisKey(diagnosis) == m.selectedKey {
			m.control.SetCursor(index)
			return m
		}
	}
	m.control.SetCursor(min(max(closest, 0), len(diagnoses)-1))
	m.selectedKey = diagnosisKey(diagnoses[m.control.Cursor()])
	return m
}

func diagnosisKey(diagnosis domain.Diagnosis) string {
	return diagnosis.ResourceKind + "\x00" + diagnosis.ResourceID + "\x00" + diagnosis.RuleID
}

func (m problemsModel) rememberSelection(all []domain.Diagnosis) problemsModel {
	diagnoses := m.visibleDiagnoses(all)
	if len(diagnoses) > 0 && m.control.Cursor() >= 0 && m.control.Cursor() < len(diagnoses) {
		m.selectedKey = diagnosisKey(diagnoses[m.control.Cursor()])
	}
	return m
}

func (m problemsModel) selected(all []domain.Diagnosis) (domain.Diagnosis, bool) {
	m = m.normalizeSelection(m.control.Cursor(), all)
	diagnoses := m.visibleDiagnoses(all)
	cursor := m.control.Cursor()
	if len(diagnoses) == 0 || cursor < 0 || cursor >= len(diagnoses) {
		return domain.Diagnosis{}, false
	}
	return diagnoses[cursor], true
}

func (m problemsModel) selectedValue(all []domain.Diagnosis) domain.Diagnosis {
	diagnosis, _ := m.selected(all)
	return diagnosis
}

func (m problemsModel) visibleDiagnoses(all []domain.Diagnosis) []domain.Diagnosis {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return all
	}
	filtered := make([]domain.Diagnosis, 0, len(all))
	for _, diagnosis := range all {
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

func (m problemsModel) view(width int, all []domain.Diagnosis) string {
	m = m.normalizeSelection(m.control.Cursor(), all)
	contents := renderProblemTable(width, m.visibleDiagnoses(all), m.control.Cursor(), m.changes)
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m problemsModel) viewSized(size contentSize, all []domain.Diagnosis) string {
	m = m.normalizeSelection(m.control.Cursor(), all)
	diagnoses := m.visibleDiagnoses(all)
	if len(diagnoses) == 0 {
		return "No problems"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(diagnoses), m.control.Cursor(), rowCapacity)
	return renderProblemTable(size.Width, diagnoses[start:end], m.control.Cursor()-start, m.changes)
}

const defaultProblemsWidth = 120

var problemColumns = []tableColumn[domain.Diagnosis]{
	{header: "SEVERITY", minWidth: 8, value: func(d domain.Diagnosis) string { return d.Severity.String() }},
	{header: "KIND", minWidth: 12, value: func(d domain.Diagnosis) string { return fallback(d.ResourceKind) }},
	{header: "RESOURCE", minWidth: 16, value: func(d domain.Diagnosis) string { return fallback(d.ResourceName) }},
	{header: "SUMMARY", minWidth: 20, grow: true, value: func(d domain.Diagnosis) string { return fallback(d.Summary) }},
}

func renderProblemTable(width int, diagnoses []domain.Diagnosis, selectedIndex int, changes ...tableChanges) string {
	widths := problemColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString(" ")
	writeTableCells(&header, problemHeaders(), widths)
	output.WriteString(renderTableHeader(header.String(), width, defaultK9sSkin()))

	for index, diagnosis := range diagnoses {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString(" ")
		writeTableCells(&row, problemRowValues(diagnosis), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, changedRowSkin(diagnosisKey(diagnosis), changes)))
	}

	return output.String()
}

func problemColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultProblemsWidth
	}
	return calculateColumnWidths(width, problemColumns)
}

func problemHeaders() []string {
	return tableHeaders(problemColumns)
}

func problemRowValues(diagnosis domain.Diagnosis) []string {
	return tableRowValues(diagnosis, problemColumns)
}
