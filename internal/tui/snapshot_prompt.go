package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// snapshotPromptModel is the text-input step of the :etcd Snapshot (s) flow:
// prefilled with a safe timestamped default, editable, and submitted with
// Enter into application.ConfirmEtcdSnapshotPrompt.
type snapshotPromptModel struct {
	node   string
	member string
	input  textinput.Model
}

func newSnapshotPromptModel(node, member, prefill string) snapshotPromptModel {
	input := textinput.New()
	input.Prompt = "SNAPSHOT :"
	input.Placeholder = "local path"
	input.CharLimit = 256
	input.SetValue(prefill)
	_ = input.Focus()
	return snapshotPromptModel{node: node, member: member, input: input}
}

func (m snapshotPromptModel) update(message tea.Msg) (snapshotPromptModel, tea.Cmd) {
	var command tea.Cmd
	m.input, command = m.input.Update(message)
	return m, command
}

func (m snapshotPromptModel) view() string {
	return m.input.View()
}
