package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type command int

const (
	commandUnknown command = iota
	commandNodes
	commandServices
	commandContexts
	commandEvents
	commandEtcd
	commandOverview
	commandProblems
	commandResources
)

type commandModel struct {
	input  textinput.Model
	active bool
}

func newCommandModel() commandModel {
	input := textinput.New()
	input.Prompt = "COMMAND :"
	input.Placeholder = "nodes, services, or contexts"
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
	case "overview", "ov":
		return commandOverview
	case "problems":
		return commandProblems
	case "resources", "res":
		return commandResources
	default:
		if _, ok := resourcesCommandArgument(value); ok {
			return commandResources
		}
		return commandUnknown
	}
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
