package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/domain"
)

const (
	defaultNodesWidth = 120
	selectionWidth    = 1
	columnSpacing     = 1
	nodesRightInset   = 4
)

var nodeColumns = []tableColumn[domain.NodeSnapshot]{
	{header: "NAME", minWidth: 12, value: func(node domain.NodeSnapshot) string { return node.DisplayName() }},
	{header: "ROLE", minWidth: 7, value: func(node domain.NodeSnapshot) string { return nodeRoleDisplayValue(node) }},
	{header: "STAGE", minWidth: 11, value: func(node domain.NodeSnapshot) string { return fallback(node.Stage) }},
	{header: "HEALTH", minWidth: 6, value: func(node domain.NodeSnapshot) string { return healthSymbol(node.Health) }},
	{header: "K8S", minWidth: 7, value: func(node domain.NodeSnapshot) string { return fallback(string(node.Kubernetes)) }},
	{header: "SERVICES", minWidth: 8, value: func(node domain.NodeSnapshot) string { return compactServiceSummary(node.Services) }},
	{header: "VERSION", minWidth: 8, value: func(node domain.NodeSnapshot) string { return fallback(node.Version) }},
}

func nodeRoleDisplayValue(node domain.NodeSnapshot) string {
	if node.Role != domain.NodeRoleUnknown && node.Role != "" {
		return string(node.Role)
	}
	if node.KubernetesNode != nil && len(node.KubernetesNode.Roles) > 0 {
		return strings.Join(node.KubernetesNode.Roles, ",")
	}
	return fallback(string(node.Role))
}

func renderNodeTable(width int, nodes []domain.NodeSnapshot, selectedIndex int, marked map[string]struct{}, changes ...map[string]time.Time) string {
	widths := nodeColumnWidths(width, nodes)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString(" ")
	writeTableCells(&header, nodeHeaders(), widths)
	output.WriteString(renderTableHeader(header.String(), width, defaultK9sSkin()))

	for index, node := range nodes {
		output.WriteByte('\n')
		row := strings.Builder{}
		if _, ok := marked[node.ID]; ok {
			row.WriteString("●")
		} else {
			row.WriteString(" ")
		}
		writeTableCells(&row, nodeRowValues(node), widths)
		skin := defaultK9sSkin()
		if len(changes) > 0 {
			if _, changed := changes[0][node.ID]; changed {
				skin.tableRow = skin.tableRow.Foreground(lipgloss.Color("#FFA500"))
			}
		}
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, skin))
	}

	return output.String()
}

func nodeColumnWidths(width int, nodes []domain.NodeSnapshot) []int {
	if width <= 0 {
		width = defaultNodesWidth
	}
	widths := calculateColumnWidths(width, nodeColumns)
	nameWidth := nodeColumns[0].minWidth
	for _, node := range nodes {
		nameWidth = max(nameWidth, ansi.StringWidth(sanitizeUntrustedText(node.DisplayName())))
	}

	available := width - selectionWidth - columnSpacing*max(0, len(widths)-1)
	for _, columnWidth := range widths[1:] {
		available -= columnWidth
	}
	if available >= nodeColumns[0].minWidth {
		nameWidth = min(nameWidth, available)
	}
	widths[0] = nameWidth

	used := selectionWidth + columnSpacing*max(0, len(widths)-1)
	for _, columnWidth := range widths {
		used += columnWidth
	}
	if extra := width - used - nodesRightInset; extra > 0 && len(widths) > 1 {
		gapCount := len(widths) - 1
		perGap, remainder := extra/gapCount, extra%gapCount
		for index := 0; index < gapCount; index++ {
			widths[index] += perGap
			if index < remainder {
				widths[index]++
			}
		}
	}
	return widths
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
