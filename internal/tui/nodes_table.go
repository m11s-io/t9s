package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/m11s-io/t9s/internal/domain"
)

const (
	defaultNodesWidth = 120
	selectionWidth    = 2
	columnSpacing     = 1
)

type nodeColumn struct {
	header   string
	minWidth int
	value    func(domain.NodeSnapshot) string
	grow     bool
}

var nodeColumns = []nodeColumn{
	{header: "NAME", minWidth: 12, grow: true, value: func(node domain.NodeSnapshot) string { return node.DisplayName() }},
	{header: "ROLE", minWidth: 7, value: func(node domain.NodeSnapshot) string { return fallback(string(node.Role)) }},
	{header: "STAGE", minWidth: 11, value: func(node domain.NodeSnapshot) string { return fallback(node.Stage) }},
	{header: "HEALTH", minWidth: 6, value: func(node domain.NodeSnapshot) string { return healthSymbol(node.Health) }},
	{header: "SERVICES", minWidth: 8, value: func(node domain.NodeSnapshot) string { return node.Services.CompactString() }},
	{header: "K8S", minWidth: 7, value: func(node domain.NodeSnapshot) string { return fallback(string(node.Kubernetes)) }},
	{header: "VERSION", minWidth: 8, value: func(node domain.NodeSnapshot) string { return fallback(node.Version) }},
}

func renderNodeTable(width int, nodes []domain.NodeSnapshot, selectedIndex int, marked map[string]struct{}) string {
	widths := nodeColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeNodeCells(&header, nodeHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, node := range nodes {
		output.WriteByte('\n')
		row := strings.Builder{}
		if _, ok := marked[node.ID]; ok {
			row.WriteString("● ")
		} else {
			row.WriteString("  ")
		}
		writeNodeCells(&row, nodeRowValues(node), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func nodeColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultNodesWidth
	}

	widths := make([]int, len(nodeColumns))
	used := selectionWidth + columnSpacing*(len(nodeColumns)-1)
	growIndex := 0
	for index, column := range nodeColumns {
		widths[index] = column.minWidth
		used += column.minWidth
		if column.grow {
			growIndex = index
		}
	}
	if remaining := width - used; remaining > 0 {
		widths[growIndex] += remaining
	}
	return widths
}

func nodeHeaders() []string {
	headers := make([]string, len(nodeColumns))
	for index, column := range nodeColumns {
		headers[index] = column.header
	}
	return headers
}

func nodeRowValues(node domain.NodeSnapshot) []string {
	values := make([]string, len(nodeColumns))
	for index, column := range nodeColumns {
		values[index] = column.value(node)
	}
	return values
}

func writeNodeCells(output *strings.Builder, values []string, widths []int) {
	for index, value := range values {
		if index > 0 {
			output.WriteByte(' ')
		}
		cell := ansi.Truncate(value, widths[index], "…")
		output.WriteString(cell)
		if padding := widths[index] - ansi.StringWidth(cell); padding > 0 && index < len(values)-1 {
			output.WriteString(strings.Repeat(" ", padding))
		}
	}
}

func healthSymbol(health domain.Health) string {
	switch health {
	case domain.HealthHealthy:
		return "✓"
	case domain.HealthUnhealthy:
		return "!"
	default:
		return "?"
	}
}
