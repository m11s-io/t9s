package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/table"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type eventsModel struct {
	state     application.EventState
	filter    string
	filtering bool
	table     table.Model
}

func newEventsModel(state application.EventState) eventsModel {
	return (eventsModel{state: state, table: table.New()}).normalizeSelection(0)
}

func (m eventsModel) setState(state application.EventState) eventsModel {
	previousIndex := m.table.Cursor()
	m.state = state
	return m.normalizeSelection(previousIndex)
}

func (m eventsModel) startFilter(value string) eventsModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(m.table.Cursor())
}

func (m eventsModel) update(message tea.KeyPressMsg) eventsModel {
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

func (m eventsModel) normalizeSelection(closest int) eventsModel {
	events := m.visibleEvents()
	m.table.SetRows(make([]table.Row, len(events)))
	m.table.SetCursor(min(max(closest, 0), max(0, len(events)-1)))
	return m
}

func (m eventsModel) selected() (domain.EventSnapshot, bool) {
	events := m.visibleEvents()
	cursor := m.table.Cursor()
	if len(events) == 0 || cursor < 0 || cursor >= len(events) {
		return domain.EventSnapshot{}, false
	}
	return events[cursor], true
}

func (m eventsModel) visibleEvents() []domain.EventSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.state.Value.Events
	}
	filtered := make([]domain.EventSnapshot, 0, len(m.state.Value.Events))
	for _, event := range m.state.Value.Events {
		values := []string{event.Node, event.Kind, event.Message}
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), query) {
				filtered = append(filtered, event)
				break
			}
		}
	}
	return filtered
}

func (m eventsModel) view(width int) string {
	contents := renderEventTable(width, m.visibleEvents(), m.table.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m eventsModel) viewSized(size contentSize) string {
	if m.state.Status == application.Failed {
		return fallback(m.state.Err)
	}
	events := m.visibleEvents()
	if len(events) == 0 {
		if m.state.Status == application.Loading || m.state.Status == application.Idle {
			return "Loading events…"
		}
		return "No events"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(events), m.table.Cursor(), rowCapacity)
	return renderEventTable(size.Width, events[start:end], m.table.Cursor()-start)
}

func renderEvents(width int, state application.EventState) string {
	return newEventsModel(state).view(width)
}

const defaultEventsWidth = 120

type eventColumn struct {
	header   string
	minWidth int
	value    func(domain.EventSnapshot) string
	grow     bool
}

var eventColumns = []eventColumn{
	{header: "NODE", minWidth: 14, value: func(event domain.EventSnapshot) string { return fallback(event.Node) }},
	{header: "KIND", minWidth: 12, value: func(event domain.EventSnapshot) string { return fallback(event.Kind) }},
	{header: "MESSAGE", minWidth: 20, grow: true, value: func(event domain.EventSnapshot) string { return fallback(event.Message) }},
	{header: "OBSERVED", minWidth: 20, value: func(event domain.EventSnapshot) string { return formatServiceChange(event.ObservedAt) }},
}

func renderEventTable(width int, events []domain.EventSnapshot, selectedIndex int) string {
	widths := eventColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeEventCells(&header, eventHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, event := range events {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeEventCells(&row, eventRowValues(event), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func eventColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultEventsWidth
	}
	widths := make([]int, len(eventColumns))
	used := selectionWidth + columnSpacing*(len(eventColumns)-1)
	growIndex := 0
	for index, column := range eventColumns {
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

func eventHeaders() []string {
	headers := make([]string, len(eventColumns))
	for index, column := range eventColumns {
		headers[index] = column.header
	}
	return headers
}

func eventRowValues(event domain.EventSnapshot) []string {
	values := make([]string, len(eventColumns))
	for index, column := range eventColumns {
		values[index] = column.value(event)
	}
	return values
}

func writeEventCells(output *strings.Builder, values []string, widths []int) {
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
