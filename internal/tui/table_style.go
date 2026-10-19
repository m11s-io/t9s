package tui

func renderTableHeader(text string, width int, skin k9sSkin) string {
	return skin.tableHeader.Render(fitK9sCell(text, width))
}

func renderSelectedRow(text string, width int, selected bool, skin k9sSkin) string {
	text = fitK9sCell(text, width)
	if selected {
		return skin.selected.Render(text)
	}
	return skin.tableRow.Render(text)
}
