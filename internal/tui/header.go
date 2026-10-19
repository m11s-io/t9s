package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const k9sHeaderHeight = 7
const headerHorizontalInset = 1

type headerLayout struct {
	Width         int
	Height        int
	MetadataWidth int
	ActionsWidth  int
	LogoWidth     int
	ShowLogo      bool
}

func layoutK9sHeader(width int) headerLayout {
	width = max(0, width)
	layout := headerLayout{Width: width, Height: k9sHeaderHeight}
	contentWidth := max(0, width-2*headerHorizontalInset)
	if width >= 120 {
		layout.MetadataWidth, layout.LogoWidth, layout.ShowLogo = 50, 26, true
	} else if width >= 60 {
		layout.MetadataWidth = min(32, contentWidth/2)
	} else {
		layout.MetadataWidth = contentWidth
	}
	layout.ActionsWidth = max(0, contentWidth-layout.MetadataWidth-layout.LogoWidth)
	return layout
}

func renderK9sHeader(layout headerLayout, metadata shellMetadata, hints []actionHint, skin k9sSkin) string {
	metadataLines := renderMetadata(metadata, layout.MetadataWidth, skin)
	actionLines := renderHeaderActions(hints, layout.ActionsWidth, skin)
	logoLines := make([]string, k9sHeaderHeight)
	if layout.ShowLogo {
		copy(logoLines, strings.Split(renderT9SLogo(skin), "\n"))
	}

	lines := make([]string, k9sHeaderHeight)
	for row := range lines {
		line := strings.Repeat(" ", headerHorizontalInset) +
			fitK9sCell(metadataLines[row], layout.MetadataWidth) +
			fitK9sCell(actionLines[row], layout.ActionsWidth)
		if layout.ShowLogo {
			line += fitK9sCell(logoLines[row], layout.LogoWidth)
		}
		line += strings.Repeat(" ", headerHorizontalInset)
		lines[row] = fitK9sCell(line, layout.Width)
	}
	return strings.Join(lines, "\n")
}

func renderMetadata(metadata shellMetadata, width int, skin k9sSkin) []string {
	values := [][2]string{
		{"Context:", strings.TrimSpace(metadata.Context + " " + metadata.Mode)},
		{"Cluster:", metadata.Cluster},
		{"Nodes:", metadata.NodeSummary},
		{"T9s Rev:", metadata.AppVersion},
		{"Talos Rev:", metadata.TalosVersion},
		{"Health:", metadata.Health},
		{"Endpoints:", metadata.EndpointSummary},
	}
	labelWidth := 0
	for _, value := range values {
		labelWidth = max(labelWidth, ansi.StringWidth(value[0]))
	}
	lines := make([]string, k9sHeaderHeight)
	for index, value := range values {
		label := skin.metadataLabel.Render(value[0] + strings.Repeat(" ", labelWidth-ansi.StringWidth(value[0])))
		text := skin.metadataValue.Render(value[1])
		lines[index] = label + " " + text
	}
	return lines
}

func renderHeaderActions(hints []actionHint, width int, skin k9sSkin) []string {
	lines := make([]string, k9sHeaderHeight)
	if width <= 0 {
		return lines
	}
	const columnGap = 6
	actionRows := 6
	if width < 64 {
		actionRows = k9sHeaderHeight
	}
	columns := max(1, (len(hints)+actionRows-1)/actionRows)
	columnWidths := make([]int, columns)
	for index, hint := range hints {
		column := index / actionRows
		columnWidths[column] = max(columnWidths[column], ansi.StringWidth("<"+hint.Key+"> "+hint.Label))
	}
	gap := min(columnGap, width/max(1, columns-1))
	availableWidth := max(0, width-gap*(columns-1))
	totalWidth := 0
	for _, columnWidth := range columnWidths {
		totalWidth += columnWidth
	}
	if totalWidth > availableWidth {
		for column := range columnWidths {
			columnWidths[column] = availableWidth / columns
			if column < availableWidth%columns {
				columnWidths[column]++
			}
		}
	}

	for row := 0; row < actionRows; row++ {
		for column, columnWidth := range columnWidths {
			index := column*actionRows + row
			if index >= len(hints) {
				break
			}
			if column > 0 && gap > 0 {
				lines[row] += strings.Repeat(" ", gap)
			}
			hint := hints[index]
			cell := skin.actionKey.Render("<"+hint.Key+">") +
				" " +
				skin.actionText.Render(hint.Label)
			lines[row] += fitK9sCell(cell, columnWidth)
		}
	}
	return lines
}

func fitK9sCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = ansi.Truncate(value, width, "")
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}
