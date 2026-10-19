package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/viewport"
)

func sanitizeLogLines(lines []string) []string {
	result := make([]string, len(lines))
	for index, line := range lines {
		result[index] = sanitizeUntrustedText(line)
	}
	return result
}

type logsModel struct {
	title          string
	state          logDisplayState
	filter         string
	filtering      bool
	following      bool
	wrap           bool
	clearRequested bool
	viewport       viewport.Model
}

type logDisplayState struct {
	Status directLoadStatus
	Lines  []string
	Err    string
	EOF    bool
}

func newDirectServiceLogsModel() logsModel {
	return logsModel{title: "SERVICE LOGS", following: true, viewport: viewport.New(), state: logDisplayState{Status: directLoading}}
}

func newDirectDmesgModel() logsModel {
	return logsModel{title: "DMESG", following: true, viewport: viewport.New(), state: logDisplayState{Status: directLoading}}
}

func (m logsModel) setStreamState(status directLoadStatus, lines []string, err string, eof bool) logsModel {
	m.state = logDisplayState{Status: status, Lines: sanitizeLogLines(lines), Err: sanitizeUntrustedText(err), EOF: eof}
	return m
}

// syncViewport applies content and size changes to the stored viewport before
// the next input event. View remains a pure renderer over a value copy.
func (m logsModel) syncViewport(size contentSize) logsModel {
	if size.Height <= 0 {
		return m
	}
	m.viewport.SetHeight(max(0, size.Height-1))
	m.viewport.SetContentLines(m.renderedLines(max(0, size.Width)))
	if m.following {
		m.viewport.GotoBottom()
	}
	return m
}

func (m logsModel) update(message tea.KeyPressMsg) logsModel {
	key := message.String()
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
	if key == "C" {
		m.clearRequested = true
	}
	return m
}

func (m logsModel) visibleLines() []string {
	// m.state.Lines is sanitized once in setState; no per-call sanitization needed here.
	lines := m.state.Lines
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return lines
	}
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), query) {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func (m logsModel) viewSized(size contentSize) string {
	if size.Height <= 0 {
		return ""
	}
	header := m.title
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
		// m.state.Err is sanitized in setState alongside m.state.Lines.
		lines = append(lines, m.state.Err)
	} else if m.state.Status == directLoading {
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
		wrapped := ansi.Hardwrap(line, width, true)
		result = append(result, strings.Split(wrapped, "\n")...)
	}
	return result
}
