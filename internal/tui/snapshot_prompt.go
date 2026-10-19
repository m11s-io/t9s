package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
)

// snapshotPromptModel is the text-input step of the :etcd Snapshot (s) flow:
// prefilled with a safe timestamped default, editable, and submitted with
// Enter into application.ConfirmEtcdSnapshotPrompt.
type snapshotPromptModel struct {
	node     string
	member   string
	memberID uint64
	kind     application.EtcdActionKind
	input    textinput.Model
	err      string
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
	if m.err == "" {
		return m.input.View()
	}
	return m.input.View() + "\n" + m.err
}
