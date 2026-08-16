package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/viewport"

	"github.com/m11s-io/t9s/internal/application"
)

type logsModel struct {
	state          application.LogState
	filter         string
	filtering      bool
	following      bool
	wrap           bool
	clearRequested bool
	viewport       viewport.Model
}

func newLogsModel(state application.LogState) logsModel {
	return logsModel{state: state, following: true, viewport: viewport.New()}
}

func (m logsModel) setState(state application.LogState) logsModel {
	m.state = state
	return m
}

func (m logsModel) update(message tea.KeyPressMsg) logsModel {
	key := message.Keystroke()
	if m.filtering {
		switch key {
		case "esc":
			m.filter, m.filtering = "", false
		case "enter":
			m.filtering = false
		case "backspace":
			m.filter = trimLastRune(m.filter)
		default:
			if text := printableText(message.Text); text != "" {
				m.filter += text
			}
		}
		return m
	}
	switch key {
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
	case "s":
		m.following = !m.following
	case "w":
		m.wrap = !m.wrap
	case "up", "k":
		m.following = false
		m.viewport.ScrollUp(1)
	case "down", "j":
		m.viewport.ScrollDown(1)
	case "g":
		m.following = false
		m.viewport.GotoTop()
	case "G":
		m.following = true
		m.viewport.GotoBottom()
	}
	if message.Text == "C" || key == "shift+c" {
		m.clearRequested = true
	}
	return m
}

func (m logsModel) visibleLines() []string {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.state.Lines
	}
	lines := make([]string, 0, len(m.state.Lines))
	for _, line := range m.state.Lines {
		if strings.Contains(strings.ToLower(line), query) {
			lines = append(lines, line)
		}
	}
	return lines
}

func (m logsModel) viewSized(size contentSize) string {
	if size.Height <= 0 {
		return ""
	}
	header := "SERVICE LOGS"
	if !m.following {
		header += "  PAUSED"
	}
	if m.wrap {
		header += "  WRAP"
	}
	bodyHeight := max(0, size.Height-1)
	body := m.renderedLines(max(0, size.Width))
	m.viewport.SetHeight(bodyHeight)
	m.viewport.SetContentLines(body)
	if m.following {
		m.viewport.GotoBottom()
	}
	start := min(m.viewport.YOffset(), len(body))
	end := min(start+bodyHeight, len(body))
	lines := make([]string, 0, bodyHeight+2)
	lines = append(lines, header)
	lines = append(lines, body[start:end]...)
	if m.state.Err != "" {
		lines = append(lines, m.state.Err)
	} else if m.state.Status == application.Loading {
		lines = append(lines, "Opening log stream…")
	} else if m.state.EOF {
		lines = append(lines, "End of stream")
	}
	if len(lines) > size.Height {
		lines = append(lines[:1], lines[len(lines)-(size.Height-1):]...)
	}
	return strings.Join(lines, "\n")
}

func (m logsModel) renderedLines(width int) []string {
	lines := m.visibleLines()
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if width <= 0 || ansi.StringWidth(line) <= width {
			result = append(result, line)
			continue
		}
		if !m.wrap {
			result = append(result, ansi.Truncate(line, width, "…"))
			continue
		}
		for line != "" {
			part := ansi.Truncate(line, width, "")
			if part == "" {
				break
			}
			result = append(result, part)
			line = strings.TrimPrefix(line, part)
		}
	}
	return result
}
