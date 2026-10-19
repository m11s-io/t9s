package tui

import (
	"strings"
)

var t9sLogo = []string{
	` _______    ___     _____ `,
	`|__   __|  / _ \   / ____|`,
	`   | |    | (_) | | (___  `,
	`   | |     \__, |  \___ \ `,
	`   | |       / /    ___) |`,
	`   |_|      /_/    |_____/`,
}

func renderT9SLogo(skin k9sSkin) string {
	lines := make([]string, len(t9sLogo))
	for index, line := range t9sLogo {
		lines[index] = skin.logo.Render(fitK9sCell(line, 26))
	}
	return strings.Join(lines, "\n")
}
