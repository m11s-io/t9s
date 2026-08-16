package tui

import "charm.land/lipgloss/v2"

type styles struct {
	title  lipgloss.Style
	status lipgloss.Style
	header lipgloss.Style
	error  lipgloss.Style
	k9s    k9sSkin
}

func defaultStyles() styles {
	return styles{
		title:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")),
		status: lipgloss.NewStyle().Faint(true),
		header: lipgloss.NewStyle().Bold(true),
		error:  lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		k9s:    defaultK9sSkin(),
	}
}
