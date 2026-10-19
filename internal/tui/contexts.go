package tui

import (
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/domain"
)

type contextsModel struct {
	values  []domain.ClusterContext
	control listControl
	active  bool
	current string
}

func newContextsModel(values []domain.ClusterContext, current string) contextsModel {
	contexts := append([]domain.ClusterContext(nil), values...)
	sort.SliceStable(contexts, func(left, right int) bool {
		return contexts[left].Name < contexts[right].Name
	})

	selected := 0
	for index, clusterContext := range contexts {
		if clusterContext.Name == current {
			selected = index
			break
		}
	}

	model := contextsModel{values: contexts, active: true, current: current}
	model.control.SetCount(len(contexts))
	model.control.SetCursor(selected)
	return model
}

func (m contextsModel) update(message tea.KeyPressMsg, current string) (contextsModel, tea.Cmd) {
	switch message.String() {
	case "esc":
		m.active = false
	case "up", "k":
		m.control.MoveUp(1)
	case "down", "j":
		m.control.MoveDown(1)
	case "enter":
		m.active = false
		if len(m.values) == 0 || m.values[m.control.Cursor()].Name == current {
			return m, nil
		}
		selected := m.values[m.control.Cursor()].Name
		return m, func() tea.Msg {
			return directSelectContext{name: selected}
		}
	}

	return m, nil
}

func (m contextsModel) view() string {
	return m.viewSized(contentSize{Width: 120, Height: len(m.values) + 1})
}

func (m contextsModel) viewSized(size contentSize) string {
	if !m.active {
		return ""
	}
	if len(m.values) == 0 {
		return "No contexts"
	}
	columns := contextColumns(m.current)
	widths := contentColumnWidths(size.Width, columns, m.values)
	header := strings.Builder{}
	header.WriteByte(' ')
	writeTableCells(&header, tableHeaders(columns), widths)
	lines := []string{renderTableHeader(header.String(), size.Width, defaultK9sSkin())}
	for index, clusterContext := range m.values {
		row := strings.Builder{}
		row.WriteByte(' ')
		writeTableCells(&row, tableRowValues(clusterContext, columns), widths)
		lines = append(lines, renderSelectedRow(row.String(), size.Width, index == m.control.Cursor(), defaultK9sSkin()))
	}
	if len(lines) > size.Height {
		lines = lines[:size.Height]
	}
	return strings.Join(lines, "\n")
}

func contextColumns(current string) []tableColumn[domain.ClusterContext] {
	return []tableColumn[domain.ClusterContext]{
		{header: "NAME", minWidth: 4, value: func(clusterContext domain.ClusterContext) string {
			name := clusterContext.Name
			if name == current {
				name += "(*)"
			}
			return name
		}},
		{header: "CLUSTER", minWidth: 7, value: func(clusterContext domain.ClusterContext) string { return clusterContext.Cluster }},
		{header: "ENDPOINTS", minWidth: 9, grow: true, value: func(clusterContext domain.ClusterContext) string {
			return strings.Join(clusterContext.Endpoints, ",")
		}},
		{header: "NODES", minWidth: 5, value: func(clusterContext domain.ClusterContext) string {
			return strconv.Itoa(len(clusterContext.Nodes))
		}},
	}
}
