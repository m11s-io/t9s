package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/table"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type contextsModel struct {
	values  []domain.ClusterContext
	table   table.Model
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

	model := contextsModel{values: contexts, table: table.New(), active: true, current: current}
	model.table.SetRows(make([]table.Row, len(contexts)))
	model.table.SetCursor(selected)
	return model
}

func (m contextsModel) update(message tea.KeyPressMsg, current string) (contextsModel, tea.Cmd) {
	switch message.String() {
	case "esc":
		m.active = false
	case "up", "k":
		m.table.MoveUp(1)
	case "down", "j":
		m.table.MoveDown(1)
	case "enter":
		m.active = false
		if len(m.values) == 0 || m.values[m.table.Cursor()].Name == current {
			return m, nil
		}
		selected := m.values[m.table.Cursor()].Name
		return m, func() tea.Msg {
			return applicationMessage{message: application.SelectContext{Name: selected}}
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
	lines := []string{renderSelectedRow("  NAME                  CLUSTER               ENDPOINTS             NODES", size.Width, false, defaultK9sSkin())}
	for index, clusterContext := range m.values {
		name := clusterContext.Name
		if clusterContext.Name == m.current {
			name += "(*)"
		}
		row := fmt.Sprintf("  %-20s  %-20s  %-20s  %d", name, clusterContext.Cluster, strings.Join(clusterContext.Endpoints, ","), len(clusterContext.Nodes))
		lines = append(lines, renderSelectedRow(row, size.Width, index == m.table.Cursor(), defaultK9sSkin()))
	}
	if len(lines) > size.Height {
		lines = lines[:size.Height]
	}
	return strings.Join(lines, "\n")
}
