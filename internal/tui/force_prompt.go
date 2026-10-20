package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	"github.com/m11s-io/t9s/internal/application"
)

type forcePromptModel struct {
	kind       application.ActionKind
	target     string
	operation  uint64
	generation uint64
	err        string
	input      textinput.Model
}

func newForcePromptModel(kind application.ActionKind, target string, operation, generation uint64) forcePromptModel {
	input := textinput.New()
	input.Prompt = "TARGET :"
	input.Placeholder = target
	input.CharLimit = 256
	_ = input.Focus()
	return forcePromptModel{kind: kind, target: target, operation: operation, generation: generation, input: input}
}

func (m forcePromptModel) view() string {
	verb := "reboot"
	if m.kind == application.ActionShutdown {
		verb = "shutdown"
	}
	lines := []string{
		"FORCE " + strings.ToUpper(verb) + " " + sanitizeUntrustedText(m.target),
		"Force can skip graceful shutdown or drain and interrupt workloads.",
		"Type the exact node target and press Enter. Esc returns to the ordinary confirmation.",
	}
	if m.err != "" {
		lines = append(lines, "ERROR: "+sanitizeUntrustedText(m.err))
	}
	lines = append(lines, m.input.View())
	return strings.Join(lines, "\n")
}
