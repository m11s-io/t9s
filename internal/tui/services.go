package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/table"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

const (
	defaultServicesWidth    = 120
	maxServiceRows          = 100
	serviceDetailEventWidth = 80
)

type servicesModel struct {
	state       application.ServiceState
	filter      string
	filtering   bool
	selectedKey string
	table       table.Model
}

func newServicesModel(state application.ServiceState) servicesModel {
	return (servicesModel{state: state, table: table.New()}).normalizeSelection(0)
}

func (m servicesModel) startFilter(value string) servicesModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(m.table.Cursor())
}

func (m servicesModel) setState(state application.ServiceState) servicesModel {
	previousIndex := m.table.Cursor()
	m.state = state
	return m.normalizeSelection(previousIndex)
}

func (m servicesModel) update(message tea.KeyPressMsg) servicesModel {
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
		m = m.moveSelection(-1)
	case "down", "j":
		m = m.moveSelection(1)
	case "g":
		m.selectedKey = ""
		m = m.normalizeSelection(0)
	case "G":
		m.selectedKey = ""
		m = m.normalizeSelection(len(m.visibleServices()) - 1)
	}
	return m
}

func (m servicesModel) moveSelection(delta int) servicesModel {
	services := m.visibleServices()
	if len(services) == 0 {
		return m.normalizeSelection(0)
	}
	if delta < 0 {
		m.table.MoveUp(-delta)
	} else {
		m.table.MoveDown(delta)
	}
	m.selectedKey = serviceKey(services[m.table.Cursor()])
	return m
}

func (m servicesModel) normalizeSelection(closest int) servicesModel {
	services := m.visibleServices()
	m.table.SetRows(make([]table.Row, len(services)))
	if len(services) == 0 {
		m.table.SetCursor(0)
		m.selectedKey = ""
		return m
	}
	for index, service := range services {
		if serviceKey(service) == m.selectedKey {
			m.table.SetCursor(index)
			return m
		}
	}
	m.table.SetCursor(min(max(closest, 0), len(services)-1))
	m.selectedKey = serviceKey(services[m.table.Cursor()])
	return m
}

func (m servicesModel) selected() (domain.ServiceSnapshot, bool) {
	services := m.visibleServices()
	cursor := m.table.Cursor()
	if len(services) == 0 || cursor < 0 || cursor >= len(services) {
		return domain.ServiceSnapshot{}, false
	}
	return services[cursor], true
}

func (m servicesModel) selectedValue() domain.ServiceSnapshot {
	service, _ := m.selected()
	return service
}

func (m servicesModel) visibleServices() []domain.ServiceSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return capServices(m.state.Value.Services)
	}

	filtered := make([]domain.ServiceSnapshot, 0, min(len(m.state.Value.Services), maxServiceRows))
	for _, service := range m.state.Value.Services {
		if strings.Contains(strings.ToLower(service.Node), query) || strings.Contains(strings.ToLower(service.Name), query) {
			filtered = append(filtered, service)
			if len(filtered) == maxServiceRows {
				break
			}
		}
	}
	return filtered
}

func capServices(services []domain.ServiceSnapshot) []domain.ServiceSnapshot {
	if len(services) <= maxServiceRows {
		return services
	}
	return services[:maxServiceRows]
}

func (m servicesModel) view(width int) string {
	contents := renderServiceTable(width, m.visibleServices(), m.table.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m servicesModel) viewSized(size contentSize) string {
	if m.state.Status == application.Failed {
		return fallback(m.state.Err)
	}
	services := m.visibleServices()
	problems := m.visibleProblems()
	if len(services) == 0 && len(problems) == 0 {
		if m.state.Status == application.Loading || m.state.Status == application.Idle {
			return "Loading services…"
		}
		return "No services"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(services), m.table.Cursor(), rowCapacity)
	services = services[start:end]
	remaining := max(0, rowCapacity-len(services))
	if len(problems) > remaining {
		problems = problems[:remaining]
	}
	return renderServiceTableWithProblems(size.Width, services, problems, m.table.Cursor()-start)
}

func (m servicesModel) visibleProblems() []domain.ServiceProblem {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.state.Value.Problems
	}
	problems := make([]domain.ServiceProblem, 0, len(m.state.Value.Problems))
	for _, problem := range m.state.Value.Problems {
		if strings.Contains(strings.ToLower(problem.Node), query) || strings.Contains(strings.ToLower(problem.Message), query) {
			problems = append(problems, problem)
		}
	}
	return problems
}

