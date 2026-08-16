package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/table"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type disksModel struct {
	state     application.DisksState
	filter    string
	filtering bool
	table     table.Model
}

func newDisksModel(state application.DisksState) disksModel {
	return (disksModel{state: state, table: table.New()}).normalizeSelection(0)
}

func (m disksModel) setState(state application.DisksState) disksModel {
	previousIndex := m.table.Cursor()
	m.state = state
	return m.normalizeSelection(previousIndex)
}

func (m disksModel) startFilter(value string) disksModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(m.table.Cursor())
}

func (m disksModel) update(message tea.KeyPressMsg) disksModel {
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

func (m disksModel) normalizeSelection(closest int) disksModel {
	disks := m.visibleDisks()
	m.table.SetRows(make([]table.Row, len(disks)))
	m.table.SetCursor(min(max(closest, 0), max(0, len(disks)-1)))
	return m
}

func (m disksModel) selected() (domain.DiskSnapshot, bool) {
	disks := m.visibleDisks()
	cursor := m.table.Cursor()
	if len(disks) == 0 || cursor < 0 || cursor >= len(disks) {
		return domain.DiskSnapshot{}, false
	}
	return disks[cursor], true
}

func (m disksModel) selectedValue() domain.DiskSnapshot {
	disk, _ := m.selected()
	return disk
}

func (m disksModel) visibleDisks() []domain.DiskSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.state.Value.Disks
	}
	filtered := make([]domain.DiskSnapshot, 0, len(m.state.Value.Disks))
	for _, disk := range m.state.Value.Disks {
		values := []string{disk.DeviceName, disk.Model, disk.Type, disk.Serial}
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), query) {
				filtered = append(filtered, disk)
				break
			}
		}
	}
	return filtered
}

func (m disksModel) view(width int) string {
	contents := renderDiskTable(width, m.visibleDisks(), m.table.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m disksModel) viewSized(size contentSize) string {
	if m.state.Status == application.Failed {
		return fallback(m.state.Err)
	}
	disks := m.visibleDisks()
	if len(disks) == 0 {
		if m.state.Status == application.Loading || m.state.Status == application.Idle {
			return "Loading disks…"
		}
		return "No disks"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(disks), m.table.Cursor(), rowCapacity)
	return renderDiskTable(size.Width, disks[start:end], m.table.Cursor()-start)
}

const defaultDisksWidth = 120

func diskSystemSymbol(systemDisk bool) string {
	if systemDisk {
		return "✓"
	}
	return ""
}

var diskColumns = []tableColumn[domain.DiskSnapshot]{
	{header: "DEVICE", minWidth: 10, value: func(d domain.DiskSnapshot) string { return fallback(d.DeviceName) }},
	{header: "TYPE", minWidth: 6, value: func(d domain.DiskSnapshot) string { return fallback(d.Type) }},
	{header: "SIZE", minWidth: 10, value: func(d domain.DiskSnapshot) string { return formatBytes(int64(d.SizeBytes)) }},
	{header: "MODEL", minWidth: 20, grow: true, value: func(d domain.DiskSnapshot) string { return fallback(d.Model) }},
	{header: "SYSTEM", minWidth: 6, value: func(d domain.DiskSnapshot) string { return diskSystemSymbol(d.SystemDisk) }},
}

func renderDiskTable(width int, disks []domain.DiskSnapshot, selectedIndex int) string {
	widths := diskColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeTableCells(&header, diskHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, disk := range disks {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeTableCells(&row, diskRowValues(disk), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func diskColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultDisksWidth
	}
	return calculateColumnWidths(width, diskColumns)
}

func diskHeaders() []string {
	return tableHeaders(diskColumns)
}

func diskRowValues(disk domain.DiskSnapshot) []string {
	return tableRowValues(disk, diskColumns)
}
