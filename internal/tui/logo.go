package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

var t9sLogo = []string{
	` _______ ___   _____ `,
	`|__   __|/ _ \ / ____|`,
	`   | |  | (_) || (___ `,
	`   | |   \__, | \___ \`,
	`   | |     / /  ____) |`,
	`   |_|    /_/  |_____/ `,
	`           T9S             `,
}

func renderT9SLogo(skin k9sSkin) string {
	lines := make([]string, len(t9sLogo))
	for index, line := range t9sLogo {
		lines[index] = skin.logo.Render(fitK9sCell(line, 26))
	}
	return strings.Join(lines, "\n")
}

func padK9sLine(value string, width int) string {
	return lipgloss.NewStyle().Width(max(0, width)).MaxWidth(max(0, width)).Render(value)
}
