package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

func sensitivityMarker(sensitive bool) string {
	if sensitive {
		return "⚠"
	}
	return ""
}

type resourceKindsModel struct {
	state     application.ResourceBrowserState
	filter    string
	filtering bool
	table     table.Model
}

func newResourceKindsModel(state application.ResourceBrowserState) resourceKindsModel {
	return (resourceKindsModel{state: state, table: table.New()}).normalizeSelection(0)
}

func (m resourceKindsModel) setState(state application.ResourceBrowserState) resourceKindsModel {
	previousIndex := m.table.Cursor()
	m.state = state
	return m.normalizeSelection(previousIndex)
}

func (m resourceKindsModel) update(message tea.KeyPressMsg) resourceKindsModel {
	key := message.Keystroke()
	if m.filtering {
		switch key {
		case "esc":
			m.filter, m.filtering = "", false
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

func (m resourceKindsModel) normalizeSelection(closest int) resourceKindsModel {
	kinds := m.visibleKinds()
	m.table.SetRows(make([]table.Row, len(kinds)))
	m.table.SetCursor(min(max(closest, 0), max(0, len(kinds)-1)))
	return m
}

func (m resourceKindsModel) selected() (domain.ResourceKindSnapshot, bool) {
	kinds := m.visibleKinds()
	cursor := m.table.Cursor()
	if len(kinds) == 0 || cursor < 0 || cursor >= len(kinds) {
		return domain.ResourceKindSnapshot{}, false
	}
	return kinds[cursor], true
}

func (m resourceKindsModel) selectedValue() domain.ResourceKindSnapshot {
	kind, _ := m.selected()
	return kind
}

func (m resourceKindsModel) visibleKinds() []domain.ResourceKindSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.state.Kinds.Kinds
	}
	filtered := make([]domain.ResourceKindSnapshot, 0, len(m.state.Kinds.Kinds))
	for _, kind := range m.state.Kinds.Kinds {
		if strings.Contains(strings.ToLower(kind.DisplayType), query) || strings.Contains(strings.ToLower(kind.Type), query) {
			filtered = append(filtered, kind)
		}
	}
	return filtered
}

func (m resourceKindsModel) view(width int) string {
	contents := renderResourceKindTable(width, m.visibleKinds(), m.table.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m resourceKindsModel) viewSized(size contentSize) string {
	if m.state.KindsStatus == application.Failed {
		return fallback(m.state.KindsErr)
	}
	kinds := m.visibleKinds()
	if len(kinds) == 0 {
		if m.state.KindsStatus == application.Loading || m.state.KindsStatus == application.Idle {
			return "Loading resource kinds…"
		}
		return "No resource kinds"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(kinds), m.table.Cursor(), rowCapacity)
	return renderResourceKindTable(size.Width, kinds[start:end], m.table.Cursor()-start)
}

const defaultResourceKindsWidth = 120

type resourceKindColumn struct {
	header   string
	minWidth int
	value    func(domain.ResourceKindSnapshot) string
	grow     bool
}

var resourceKindColumns = []resourceKindColumn{
	{header: "TYPE", minWidth: 20, value: func(k domain.ResourceKindSnapshot) string { return fallback(k.DisplayType) }},
	{header: "NAMESPACE", minWidth: 14, value: func(k domain.ResourceKindSnapshot) string { return fallback(k.DefaultNamespace) }},
	{header: "ALIASES", minWidth: 20, grow: true, value: func(k domain.ResourceKindSnapshot) string {
		return sensitivityMarker(k.Sensitive) + strings.Join(k.Aliases, ", ")
	}},
}

func renderResourceKindTable(width int, kinds []domain.ResourceKindSnapshot, selectedIndex int) string {
	widths := resourceKindColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	headers := make([]string, len(resourceKindColumns))
	for index, column := range resourceKindColumns {
		headers[index] = column.header
	}
	writeResourceCells(&header, headers, widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, kind := range kinds {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		values := make([]string, len(resourceKindColumns))
		for columnIndex, column := range resourceKindColumns {
			values[columnIndex] = column.value(kind)
		}
		writeResourceCells(&row, values, widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func resourceKindColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultResourceKindsWidth
	}
	widths := make([]int, len(resourceKindColumns))
	used := selectionWidth + columnSpacing*(len(resourceKindColumns)-1)
	growIndex := 0
	for index, column := range resourceKindColumns {
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

func writeResourceCells(output *strings.Builder, values []string, widths []int) {
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

type resourceInstancesModel struct {
	state     application.ResourceBrowserState
	filter    string
	filtering bool
	table     table.Model
}

func newResourceInstancesModel(state application.ResourceBrowserState) resourceInstancesModel {
	return (resourceInstancesModel{state: state, table: table.New()}).normalizeSelection(0)
}

func (m resourceInstancesModel) setState(state application.ResourceBrowserState) resourceInstancesModel {
	previousIndex := m.table.Cursor()
	m.state = state
	return m.normalizeSelection(previousIndex)
}

func (m resourceInstancesModel) update(message tea.KeyPressMsg) resourceInstancesModel {
	key := message.Keystroke()
	if m.filtering {
		switch key {
		case "esc":
			m.filter, m.filtering = "", false
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

func (m resourceInstancesModel) normalizeSelection(closest int) resourceInstancesModel {
	instances := m.visibleInstances()
	m.table.SetRows(make([]table.Row, len(instances)))
	m.table.SetCursor(min(max(closest, 0), max(0, len(instances)-1)))
	return m
}

func (m resourceInstancesModel) selected() (domain.ResourceInstanceSnapshot, bool) {
	instances := m.visibleInstances()
	cursor := m.table.Cursor()
	if len(instances) == 0 || cursor < 0 || cursor >= len(instances) {
		return domain.ResourceInstanceSnapshot{}, false
	}
	return instances[cursor], true
}

func (m resourceInstancesModel) selectedValue() domain.ResourceInstanceSnapshot {
	instance, _ := m.selected()
	return instance
}

func (m resourceInstancesModel) visibleInstances() []domain.ResourceInstanceSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.state.Instances.Instances
	}
	filtered := make([]domain.ResourceInstanceSnapshot, 0, len(m.state.Instances.Instances))
	for _, instance := range m.state.Instances.Instances {
		if strings.Contains(strings.ToLower(instance.ID), query) {
			filtered = append(filtered, instance)
		}
	}
	return filtered
}

type resourceInstanceColumn struct {
	header   string
	minWidth int
	value    func(domain.ResourceInstanceSnapshot) string
	grow     bool
}

var resourceInstanceColumns = []resourceInstanceColumn{
	{header: "NAMESPACE", minWidth: 14, value: func(r domain.ResourceInstanceSnapshot) string { return fallback(r.Namespace) }},
	{header: "ID", minWidth: 20, grow: true, value: func(r domain.ResourceInstanceSnapshot) string { return fallback(r.ID) }},
	{header: "PHASE", minWidth: 10, value: func(r domain.ResourceInstanceSnapshot) string { return fallback(r.Phase) }},
}

func resourceInstanceColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultResourceKindsWidth
	}
	widths := make([]int, len(resourceInstanceColumns))
	used := selectionWidth + columnSpacing*(len(resourceInstanceColumns)-1)
	growIndex := 0
	for index, column := range resourceInstanceColumns {
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

func (m resourceInstancesModel) view(width int) string {
	contents := renderResourceInstanceTable(width, m.visibleInstances(), m.table.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m resourceInstancesModel) viewSized(size contentSize) string {
	if m.state.InstancesStatus == application.Failed {
		return fallback(m.state.InstancesErr)
	}
	instances := m.visibleInstances()
	if len(instances) == 0 {
		if m.state.InstancesStatus == application.Loading || m.state.InstancesStatus == application.Idle {
			return "Loading instances…"
		}
		return "No instances"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(instances), m.table.Cursor(), rowCapacity)
	return renderResourceInstanceTable(size.Width, instances[start:end], m.table.Cursor()-start)
}

func renderResourceInstanceTable(width int, instances []domain.ResourceInstanceSnapshot, selectedIndex int) string {
	widths := resourceInstanceColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	headers := make([]string, len(resourceInstanceColumns))
	for index, column := range resourceInstanceColumns {
		headers[index] = column.header
	}
	writeResourceCells(&header, headers, widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, instance := range instances {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		values := make([]string, len(resourceInstanceColumns))
		for columnIndex, column := range resourceInstanceColumns {
			values[columnIndex] = column.value(instance)
		}
		writeResourceCells(&row, values, widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

type resourceDetailModel struct {
	viewport viewport.Model
}

func newResourceDetailModel() resourceDetailModel {
	return resourceDetailModel{viewport: viewport.New()}
}

func (m resourceDetailModel) update(message tea.KeyPressMsg) resourceDetailModel {
	switch message.Keystroke() {
	case "up", "k":
		m.viewport.ScrollUp(1)
	case "down", "j":
		m.viewport.ScrollDown(1)
	case "g":
		m.viewport.GotoTop()
	case "G":
		m.viewport.GotoBottom()
	}
	return m
}

func renderResourceDetailHeader(instance domain.ResourceInstanceSnapshot, sensitive bool) string {
	var view strings.Builder
	if sensitive {
		view.WriteString("⚠ SENSITIVE\n")
	}
	view.WriteString(fmt.Sprintf("NAMESPACE  %s\n", fallback(instance.Namespace)))
	view.WriteString(fmt.Sprintf("TYPE       %s\n", fallback(instance.Type)))
	view.WriteString(fmt.Sprintf("ID         %s\n", fallback(instance.ID)))
	view.WriteString(fmt.Sprintf("VERSION    %s\n", fallback(instance.Version)))
	view.WriteString(fmt.Sprintf("PHASE      %s\n", fallback(instance.Phase)))
	return strings.TrimSuffix(view.String(), "\n")
}

func (m resourceDetailModel) viewSized(size contentSize, instance domain.ResourceInstanceSnapshot, sensitive bool) string {
	header := renderResourceDetailHeader(instance, sensitive)
	headerLines := strings.Split(header, "\n")
	bodyHeight := max(0, size.Height-len(headerLines)-1)
	body := strings.Split(instance.YAML, "\n")
	m.viewport.SetHeight(bodyHeight)
	m.viewport.SetContentLines(body)
	start := min(m.viewport.YOffset(), len(body))
	end := min(start+bodyHeight, len(body))
	lines := append([]string(nil), headerLines...)
	lines = append(lines, "")
	lines = append(lines, body[start:end]...)
	return strings.Join(lines, "\n")
}
