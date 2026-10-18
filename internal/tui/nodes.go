package tui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type nodesModel struct {
	filter     string
	filtering  bool
	selectedID string
	marked     map[string]struct{}
	control    listControl
	notice     string
}

func newNodesModel() nodesModel { return nodesModel{} }

func (m nodesModel) startFilter(value string, nodes []domain.NodeSnapshot) nodesModel {
	m.filter = value
	m.filtering = true
	m.notice = ""
	return m.normalizeSelection(nodes, m.control.Cursor())
}

func (m nodesModel) update(message tea.KeyPressMsg, nodes []domain.NodeSnapshot) nodesModel {
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			m.notice = ""
			return m.normalizeSelection(nodes, m.control.Cursor())
		case "enter":
			m.filtering = false
			return m
		case "backspace":
			m.filter = trimLastRune(m.filter)
			return m.normalizeSelection(nodes, m.control.Cursor())
		}

		if text := printableText(message.Text); text != "" {
			m.filter += text
			return m.normalizeSelection(nodes, m.control.Cursor())
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
		m = m.normalizeSelection(nodes, m.control.Cursor())
	case "up", "k":
		m = m.moveSelection(nodes, -1)
	case "down", "j":
		m = m.moveSelection(nodes, 1)
	case "g":
		m.selectedID = ""
		m = m.normalizeSelection(nodes, 0)
	case "G":
		m.selectedID = ""
		m = m.normalizeSelection(nodes, len(m.visibleNodes(nodes))-1)
	case "space":
		if node, ok := m.selected(nodes); ok {
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

func (m nodesModel) moveSelection(snapshot []domain.NodeSnapshot, delta int) nodesModel {
	nodes := m.visibleNodes(snapshot)
	if len(nodes) == 0 {
		return m.normalizeSelection(snapshot, 0)
	}
	if delta < 0 {
		m.control.MoveUp(-delta)
	} else {
		m.control.MoveDown(delta)
	}
	m.selectedID = nodes[m.control.Cursor()].ID
	m.notice = ""
	return m
}

func (m nodesModel) normalizeSelection(snapshot []domain.NodeSnapshot, closest int) nodesModel {
	nodes := m.visibleNodes(snapshot)
	m.control.SetCount(len(nodes))
	if len(nodes) == 0 {
		m.control.SetCursor(0)
		m.selectedID = ""
		return m
	}

	if m.selectedID != "" {
		for index, node := range nodes {
			if node.ID == m.selectedID {
				m.control.SetCursor(index)
				return m
			}
		}
	}

	m.control.SetCursor(min(max(closest, 0), len(nodes)-1))
	m.selectedID = nodes[m.control.Cursor()].ID
	return m
}

func (m nodesModel) selectID(id string, snapshot []domain.NodeSnapshot) nodesModel {
	for index, node := range m.visibleNodes(snapshot) {
		if node.ID == id {
			m.selectedID = id
			return m.normalizeSelection(snapshot, index)
		}
	}
	return m
}

func (m nodesModel) selected(snapshot []domain.NodeSnapshot) (domain.NodeSnapshot, bool) {
	nodes := m.visibleNodes(snapshot)
	cursor := m.control.Cursor()
	if len(nodes) == 0 || cursor < 0 || cursor >= len(nodes) {
		return domain.NodeSnapshot{}, false
	}
	return nodes[cursor], true
}

func (m nodesModel) selectedValue(snapshot []domain.NodeSnapshot) domain.NodeSnapshot {
	node, _ := m.selected(snapshot)
	return node
}

func (m nodesModel) isMarked(id string) bool {
	if m.marked == nil {
		return false
	}
	_, ok := m.marked[id]
	return ok
}

func (m nodesModel) actionTargets(snapshot []domain.NodeSnapshot) []string {
	if len(m.marked) > 0 {
		targets := make([]string, 0, len(m.marked))
		for _, node := range snapshot {
			if m.isMarked(node.ID) {
				targets = append(targets, node.Target())
			}
		}
		return targets
	}
	if node, ok := m.selected(snapshot); ok {
		return []string{node.Target()}
	}
	return nil
}

func (m nodesModel) visibleNodes(snapshot []domain.NodeSnapshot) []domain.NodeSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return snapshot
	}

	filtered := make([]domain.NodeSnapshot, 0, len(snapshot))
	for _, node := range snapshot {
		values := []string{
			node.DisplayName(),
			string(node.Role),
			node.Stage,
			string(node.Health),
			serviceSummary(node.Services),
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

func (m nodesModel) view(width int, snapshot []domain.NodeSnapshot) string {
	contents := renderNodeTable(width, m.visibleNodes(snapshot), m.control.Cursor(), m.marked)
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	if m.notice != "" {
		contents += "\n" + m.notice
	}
	return contents
}

func (m nodesModel) viewSized(size contentSize, state application.NodeState) string {
	if state.Status == application.Failed {
		return fallback(state.Err)
	}
	nodes := m.visibleNodes(state.Value.Nodes)
	if len(nodes) == 0 {
		if state.Status == application.Loading || state.Status == application.Idle {
			return "Loading nodes…"
		}
		return "No nodes"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(nodes), m.control.Cursor(), rowCapacity)
	return renderNodeTable(size.Width, nodes[start:end], m.control.Cursor()-start, m.marked)
}

func renderNodes(width int, state application.NodeState) string {
	model := newNodesModel().normalizeSelection(state.Value.Nodes, 0)
	return model.view(width, state.Value.Nodes)
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
