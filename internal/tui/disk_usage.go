package tui

import (
	"strings"

	"charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type diskUsageModel struct {
	status    directLoadStatus
	node      string
	path      string
	rows      []domain.DiskUsageEntry
	err       string
	partial   bool
	filter    string
	filtering bool
	control   listControl
	stream    ports.DiskUsageStream
}

func newDiskUsageModel(node, root string) diskUsageModel {
	return diskUsageModel{status: directLoading, node: node, path: root}
}

func (m *diskUsageModel) attach(stream ports.DiskUsageStream) {
	m.stream = stream
}

func (m *diskUsageModel) close() {
	if m.stream != nil {
		_ = m.stream.Close()
		m.stream = nil
	}
}

func (m *diskUsageModel) accept(batch domain.DiskUsageBatch, err error) bool {
	m.rows = append(m.rows, batch.Rows...)
	*m = m.normalizeSelection(m.control.Cursor())
	partial := m.partial || batch.Truncated || batch.Err != ""
	for _, row := range batch.Rows {
		partial = partial || row.Error != ""
	}
	m.partial = partial
	if err != nil {
		if len(m.rows) == 0 {
			m.status, m.err = directFailed, "Directory usage is unavailable"
		} else {
			m.status, m.err = directPartial, "Directory usage ended before the scan completed"
		}
		return true
	}
	if batch.Err != "" {
		m.err = sanitizeUntrustedText(batch.Err)
	} else if batch.Truncated {
		m.err = "Directory scan was truncated"
	} else if partial {
		m.err = "Some directory entries are unavailable"
	}
	if batch.EOF {
		if m.partial {
			m.status = directPartial
		} else {
			m.status = directReady
		}
		return true
	}
	m.status = directLoading
	return false
}

func (m diskUsageModel) update(message tea.KeyPressMsg) diskUsageModel {
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter, m.filtering = "", false
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

func (m diskUsageModel) normalizeSelection(closest int) diskUsageModel {
	rows := m.visibleRows()
	m.control.SetCount(len(rows))
	m.control.SetCursor(min(max(closest, 0), max(0, len(rows)-1)))
	return m
}

func (m diskUsageModel) visibleRows() []domain.DiskUsageEntry {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.rows
	}
	filtered := make([]domain.DiskUsageEntry, 0, len(m.rows))
	for _, row := range m.rows {
		if strings.Contains(strings.ToLower(row.Path), query) || strings.Contains(strings.ToLower(row.Error), query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (m diskUsageModel) viewSized(size contentSize) string {
	if m.status == directLoading && len(m.rows) == 0 {
		return "Scanning " + sanitizeUntrustedText(m.path) + "…"
	}
	if m.status == directFailed && len(m.rows) == 0 {
		return fallback(m.err)
	}
	rows := m.visibleRows()
	fixedRows := 1 // table header
	if m.status == directPartial || m.status == directLoading && len(m.rows) > 0 {
		fixedRows++
	}
	if m.filtering || m.filter != "" {
		fixedRows++
	}
	capacity := max(0, size.Height-fixedRows)
	start, end := resourceWindow(len(rows), m.control.Cursor(), capacity)
	content := renderDiskUsageTable(size.Width, rows[start:end], m.control.Cursor()-start)
	if m.status == directPartial {
		content = "SCAN PARTIAL: " + fallback(m.err) + "\n" + content
	}
	if m.status == directLoading && len(m.rows) > 0 {
		content = "SCANNING: " + sanitizeUntrustedText(m.path) + "\n" + content
	}
	if m.filtering || m.filter != "" {
		content += "\nFILTER " + sanitizeUntrustedText(m.filter)
	}
	if len(rows) == 0 && m.status != directLoading {
		content = "No directory entries"
		if m.status == directPartial {
			content = "SCAN PARTIAL: " + fallback(m.err) + "\n" + content
		}
	}
	return content
}

var diskUsageColumns = []tableColumn[domain.DiskUsageEntry]{
	{header: "PATH", minWidth: 22, grow: true, value: func(row domain.DiskUsageEntry) string { return sanitizeUntrustedText(row.Path) }},
	{header: "SIZE", minWidth: 10, value: func(row domain.DiskUsageEntry) string {
		if row.Error != "" {
			return "-"
		}
		return formatBytes(row.SizeBytes)
	}},
	{header: "ERROR", minWidth: 16, grow: true, value: func(row domain.DiskUsageEntry) string { return sanitizeUntrustedText(row.Error) }},
}

func renderDiskUsageTable(width int, rows []domain.DiskUsageEntry, selectedIndex int) string {
	widths := calculateColumnWidths(width, diskUsageColumns)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteByte(' ')
	writeTableCells(&header, tableHeaders(diskUsageColumns), widths)
	output.WriteString(renderTableHeader(header.String(), width, defaultK9sSkin()))
	for index, rowValue := range rows {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteByte(' ')
		writeTableCells(&row, tableRowValues(rowValue, diskUsageColumns), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}
	return output.String()
}
