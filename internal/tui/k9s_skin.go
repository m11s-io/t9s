package tui

import "charm.land/lipgloss/v2"

type k9sSkin struct {
	body           lipgloss.Style
	metadataLabel  lipgloss.Style
	metadataValue  lipgloss.Style
	actionKey      lipgloss.Style
	actionText     lipgloss.Style
	logo           lipgloss.Style
	frame          lipgloss.Style
	focusedFrame   lipgloss.Style
	title          lipgloss.Style
	titleHighlight lipgloss.Style
	titleCounter   lipgloss.Style
	tableHeader    lipgloss.Style
	tableRow       lipgloss.Style
	selected       lipgloss.Style
	crumb          lipgloss.Style
	crumbActive    lipgloss.Style
	prompt         lipgloss.Style
	flashInfo      lipgloss.Style
	flashError     lipgloss.Style
}

func defaultK9sSkin() k9sSkin {
	border := lipgloss.NormalBorder()
	return k9sSkin{
		body:           lipgloss.NewStyle().Foreground(lipgloss.Color("#5F9EA0")),
		metadataLabel:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")),
		metadataValue:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true),
		actionKey:      lipgloss.NewStyle().Foreground(lipgloss.Color("#1E90FF")),
		actionText:     lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Faint(true),
		logo:           lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")),
		frame:          lipgloss.NewStyle().Border(border).BorderForeground(lipgloss.Color("#1E90FF")),
		focusedFrame:   lipgloss.NewStyle().Border(border).BorderForeground(lipgloss.Color("#87CEFA")),
		title:          lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true),
		titleHighlight: lipgloss.NewStyle().Foreground(lipgloss.Color("#FF00FF")).Bold(true),
		titleCounter:   lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true),
		tableHeader:    lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true),
		tableRow:       lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")),
		selected:       lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#00FFFF")),
		crumb:          lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#00FFFF")).Bold(true),
		crumbActive:    lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#FFA500")).Bold(true),
		prompt:         lipgloss.NewStyle().Foreground(lipgloss.Color("#5F9EA0")),
		flashInfo:      lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEFA")),
		flashError:     lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4500")),
	}
}
