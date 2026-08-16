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
		body:           lipgloss.NewStyle().Foreground(lipgloss.Color("#5F9EA0")).Background(lipgloss.Color("#000000")),
		metadataLabel:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Background(lipgloss.Color("#000000")),
		metadataValue:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#000000")).Bold(true),
		actionKey:      lipgloss.NewStyle().Foreground(lipgloss.Color("#1E90FF")).Background(lipgloss.Color("#000000")),
		actionText:     lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#000000")),
		logo:           lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Background(lipgloss.Color("#000000")),
		frame:          lipgloss.NewStyle().Border(border).BorderForeground(lipgloss.Color("#1E90FF")).Background(lipgloss.Color("#000000")),
		focusedFrame:   lipgloss.NewStyle().Border(border).BorderForeground(lipgloss.Color("#87CEFA")).Background(lipgloss.Color("#000000")),
		title:          lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Background(lipgloss.Color("#000000")).Bold(true),
		titleHighlight: lipgloss.NewStyle().Foreground(lipgloss.Color("#FF00FF")).Background(lipgloss.Color("#000000")).Bold(true),
		titleCounter:   lipgloss.NewStyle().Foreground(lipgloss.Color("#FFEFD5")).Background(lipgloss.Color("#000000")).Bold(true),
		tableHeader:    lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#000000")).Bold(true),
		tableRow:       lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Background(lipgloss.Color("#000000")),
		selected:       lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#00FFFF")),
		crumb:          lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#00FFFF")).Bold(true),
		crumbActive:    lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#FFA500")).Bold(true),
		prompt:         lipgloss.NewStyle().Foreground(lipgloss.Color("#5F9EA0")).Background(lipgloss.Color("#000000")),
		flashInfo:      lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEFA")).Background(lipgloss.Color("#000000")),
		flashError:     lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4500")).Background(lipgloss.Color("#000000")),
	}
}
