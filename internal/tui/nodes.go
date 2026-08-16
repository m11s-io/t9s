package tui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/table"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type nodesModel struct {
	state      application.NodeState
	filter     string
	filtering  bool
	selectedID string
	marked     map[string]struct{}
	table      table.Model
	notice     string
}

func newNodesModel(state application.NodeState) nodesModel {
	model := nodesModel{state: state, table: table.New()}
	return model.normalizeSelection(0)
}

func (m nodesModel) setState(state application.NodeState) nodesModel {
	previousIndex := m.table.Cursor()
	m.state = state
	return m.normalizeSelection(previousIndex)
}

func (m nodesModel) startFilter(value string) nodesModel {
	m.filter = value
	m.filtering = true
	m.notice = ""
	return m.normalizeSelection(m.table.Cursor())
}

func (m nodesModel) update(message tea.KeyPressMsg) nodesModel {
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			m.notice = ""
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
		m.notice = ""
	case "esc":
		m.filter = ""
		m.notice = ""
		m = m.normalizeSelection(m.table.Cursor())
	case "up", "k":
		m = m.moveSelection(-1)
	case "down", "j":
		m = m.moveSelection(1)
	case "g":
		m.selectedID = ""
		m = m.normalizeSelection(0)
	case "G":
		m.selectedID = ""
		m = m.normalizeSelection(len(m.visibleNodes()) - 1)
	case "space":
		if node, ok := m.selected(); ok {
			if m.marked == nil {
				m.marked = make(map[string]struct{})
			}
			if _, ok := m.marked[node.ID]; ok {
				delete(m.marked, node.ID)
			} else {
				m.marked[node.ID] = struct{}{}
			}
		}
	}

	return m
}

func (m nodesModel) moveSelection(delta int) nodesModel {
	nodes := m.visibleNodes()
	if len(nodes) == 0 {
		return m.normalizeSelection(0)
	}
	if delta < 0 {
		m.table.MoveUp(-delta)
	} else {
		m.table.MoveDown(delta)
	}
	m.selectedID = nodes[m.table.Cursor()].ID
	m.notice = ""
	return m
}

func (m nodesModel) normalizeSelection(closest int) nodesModel {
	nodes := m.visibleNodes()
	m.table.SetRows(make([]table.Row, len(nodes)))
	if len(nodes) == 0 {
		m.table.SetCursor(0)
		m.selectedID = ""
		return m
	}

	if m.selectedID != "" {
		for index, node := range nodes {
			if node.ID == m.selectedID {
				m.table.SetCursor(index)
				return m
			}
		}
	}

	m.table.SetCursor(min(max(closest, 0), len(nodes)-1))
	m.selectedID = nodes[m.table.Cursor()].ID
	return m
}

func (m nodesModel) selected() (domain.NodeSnapshot, bool) {
	nodes := m.visibleNodes()
	cursor := m.table.Cursor()
	if len(nodes) == 0 || cursor < 0 || cursor >= len(nodes) {
		return domain.NodeSnapshot{}, false
	}
	return nodes[cursor], true
}

func (m nodesModel) selectedValue() domain.NodeSnapshot { node, _ := m.selected(); return node }

func (m nodesModel) isMarked(id string) bool {
	if m.marked == nil {
		return false
	}
	_, ok := m.marked[id]
	return ok
}

func (m nodesModel) actionTargets() []string {
	if len(m.marked) > 0 {
		targets := make([]string, 0, len(m.marked))
		for _, node := range m.state.Value.Nodes {
			if m.isMarked(node.ID) {
				targets = append(targets, node.Target())
			}
		}
		return targets
	}
	if node, ok := m.selected(); ok {
		return []string{node.Target()}
	}
	return nil
}

func (m nodesModel) visibleNodes() []domain.NodeSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.state.Value.Nodes
	}

	filtered := make([]domain.NodeSnapshot, 0, len(m.state.Value.Nodes))
	for _, node := range m.state.Value.Nodes {
		values := []string{
			node.DisplayName(),
			string(node.Role),
			node.Stage,
			string(node.Health),
			node.Services.String(),
			node.Version,
		}
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), query) {
				filtered = append(filtered, node)
				break
			}
		}
	}
	return filtered
}

func (m nodesModel) view(width int) string {
	contents := renderNodeTable(width, m.visibleNodes(), m.table.Cursor(), m.marked)
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	if m.notice != "" {
		contents += "\n" + m.notice
	}
	return contents
}

func (m nodesModel) viewSized(size contentSize) string {
	if m.state.Status == application.Failed {
		return fallback(m.state.Err)
	}
	nodes := m.visibleNodes()
	if len(nodes) == 0 {
		if m.state.Status == application.Loading || m.state.Status == application.Idle {
			return "Loading nodes…"
		}
		return "No nodes"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(nodes), m.table.Cursor(), rowCapacity)
	return renderNodeTable(size.Width, nodes[start:end], m.table.Cursor()-start, m.marked)
}

func renderNodes(width int, state application.NodeState) string {
	return newNodesModel(state).view(width)
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	return string(runes[:len(runes)-1])
}

func printableText(value string) string {
	for _, character := range value {
		if unicode.IsControl(character) {
			return ""
		}
	}
	return value
}
