package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type command int

const (
	commandUnknown command = iota
	commandNodes
	commandServices
	commandContexts
	commandEvents
	commandEtcd
	commandBGPPeers
	commandOverview
	commandProblems
	commandResources
	commandClusterHealth
	commandHelp
)

type commandModel struct {
	input  textinput.Model
	active bool
}

func newCommandModel() commandModel {
	input := textinput.New()
	input.Prompt = ">"
	input.Placeholder = "nodes, services, contexts, bgp, or healthcheck"
	input.CharLimit = 64
	return commandModel{input: input}
}

func resolveCommand(value string) command {
	switch value {
	case "nodes", "no":
		return commandNodes
	case "services", "svc":
		return commandServices
	case "contexts", "ctx":
		return commandContexts
	case "events", "ev":
		return commandEvents
	case "etcd", "et":
		return commandEtcd
	case "bgp", "bgps":
		return commandBGPPeers
	case "overview", "ov":
		return commandOverview
	case "problems":
		return commandProblems
	case "resources", "res":
		return commandResources
	case "healthcheck", "hc":
		return commandClusterHealth
	case "help", "?":
		return commandHelp
	default:
		if _, ok := resourcesCommandArgument(value); ok {
			return commandResources
		}
		if _, ok := healthcheckCommandArgument(value); ok {
			return commandClusterHealth
		}
		return commandUnknown
	}
}

// healthcheckCommandArgument parses the optional trailing Go duration from a
// ":healthcheck <duration>" command. An unparseable or non-positive duration
// is rejected so the command stays a closed enum (it resolves to
// commandUnknown).
func healthcheckCommandArgument(value string) (time.Duration, bool) {
	for _, prefix := range []string{"healthcheck ", "hc "} {
		if strings.HasPrefix(value, prefix) {
			argument := strings.TrimSpace(strings.TrimPrefix(value, prefix))
			timeout, err := time.ParseDuration(argument)
			if err != nil || timeout <= 0 {
				return 0, false
			}
			return timeout, true
		}
	}
	return 0, false
}

func resourcesCommandArgument(value string) (string, bool) {
	for _, prefix := range []string{"resources ", "res "} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(value, prefix)), true
		}
	}
	return "", false
}

func (m commandModel) open() (commandModel, tea.Cmd) {
	m.active = true
	m.input.Reset()
	return m, m.input.Focus()
}

func (m commandModel) close() commandModel {
	m.active = false
	m.input.Blur()
	m.input.Reset()
	return m
}

func (m commandModel) update(message tea.Msg) (commandModel, tea.Cmd) {
	var command tea.Cmd
	m.input, command = m.input.Update(message)
	return m, command
}

func (m commandModel) view() string {
	if !m.active {
		return ""
	}
	return m.input.View()
}

func unknownCommandNotice(value string) string {
	return fmt.Sprintf("Unknown command %q", value)
}

func (m commandModel) viewBar(width int, skin k9sSkin) string {
	if !m.active || width <= 0 {
		return ""
	}
	innerWidth := max(0, width-2)
	body := fitK9sCell(m.view(), innerWidth)
	return skin.frame.BorderForeground(lipgloss.Color("#00FFFF")).Render(body)
}

func (m *commandModel) resize(width int) {
	inputWidth := max(1, width-2-ansi.StringWidth(m.input.Prompt))
	if inputWidth != m.input.Width() {
		m.input.SetWidth(inputWidth)
		// SetWidth alone does not recompute Bubbles' horizontal scroll window.
		m.input.SetCursor(m.input.Position())
	}
}
