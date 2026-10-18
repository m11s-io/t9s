package tui

import (
	"fmt"
	"strconv"
	"strings"

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
	keys := eventKeys(all)
	visibleIndexes := m.visibleEventIndexes(all)
	m.control.SetCount(len(visibleIndexes))
	if len(visibleIndexes) == 0 {
		m.control.SetCursor(0)
		if !eventKeyExists(keys, m.selectedKey) {
			m.selectedKey = ""
		}
		return m
	}
	for index, sourceIndex := range visibleIndexes {
		if keys[sourceIndex] == m.selectedKey {
			m.control.SetCursor(index)
			return m
		}
	}
	m.control.SetCursor(min(max(closest, 0), len(visibleIndexes)-1))
	if !eventKeyExists(keys, m.selectedKey) {
		m.selectedKey = keys[visibleIndexes[m.control.Cursor()]]
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
	visibleIndexes := m.visibleEventIndexes(all)
	keys := eventKeys(all)
	if len(visibleIndexes) > 0 {
		m.selectedKey = keys[visibleIndexes[m.control.Cursor()]]
	}
	return m
}

func eventKey(event domain.EventSnapshot) string {
	return event.Node + "\x00" + event.Kind + "\x00" + event.Message + "\x00" + "0"
}

// eventKeys assigns duplicate rows a stable occurrence suffix based on their
// order in the current snapshot. Event snapshots expose no immutable event ID.
func eventKeys(events []domain.EventSnapshot) []string {
	seen := make(map[string]int, len(events))
	keys := make([]string, len(events))
	for index, event := range events {
		base := event.Node + "\x00" + event.Kind + "\x00" + event.Message
		keys[index] = base + "\x00" + strconv.Itoa(seen[base])
		seen[base]++
	}
	return keys
}

func eventKeyExists(keys []string, key string) bool {
	if key == "" {
		return false
	}
	for _, candidate := range keys {
		if candidate == key {
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
	indexes := m.visibleEventIndexes(events)
	visible := make([]domain.EventSnapshot, 0, len(indexes))
	for _, index := range indexes {
		visible = append(visible, events[index])
	}
	return visible
}

func (m eventsModel) visibleEventIndexes(events []domain.EventSnapshot) []int {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		indexes := make([]int, len(events))
		for index := range events {
			indexes[index] = index
		}
		return indexes
	}
	filtered := make([]int, 0, len(events))
	for index, event := range events {
		for _, value := range []string{event.Node, event.Kind, event.Message} {
			if strings.Contains(strings.ToLower(value), query) {
				filtered = append(filtered, index)
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
	if state.Status != directPartial {
		return m.tableViewSized(size, state.Value, state.Status == directLoading || state.Status == "", state.Status == directFailed, state.Err)
	}
	warning := "EVENTS PARTIAL"
	if state.Err != "" {
		warning += " — " + state.Err
	}
	if size.Height <= 1 {
		return warning
	}
	size.Height--
	return warning + "\n" + m.tableViewSized(size, state.Value, false, false, state.Err)
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
