package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/domain"
)

type disksModel struct {
	changes   tableChanges
	status    directLoadStatus
	values    domain.DiskSet
	node      string
	err       string
	filter    string
	filtering bool
	control   listControl
}

func newDirectDisksModel() disksModel { return disksModel{status: directLoading} }

func (m disksModel) setResult(node string, values domain.DiskSet, err error) disksModel {
	selectedDevice, hadSelection := m.selectedDevice()
	previousIndex := m.control.Cursor()
	m.node, m.values, m.err, m.status = node, values, "", directReady
	if err != nil {
		m.status, m.err = directFailed, "Disk data is unavailable"
		if len(values.Disks) > 0 {
			m.status, m.err = directPartial, "Some disk data is unavailable"
		}
	}
	m = m.normalizeSelection(previousIndex)
	if hadSelection {
		for index, disk := range m.visibleDisks() {
			if disk.DeviceName == selectedDevice {
				m.control.SetCursor(index)
				break
			}
		}
	}
	return m
}

func (m disksModel) selectedDevice() (string, bool) {
	disk, ok := m.selected()
	return disk.DeviceName, ok
}

func (m disksModel) startFilter(value string) disksModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(m.control.Cursor())
}

func (m disksModel) update(message tea.KeyPressMsg) disksModel {
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			return m.normalizeSelection(m.control.Cursor())
		case "enter":
			m.filtering = false
			return m
		case "backspace":
			m.filter = trimLastRune(m.filter)
			return m.normalizeSelection(m.control.Cursor())
		}
		if text := printableText(message.Text); text != "" {
			m.filter += text
			return m.normalizeSelection(m.control.Cursor())
		}
		return m
	}

	switch key {
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m = m.normalizeSelection(m.control.Cursor())
	case "up", "k":
		m.control.MoveUp(1)
	case "down", "j":
		m.control.MoveDown(1)
	case "g":
		m.control.GotoTop()
	case "G":
		m.control.GotoBottom()
	}
	return m
}

func (m disksModel) normalizeSelection(closest int) disksModel {
	disks := m.visibleDisks()
	m.control.SetCount(len(disks))
	m.control.SetCursor(min(max(closest, 0), max(0, len(disks)-1)))
	return m
}

func (m disksModel) selected() (domain.DiskSnapshot, bool) {
	disks := m.visibleDisks()
	cursor := m.control.Cursor()
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
		return m.values.Disks
	}
	filtered := make([]domain.DiskSnapshot, 0, len(m.values.Disks))
	for _, disk := range m.values.Disks {
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
	contents := renderDiskTable(width, m.visibleDisks(), m.control.Cursor(), m.changes)
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m disksModel) viewSized(size contentSize) string {
	if m.status == directFailed {
		return fallback(m.err)
	}
	disks := m.visibleDisks()
	if len(disks) == 0 {
		if m.status == directLoading {
			return "Loading disks…"
		}
		if m.status == directFailed || m.status == directPartial {
			return fallback(m.err)
		}
		return "No disks"
	}
	warningRows := 0
	if m.status == directPartial {
		warningRows = 1
	}
	rowCapacity := max(0, size.Height-1-warningRows)
	start, end := resourceWindow(len(disks), m.control.Cursor(), rowCapacity)
	content := renderDiskRows(size.Width, disks[start:end], disks, m.control.Cursor()-start, m.changes)
	if warningRows > 0 {
		content = "DISKS PARTIAL: " + fallback(m.err) + "\n" + content
	}
	return content
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
	{header: "MODEL", minWidth: 20, value: func(d domain.DiskSnapshot) string { return fallback(d.Model) }},
	{header: "SYSTEM", minWidth: 6, value: func(d domain.DiskSnapshot) string { return diskSystemSymbol(d.SystemDisk) }},
}

func renderDiskTable(width int, disks []domain.DiskSnapshot, selectedIndex int, changes ...tableChanges) string {
	return renderDiskRows(width, disks, disks, selectedIndex, changes...)
}

func renderDiskRows(width int, disks, widthValues []domain.DiskSnapshot, selectedIndex int, changes ...tableChanges) string {
	widths := diskColumnWidths(width, widthValues)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString(" ")
	writeTableCells(&header, diskHeaders(), widths)
	output.WriteString(renderTableHeader(header.String(), width, defaultK9sSkin()))

	for index, disk := range disks {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString(" ")
		writeTableCells(&row, diskRowValues(disk), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, changedRowSkin(disk.DeviceName, changes)))
	}

	return output.String()
}

func diskColumnWidths(width int, disks []domain.DiskSnapshot) []int {
	if width <= 0 {
		width = defaultDisksWidth
	}
	return contentColumnWidths(width, diskColumns, disks)
}

func diskHeaders() []string {
	return tableHeaders(diskColumns)
}

func diskRowValues(disk domain.DiskSnapshot) []string {
	return tableRowValues(disk, diskColumns)
}
