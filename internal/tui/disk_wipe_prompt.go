package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
)

// diskWipePromptModel is the text-input step of the :disks Wipe (W) flow: the
// operator types the selected device path to confirm a single-device
// BlockDeviceWipe, then Enter opens the gated (y/n) confirm. It is a single
// line footer prompt because there is no multi-line preview.
type diskWipePromptModel struct {
	node   string
	device string
	input  textinput.Model
	err    string
}

func newDiskWipePromptModel(node, device string) diskWipePromptModel {
	input := textinput.New()
	input.Prompt = "WIPE DEVICE :"
	input.Placeholder = application.NormalizeDeviceToken(device)
	input.CharLimit = 128
	_ = input.Focus()
	return diskWipePromptModel{node: node, device: device, input: input}
}

func (m diskWipePromptModel) update(message tea.Msg) (diskWipePromptModel, tea.Cmd) {
	var command tea.Cmd
	m.input, command = m.input.Update(message)
	return m, command
}

func (m diskWipePromptModel) view() string {
	if m.err != "" {
		return "!! " + m.err + " — " + m.input.View()
	}
	return m.input.View()
}
