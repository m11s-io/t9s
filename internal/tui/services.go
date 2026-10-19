package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/m11s-io/t9s/internal/domain"
)

const (
	defaultServicesWidth    = 120
	maxServiceRows          = 100
	serviceDetailEventWidth = 80
)

type servicesModel struct {
	filter      string
	filtering   bool
	selectedKey string
	control     listControl
}

func newServicesModel() servicesModel {
	return servicesModel{}
}

func (m servicesModel) startFilter(value string, services []domain.ServiceSnapshot) servicesModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(services, m.control.Cursor())
}

func (m servicesModel) update(message tea.KeyPressMsg, services []domain.ServiceSnapshot) servicesModel {
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			return m.normalizeSelection(services, m.control.Cursor())
		case "enter":
			m.filtering = false
			return m
		case "backspace":
			m.filter = trimLastRune(m.filter)
			return m.normalizeSelection(services, m.control.Cursor())
		}
		if text := printableText(message.Text); text != "" {
			m.filter += text
			return m.normalizeSelection(services, m.control.Cursor())
		}
		return m
	}

	switch key {
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m = m.normalizeSelection(services, m.control.Cursor())
	case "up", "k":
		m = m.moveSelection(-1, services)
	case "down", "j":
		m = m.moveSelection(1, services)
	case "g":
		m.selectedKey = ""
		m = m.normalizeSelection(services, 0)
	case "G":
		m.selectedKey = ""
		m = m.normalizeSelection(services, len(m.visibleServices(services))-1)
	}
	return m
}

func (m servicesModel) moveSelection(delta int, all []domain.ServiceSnapshot) servicesModel {
	services := m.visibleServices(all)
	if len(services) == 0 {
		return m.normalizeSelection(all, 0)
	}
	if delta < 0 {
		m.control.MoveUp(-delta)
	} else {
		m.control.MoveDown(delta)
	}
	m.selectedKey = serviceKey(services[m.control.Cursor()])
	return m
}

func (m servicesModel) normalizeSelection(all []domain.ServiceSnapshot, closest int) servicesModel {
	services := m.visibleServices(all)
	m.control.SetCount(len(services))
	selectedPresent := false
	if m.selectedKey != "" {
		for _, service := range all {
			if serviceKey(service) == m.selectedKey {
				selectedPresent = true
				break
			}
		}
	}
	if len(services) == 0 {
		m.control.SetCursor(0)
		if !selectedPresent {
			m.selectedKey = ""
		}
		return m
	}
	for index, service := range services {
		if serviceKey(service) == m.selectedKey {
			m.control.SetCursor(index)
			return m
		}
	}
	m.control.SetCursor(min(max(closest, 0), len(services)-1))
	if !selectedPresent {
		m.selectedKey = serviceKey(services[m.control.Cursor()])
	}
	return m
}

func (m servicesModel) selected(all []domain.ServiceSnapshot) (domain.ServiceSnapshot, bool) {
	services := m.visibleServices(all)
	cursor := m.control.Cursor()
	if len(services) == 0 || cursor < 0 || cursor >= len(services) {
		return domain.ServiceSnapshot{}, false
	}
	return services[cursor], true
}

func (m servicesModel) selectedValue(all []domain.ServiceSnapshot) domain.ServiceSnapshot {
	service, _ := m.selected(all)
	return service
}

func (m servicesModel) visibleServices(all []domain.ServiceSnapshot) []domain.ServiceSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return capServices(all)
	}

	filtered := make([]domain.ServiceSnapshot, 0, min(len(all), maxServiceRows))
	for _, service := range all {
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

func (m servicesModel) view(width int, all []domain.ServiceSnapshot) string {
	contents := renderServiceTable(width, m.visibleServices(all), m.control.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m servicesModel) viewSized(size contentSize, services domain.ServiceSet, loading, failed bool, err string) string {
	if failed {
		return fallback(err)
	}
	visible := m.visibleServices(services.Services)
	problems := m.visibleProblems(services)
	if len(visible) == 0 && len(problems) == 0 {
		if loading {
			return "Loading services…"
		}
		return "No services"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(visible), m.control.Cursor(), rowCapacity)
	visible = visible[start:end]
	remaining := max(0, rowCapacity-len(visible))
	if len(problems) > remaining {
		problems = problems[:remaining]
	}
	return renderServiceTableWithProblems(size.Width, visible, problems, m.control.Cursor()-start)
}

func (m servicesModel) visibleProblems(services domain.ServiceSet) []domain.ServiceProblem {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return services.Problems
	}
	problems := make([]domain.ServiceProblem, 0, len(services.Problems))
	for _, problem := range services.Problems {
		if strings.Contains(strings.ToLower(problem.Node), query) || strings.Contains(strings.ToLower(problem.Message), query) {
			problems = append(problems, problem)
		}
	}
	return problems
}

var serviceColumns = []tableColumn[domain.ServiceSnapshot]{
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
	writeTableCells(&header, serviceHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))
	for index, service := range services {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeTableCells(&row, serviceRowValues(service), widths)
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
	writeTableCells(&header, serviceHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))
	for index, service := range services {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeTableCells(&row, serviceRowValues(service), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}
	for _, problem := range problems {
		output.WriteString("\n! ")
		writeTableCells(&output, []string{fallback(problem.Node), "<error>", "Error", "!", fallback(problem.Message)}, widths)
	}
	return output.String()
}

func serviceColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultServicesWidth
	}
	return calculateColumnWidths(width, serviceColumns)
}

func serviceHeaders() []string {
	return tableHeaders(serviceColumns)
}

func serviceRowValues(service domain.ServiceSnapshot) []string {
	return tableRowValues(service, serviceColumns)
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

func findServiceByKey(services []domain.ServiceSnapshot, key string) (domain.ServiceSnapshot, bool) {
	for _, service := range services {
		if serviceKey(service) == key {
			return service, true
		}
	}
	return domain.ServiceSnapshot{}, false
}
