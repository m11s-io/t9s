package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// upgradePromptModel is the text-input step of the Nodes-screen Upgrade (U)
// flow: prefilled with the target node's current install image, editable,
// and submitted with Enter into the same PendingAction confirm flow Rollback
// and Reboot/Shutdown already use.
type upgradePromptModel struct {
	target string
	input  textinput.Model
}

func newUpgradePromptModel(target, prefill string) upgradePromptModel {
	input := textinput.New()
	input.Prompt = "UPGRADE :"
	input.Placeholder = "installer image"
	input.CharLimit = 256
	input.SetValue(prefill)
	_ = input.Focus()
	return upgradePromptModel{target: target, input: input}
}

func (m upgradePromptModel) update(message tea.Msg) (upgradePromptModel, tea.Cmd) {
	var command tea.Cmd
	m.input, command = m.input.Update(message)
	return m, command
}

func (m upgradePromptModel) view() string {
	return m.input.View()
}
