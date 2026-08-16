package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const k9sHeaderHeight = 7

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
	if width >= 120 {
		layout.MetadataWidth, layout.LogoWidth, layout.ShowLogo = 50, 26, true
	} else if width >= 60 {
		layout.MetadataWidth = min(32, width/2)
	} else {
		layout.MetadataWidth = width
	}
	layout.ActionsWidth = max(0, width-layout.MetadataWidth-layout.LogoWidth)
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
		line := fitK9sCell(metadataLines[row], layout.MetadataWidth) + fitK9sCell(actionLines[row], layout.ActionsWidth)
		if layout.ShowLogo {
			line += fitK9sCell(logoLines[row], layout.LogoWidth)
		}
		lines[row] = fitK9sCell(line, layout.Width)
	}
	return strings.Join(lines, "\n")
}

func renderMetadata(metadata shellMetadata, width int, skin k9sSkin) []string {
	values := [][2]string{
		{"Context:", strings.TrimSpace(metadata.Context + " " + metadata.Mode)},
		{"Cluster:", metadata.Cluster},
		{"Nodes:", metadata.NodeSummary},
		{"T9s Rev:", ""},
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
	columns := max(1, (len(hints)+5)/6)
	columnWidth := max(1, width/columns)
	for index, hint := range hints {
		column, row := index/6, index%6
		key := skin.actionKey.Render("<" + hint.Key + ">")
		description := skin.actionText.Render(hint.Label)
		cell := fitK9sCell(key+" "+description, columnWidth)
		lines[row] += cell
		_ = column
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
