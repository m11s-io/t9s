package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/m11s-io/t9s/internal/application"
)

// defaultClusterHealthWait is the wait timeout used when ":healthcheck" is run
// without an explicit duration.
const defaultClusterHealthWait = 60 * time.Second

type healthcheckModel struct {
	title          string
	state          application.ClusterHealthState
	filter         string
	filtering      bool
	clearRequested bool
	viewport       viewport.Model
}

func newHealthcheckModel(state application.ClusterHealthState) healthcheckModel {
	m := healthcheckModel{title: "CLUSTER HEALTH", viewport: viewport.New()}
	return m.setState(state)
}

// setState is the single point where health-check content enters the model. It
// sanitizes Lines and Err so m.state is always sanitized thereafter.
func (m healthcheckModel) setState(state application.ClusterHealthState) healthcheckModel {
	state.Lines = sanitizeLogLines(state.Lines)
	state.Err = sanitizeUntrustedText(state.Err)
	m.state = state
	return m
}

func (m healthcheckModel) update(message tea.KeyPressMsg) healthcheckModel {
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
	case "up", "k":
		m.viewport.ScrollUp(1)
	case "down", "j":
		m.viewport.ScrollDown(1)
	case "g":
		m.viewport.GotoTop()
	case "G":
		m.viewport.GotoBottom()
	}
	if key == "C" {
		m.clearRequested = true
	}
	return m
}

func (m healthcheckModel) visibleLines() []string {
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

func (m healthcheckModel) headerLine() string {
	request := m.state.Request
	return fmt.Sprintf("wait %s · cp %s · worker %s · / filter · C clear · r reconnect · q/Esc back",
		request.WaitTimeout, countLabel(len(request.ControlPlaneNodes)), countLabel(len(request.WorkerNodes)))
}

// countLabel renders a node count, or "?" when the count is zero/unknown.
func countLabel(count int) string {
	if count == 0 {
		return "?"
	}
	return fmt.Sprintf("%d", count)
}

func (m healthcheckModel) verdictLine() string {
	switch {
	case m.state.Err != "":
		return "✘ " + m.state.Err
	case m.state.VerdictReady:
		return "✔ cluster is healthy"
	case m.state.Status == application.Loading:
		return "… starting"
	default:
		return "… checking"
	}
}

func (m healthcheckModel) viewSized(size contentSize) string {
	if size.Height <= 0 {
		return ""
	}
	header := m.headerLine()
	verdict := m.verdictLine()
	bodyHeight := max(0, size.Height-2)
	body := m.renderedLines(max(0, size.Width))
	m.viewport.SetHeight(bodyHeight)
	m.viewport.SetContentLines(body)
	start := min(m.viewport.YOffset(), len(body))
	end := min(start+bodyHeight, len(body))
	lines := make([]string, 0, bodyHeight+2)
	lines = append(lines, header)
	lines = append(lines, body[start:end]...)
	lines = append(lines, verdict)
	if len(lines) > size.Height {
		lines = append(lines[:1], lines[len(lines)-(size.Height-1):]...)
	}
	return strings.Join(lines, "\n")
}

func (m healthcheckModel) renderedLines(width int) []string {
	lines := m.visibleLines()
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if width <= 0 || ansi.StringWidth(line) <= width {
			result = append(result, line)
			continue
		}
		result = append(result, ansi.Truncate(line, width, "…"))
	}
	return result
}
