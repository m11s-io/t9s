package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type eventsModel struct {
	filter      string
	filtering   bool
	selectedKey string
	control     listControl
}

func newEventsModel() eventsModel { return eventsModel{} }

func (m eventsModel) normalizeSelection(all []domain.EventSnapshot, closest int) eventsModel {
	visible := m.visibleEvents(all)
	m.control.SetCount(len(visible))
	if len(visible) == 0 {
		m.control.SetCursor(0)
		if !eventKeyExists(all, m.selectedKey) {
			m.selectedKey = ""
		}
		return m
	}
	for index, event := range visible {
		if eventKey(event) == m.selectedKey {
			m.control.SetCursor(index)
			return m
		}
	}
	m.control.SetCursor(min(max(closest, 0), len(visible)-1))
	if !eventKeyExists(all, m.selectedKey) {
		m.selectedKey = eventKey(visible[m.control.Cursor()])
	}
	return m
}

func (m eventsModel) startFilter(value string, all []domain.EventSnapshot) eventsModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(all, m.control.Cursor())
}

func (m eventsModel) update(message tea.KeyPressMsg, all []domain.EventSnapshot) eventsModel {
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			return m.normalizeSelection(all, m.control.Cursor())
		case "enter":
			m.filtering = false
			return m
		case "backspace":
			m.filter = trimLastRune(m.filter)
			return m.normalizeSelection(all, m.control.Cursor())
		}
		if text := printableText(message.Text); text != "" {
			m.filter += text
			return m.normalizeSelection(all, m.control.Cursor())
		}
		return m
	}

	switch key {
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m = m.normalizeSelection(all, m.control.Cursor())
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

func (m eventsModel) rememberSelection(all []domain.EventSnapshot) eventsModel {
	visible := m.visibleEvents(all)
	if len(visible) > 0 {
		m.selectedKey = eventKey(visible[m.control.Cursor()])
	}
	return m
}

func eventKey(event domain.EventSnapshot) string {
	return event.Node + "\x00" + event.Kind + "\x00" + event.Message + "\x00" + event.ObservedAt.UTC().Format(time.RFC3339Nano)
}

func eventKeyExists(events []domain.EventSnapshot, key string) bool {
	if key == "" {
		return false
	}
	for _, event := range events {
		if eventKey(event) == key {
			return true
		}
	}
	return false
}

func (m eventsModel) selected(all []domain.EventSnapshot) (domain.EventSnapshot, bool) {
	events := m.visibleEvents(all)
	cursor := m.control.Cursor()
	if len(events) == 0 || cursor < 0 || cursor >= len(events) {
		return domain.EventSnapshot{}, false
	}
	return events[cursor], true
}

func (m eventsModel) visibleEvents(events []domain.EventSnapshot) []domain.EventSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return events
	}
	filtered := make([]domain.EventSnapshot, 0, len(events))
	for _, event := range events {
		for _, value := range []string{event.Node, event.Kind, event.Message} {
			if strings.Contains(strings.ToLower(value), query) {
				filtered = append(filtered, event)
				break
			}
		}
	}
	return filtered
}

func (m eventsModel) view(width int, all []domain.EventSnapshot) string {
	contents := renderEventTable(width, m.visibleEvents(all), m.control.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func eventRows(events domain.EventSet) []domain.EventSnapshot {
	if len(events.Problems) == 0 {
		return events.Events
	}
	rows := make([]domain.EventSnapshot, 0, len(events.Problems)+len(events.Events))
	for _, problem := range events.Problems {
		rows = append(rows, domain.EventSnapshot{Node: problem.Node, Kind: "StreamError", Message: problem.Message})
	}
	return append(rows, events.Events...)
}

// syncLegacySelection adapts the legacy reducer's event state into the same
// presentation-only selection model while that root is still supported.
func (m eventsModel) syncLegacySelection(state application.EventState) eventsModel {
	rows := eventRows(state.Value)
	closest := m.control.Cursor()
	if state.Following {
		m.selectedKey = ""
		closest = len(rows) - 1
	}
	return m.normalizeSelection(rows, closest)
}

func (m eventsModel) viewSized(size contentSize, state directEventState) string {
	return m.tableViewSized(size, state.Value, state.Status == directLoading || state.Status == "", state.Status == directFailed, state.Err)
}

func (m eventsModel) tableViewSized(size contentSize, eventSet domain.EventSet, loading, failed bool, err string) string {
	if failed {
		return fallback(err)
	}
	events := m.visibleEvents(eventRows(eventSet))
	if len(events) == 0 {
		if loading {
			return "Loading events…"
		}
		return "No events"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(events), m.control.Cursor(), rowCapacity)
	return renderEventTable(size.Width, events[start:end], m.control.Cursor()-start)
}

func (m eventsModel) legacyViewSized(size contentSize, state application.EventState) string {
	status := ""
	if state.FollowAttempted {
		status = "EVENTS PAUSED"
		if state.Following {
			status = "EVENTS FOLLOW"
		}
		if state.Err != "" {
			status += " — " + state.Err
		}
		if len(state.Value.Problems) > 0 {
			status += " — failed nodes: " + fmt.Sprint(len(state.Value.Problems))
		}
	}
	if status != "" {
		if size.Height <= 1 {
			return status
		}
		size.Height--
		return status + "\n" + m.tableViewSized(size, state.Value, state.Status == application.Loading || state.Status == application.Idle, state.Status == application.Failed, state.Err)
	}
	return m.tableViewSized(size, state.Value, state.Status == application.Loading || state.Status == application.Idle, state.Status == application.Failed, state.Err)
}

func renderEvents(width int, state application.EventState) string {
	return newEventsModel().view(width, eventRows(state.Value))
}

const defaultEventsWidth = 120

var eventColumns = []tableColumn[domain.EventSnapshot]{
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
	writeTableCells(&header, eventHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, event := range events {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeTableCells(&row, eventRowValues(event), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func eventColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultEventsWidth
	}
	return calculateColumnWidths(width, eventColumns)
}

func eventHeaders() []string { return tableHeaders(eventColumns) }

func eventRowValues(event domain.EventSnapshot) []string { return tableRowValues(event, eventColumns) }