func renderServices(width int, state application.ServiceState) string {
	return newServicesModel(state).view(width)
}

type serviceColumn struct {
	header   string
	minWidth int
	value    func(domain.ServiceSnapshot) string
	grow     bool
}

var serviceColumns = []serviceColumn{
	{header: "NODE", minWidth: 16, value: func(service domain.ServiceSnapshot) string { return fallback(service.Node) }},
	{header: "SERVICE", minWidth: 16, value: func(service domain.ServiceSnapshot) string { return fallback(service.Name) }},
	{header: "STATE", minWidth: 10, value: func(service domain.ServiceSnapshot) string { return fallback(service.State) }},
	{header: "HEALTH", minWidth: 6, value: func(service domain.ServiceSnapshot) string { return serviceHealthSymbol(service.Healthy) }},
	{header: "LAST EVENT", minWidth: 12, grow: true, value: func(service domain.ServiceSnapshot) string { return serviceEventText(service.LastMessage) }},
}

func renderServiceTable(width int, services []domain.ServiceSnapshot, selectedIndex int) string {
	services = capServices(services)
	widths := serviceColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeServiceCells(&header, serviceHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))
	for index, service := range services {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeServiceCells(&row, serviceRowValues(service), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}
	if len(services) == 0 {
		output.WriteString("\nNo services")
	}
	return output.String()
}

func renderServiceTableWithProblems(width int, services []domain.ServiceSnapshot, problems []domain.ServiceProblem, selectedIndex int) string {
	widths := serviceColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeServiceCells(&header, serviceHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))
	for index, service := range services {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeServiceCells(&row, serviceRowValues(service), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}
	for _, problem := range problems {
		output.WriteString("\n! ")
		writeServiceCells(&output, []string{fallback(problem.Node), "<error>", "Error", "!", fallback(problem.Message)}, widths)
	}
	return output.String()
}

func serviceColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultServicesWidth
	}
	widths := make([]int, len(serviceColumns))
	used := selectionWidth + columnSpacing*(len(serviceColumns)-1)
	growIndex := 0
	for index, column := range serviceColumns {
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

func serviceHeaders() []string {
	headers := make([]string, len(serviceColumns))
	for index, column := range serviceColumns {
		headers[index] = column.header
	}
	return headers
}

func serviceRowValues(service domain.ServiceSnapshot) []string {
	values := make([]string, len(serviceColumns))
	for index, column := range serviceColumns {
		values[index] = column.value(service)
	}
	return values
}

func writeServiceCells(output *strings.Builder, values []string, widths []int) {
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

func serviceHealthSymbol(healthy *bool) string {
	if healthy == nil {
		return "?"
	}
	if *healthy {
		return "✓"
	}
	return "!"
}

func renderServiceDetail(service domain.ServiceSnapshot) string {
	var view strings.Builder
	view.WriteString("SERVICE DETAIL\n")
	view.WriteString(fmt.Sprintf("NODE       %s\n", fallback(service.Node)))
	view.WriteString(fmt.Sprintf("SERVICE    %s\n", fallback(service.Name)))
	view.WriteString(fmt.Sprintf("STATE      %s\n", fallback(service.State)))
	view.WriteString(fmt.Sprintf("HEALTH     %s\n", serviceHealthSymbol(service.Healthy)))
	view.WriteString(fmt.Sprintf("LAST EVENT %s\n", boundedServiceEvent(service.LastMessage)))
	view.WriteString(fmt.Sprintf("LAST CHANGE %s\n", formatServiceChange(service.LastChange)))
	return strings.TrimSuffix(view.String(), "\n")
}

func formatServiceChange(change time.Time) string {
	if change.IsZero() {
		return "-"
	}
	return change.UTC().Format(time.RFC3339)
}

func serviceEventText(event string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(fallback(event))
}

func boundedServiceEvent(event string) string {
	return ansi.Truncate(serviceEventText(event), serviceDetailEventWidth, "…")
}

func serviceKey(service domain.ServiceSnapshot) string {
	return service.Node + "\x00" + service.Name
}
