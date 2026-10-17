package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type timePromptModel struct {
	input textinput.Model
	err   string
}

func newTimePromptModel() timePromptModel {
	input := textinput.New()
	input.Prompt = "TIME SERVER :"
	input.Placeholder = "hostname or bare IP"
	input.CharLimit = 253
	_ = input.Focus()
	return timePromptModel{input: input}
}
func (m timePromptModel) update(message tea.Msg) (timePromptModel, tea.Cmd) {
	var command tea.Cmd
	m.input, command = m.input.Update(message)
	return m, command
}
func (m timePromptModel) view() string {
	result := m.input.View()
	if m.err != "" {
		result += "  " + m.err
	}
	return result
}
