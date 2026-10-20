package tui

import (
	"strings"
)

// The 9s lettering is copied from K9s internal/ui/splash.go (LogoSmall).
var t9sLogo = []string{
	` ________ ________       `,
	`|__    __/   __   \______`,
	`   |  |  \____    /  ___/`,
	`   |  |     /    /\___  \`,
	`   |__|    /____//____  /`,
	`                      \/ `,
}

func renderT9SLogo(skin k9sSkin) string {
	lines := make([]string, len(t9sLogo))
	for index, line := range t9sLogo {
		lines[index] = skin.logo.Render(fitK9sCell(line, 26))
	}
	return strings.Join(lines, "\n")
}
