package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/table"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type memoryMetric struct {
	Label string
	Value string
}

type memoryModel struct {
	state     application.MemoryState
	filter    string
	filtering bool
	table     table.Model
}

func newMemoryModel(state application.MemoryState) memoryModel {
	return (memoryModel{state: state, table: table.New()}).normalizeSelection(0)
}

func (m memoryModel) setState(state application.MemoryState) memoryModel {
	previousIndex := m.table.Cursor()
	m.state = state
	return m.normalizeSelection(previousIndex)
}

func (m memoryModel) update(message tea.KeyPressMsg) memoryModel {
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

func (m memoryModel) normalizeSelection(closest int) memoryModel {
	metrics := m.visibleMetrics()
	m.table.SetRows(make([]table.Row, len(metrics)))
	m.table.SetCursor(min(max(closest, 0), max(0, len(metrics)-1)))
	return m
}

func (m memoryModel) visibleMetrics() []memoryMetric {
	metrics := memoryRows(m.state.Value)
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return metrics
	}
	filtered := make([]memoryMetric, 0, len(metrics))
	for _, metric := range metrics {
		if strings.Contains(strings.ToLower(metric.Label), query) || strings.Contains(strings.ToLower(metric.Value), query) {
			filtered = append(filtered, metric)
		}
	}
	return filtered
}

func (m memoryModel) view(width int) string {
	contents := renderMemoryTable(width, m.visibleMetrics(), m.table.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m memoryModel) viewSized(size contentSize) string {
	if m.state.Status == application.Failed {
		return fallback(m.state.Err)
	}
	if m.state.Status == application.Loading || m.state.Status == application.Idle {
		return "Loading memory…"
	}
	if m.state.Value.TotalBytes == 0 {
		return "No memory data"
	}
	metrics := m.visibleMetrics()
	if len(metrics) == 0 {
		return "No memory data"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(metrics), m.table.Cursor(), rowCapacity)
	return renderMemoryTable(size.Width, metrics[start:end], m.table.Cursor()-start)
}

const defaultMemoryWidth = 60

func memoryRows(snapshot domain.MemorySnapshot) []memoryMetric {
	percent := 0.0
	if snapshot.TotalBytes > 0 {
		percent = 100 * float64(snapshot.UsedBytes) / float64(snapshot.TotalBytes)
	}
	return []memoryMetric{
		{Label: "Total", Value: formatBytes(int64(snapshot.TotalBytes))},
		{Label: "Used", Value: fmt.Sprintf("%s (%.0f%% of total)", formatBytes(int64(snapshot.UsedBytes)), percent)},
		{Label: "Available", Value: formatBytes(int64(snapshot.AvailableBytes))},
		{Label: "Free", Value: formatBytes(int64(snapshot.FreeBytes))},
		{Label: "Buffers", Value: formatBytes(int64(snapshot.BuffersBytes))},
		{Label: "Cached", Value: formatBytes(int64(snapshot.CachedBytes))},
		{Label: "Swap total", Value: formatBytes(int64(snapshot.SwapTotalBytes))},
		{Label: "Swap free", Value: formatBytes(int64(snapshot.SwapFreeBytes))},
		{Label: "Dirty", Value: formatBytes(int64(snapshot.DirtyBytes))},
		{Label: "Slab", Value: formatBytes(int64(snapshot.SlabBytes))},
	}
}

var memoryColumns = []tableColumn[memoryMetric]{
	{header: "METRIC", minWidth: 16, value: func(m memoryMetric) string { return fallback(m.Label) }},
	{header: "VALUE", grow: true, value: func(m memoryMetric) string { return fallback(m.Value) }},
}

func renderMemoryTable(width int, metrics []memoryMetric, selectedIndex int) string {
	widths := memoryColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeTableCells(&header, memoryHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, metric := range metrics {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeTableCells(&row, memoryRowValues(metric), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func memoryColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultMemoryWidth
	}
	return calculateColumnWidths(width, memoryColumns)
}

func memoryHeaders() []string {
	return tableHeaders(memoryColumns)
}

func memoryRowValues(metric memoryMetric) []string {
	return tableRowValues(metric, memoryColumns)
}
