package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const splashDuration = time.Second

type splashDoneMsg struct{}

type shellLayout struct {
	Width         int
	Height        int
	HeaderHeight  int
	ContentHeight int
	PromptHeight  int
}

func splashTimer() tea.Cmd {
	return tea.Tick(splashDuration, func(time.Time) tea.Msg { return splashDoneMsg{} })
}

func layoutShell(width, height int, promptActive bool) shellLayout {
	_ = promptActive
	width = max(0, width)
	height = max(0, height)
	remaining := height

	headerHeight := min(k9sHeaderHeight, remaining)
	if height < 16 {
		headerHeight = min(2, remaining)
	}
	remaining -= headerHeight

	promptHeight := 0

	fixedBottom := min(2, remaining) // breadcrumb and flash
	remaining -= fixedBottom

	return shellLayout{
		Width:         width,
		Height:        height,
		HeaderHeight:  headerHeight,
		ContentHeight: remaining,
		PromptHeight:  promptHeight,
	}
}

func renderShell(layout shellLayout, header, content, footer string) string {
	if layout.Height == 0 {
		return ""
	}

	lines := make([]string, 0, layout.Height)
	lines = appendRegion(lines, header, layout.HeaderHeight, layout.Width)
	lines = appendRegion(lines, content, layout.ContentHeight, layout.Width)

	bottomHeight := layout.Height - layout.HeaderHeight - layout.ContentHeight
	lines = appendRegion(lines, footer, bottomHeight, layout.Width)

	for len(lines) < layout.Height {
		lines = append(lines, fitShellLine("", layout.Width))
	}
	return strings.Join(lines[:layout.Height], "\n")
}

func appendRegion(lines []string, text string, height, width int) []string {
	region := strings.Split(text, "\n")
	if text == "" {
		region = nil
	}
	for index := 0; index < height; index++ {
		line := ""
		if index < len(region) {
			line = region[index]
		}
		lines = append(lines, fitShellLine(line, width))
	}
	return lines
}

func fitShellLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	return strings.TrimRight(ansi.Truncate(strings.ReplaceAll(line, "\t", "    "), width, "…"), " ")
}
