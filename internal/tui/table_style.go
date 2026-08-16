package tui

func renderSelectedRow(text string, width int, selected bool, skin k9sSkin) string {
	text = fitK9sCell(text, width)
	if selected {
		return skin.selected.Render(text)
	}
	return skin.tableRow.Render(text)
}
