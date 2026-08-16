package tui

import (
	"strings"

	"github.com/m11s-io/t9s/internal/domain"
)

const (
	defaultNodesWidth = 120
	selectionWidth    = 2
	columnSpacing     = 1
)

var nodeColumns = []tableColumn[domain.NodeSnapshot]{
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
	writeTableCells(&header, nodeHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, node := range nodes {
		output.WriteByte('\n')
		row := strings.Builder{}
		if _, ok := marked[node.ID]; ok {
			row.WriteString("● ")
		} else {
			row.WriteString("  ")
		}
		writeTableCells(&row, nodeRowValues(node), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func nodeColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultNodesWidth
	}
	return calculateColumnWidths(width, nodeColumns)
}

func nodeHeaders() []string {
	return tableHeaders(nodeColumns)
}

func nodeRowValues(node domain.NodeSnapshot) []string {
	return tableRowValues(node, nodeColumns)
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
