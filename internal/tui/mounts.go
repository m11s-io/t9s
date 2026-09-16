package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/table"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type mountsModel struct {
	state     application.MountState
	filter    string
	filtering bool
	table     table.Model
}

func newMountsModel(state application.MountState) mountsModel {
	return (mountsModel{state: state, table: table.New()}).normalizeSelection(0)
}

func (m mountsModel) setState(state application.MountState) mountsModel {
	previousIndex := m.table.Cursor()
	m.state = state
	return m.normalizeSelection(previousIndex)
}

func (m mountsModel) update(message tea.KeyPressMsg) mountsModel {
	key := message.String()
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

func (m mountsModel) normalizeSelection(closest int) mountsModel {
	mounts := m.visibleMounts()
	m.table.SetRows(make([]table.Row, len(mounts)))
	m.table.SetCursor(min(max(closest, 0), max(0, len(mounts)-1)))
	return m
}

func (m mountsModel) visibleMounts() []domain.MountSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.state.Value.Mounts
	}
	filtered := make([]domain.MountSnapshot, 0, len(m.state.Value.Mounts))
	for _, mount := range m.state.Value.Mounts {
		if mountMatches(mount, query) {
			filtered = append(filtered, mount)
		}
	}
	return filtered
}

func mountMatches(mount domain.MountSnapshot, query string) bool {
	for _, value := range []string{mount.Filesystem, mount.MountedOn} {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func (m mountsModel) view(width int) string {
	contents := renderMountsTable(width, m.visibleMounts(), m.table.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m mountsModel) viewSized(size contentSize) string {
	if m.state.Status == application.Failed {
		return fallback(m.state.Err)
	}
	mounts := m.visibleMounts()
	if len(mounts) == 0 {
		if m.state.Status == application.Loading || m.state.Status == application.Idle {
			return "Loading mounts…"
		}
		return "No mounts"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(mounts), m.table.Cursor(), rowCapacity)
	return renderMountsTable(size.Width, mounts[start:end], m.table.Cursor()-start)
}

const defaultMountsWidth = 120

var mountColumns = []tableColumn[domain.MountSnapshot]{
	{header: "FILESYSTEM", minWidth: 18, value: func(m domain.MountSnapshot) string { return fallback(m.Filesystem) }},
	{header: "SIZE", minWidth: 10, value: func(m domain.MountSnapshot) string { return formatBytes(int64(m.SizeBytes)) }},
	{header: "USED", minWidth: 10, value: func(m domain.MountSnapshot) string { return formatBytes(int64(m.UsedBytes)) }},
	{header: "AVAIL", minWidth: 10, value: func(m domain.MountSnapshot) string { return formatBytes(int64(m.AvailableBytes)) }},
	{header: "USE%", minWidth: 6, value: func(m domain.MountSnapshot) string { return fmt.Sprintf("%.0f%%", m.UsedPercent) }},
	{header: "MOUNTED", minWidth: 20, grow: true, value: func(m domain.MountSnapshot) string { return fallback(m.MountedOn) }},
}

func renderMountsTable(width int, mounts []domain.MountSnapshot, selectedIndex int) string {
	widths := mountsColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeTableCells(&header, mountsHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, mount := range mounts {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeTableCells(&row, mountsRowValues(mount), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func mountsColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultMountsWidth
	}
	return calculateColumnWidths(width, mountColumns)
}

func mountsHeaders() []string {
	return tableHeaders(mountColumns)
}

func mountsRowValues(mount domain.MountSnapshot) []string {
	return tableRowValues(mount, mountColumns)
}
