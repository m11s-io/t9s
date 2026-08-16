package tui

import "strings"

func renderK9sFooter(width int, breadcrumb, prompt, flash string, skin k9sSkin) string {
	status := flash
	if prompt != "" {
		status = prompt
	}
	statusLine := fitK9sCell(skin.prompt.Render(status), width)
	parts := strings.Split(breadcrumb, " > ")
	active := ""
	if len(parts) > 0 {
		active = strings.TrimSpace(parts[len(parts)-1])
	}
	crumbLine := skin.crumbActive.Render(" <" + active + "> ")
	return statusLine + "\n" + fitK9sCell(crumbLine, width)
}
