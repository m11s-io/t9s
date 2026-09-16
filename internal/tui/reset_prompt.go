package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/ports"
)

// Focus slots of the reset overlay. The confirm slot is last and hosts the
// text input; the earlier slots cycle the wipe mode and toggle the graceful
// and reboot booleans.
const (
	resetFocusMode = iota
	resetFocusGraceful
	resetFocusReboot
	resetFocusConfirm
	resetFocusCount
)

var resetModeLabels = []string{"ALL", "SYSTEM_DISK", "USER_DISKS"}

// resetPromptModel is the content-area overlay for the Nodes-screen reset (W)
// flow. It is an overlay rather than a footer prompt because the disk preview
// and risk text are multi-line. It carries the typed-confirmation input whose
// accepted value is re-validated by the reducer.
type resetPromptModel struct {
	targets []string
	options ports.ResetOptions
	preview map[string]application.ResetPreview
	warning string
	blocked string
	focus   int
	input   textinput.Model
	err     string
}

func newResetPromptModel(targets []string, preview map[string]application.ResetPreview, warning, blocked string) resetPromptModel {
	input := textinput.New()
	input.Prompt = "CONFIRM :"
	input.Placeholder = application.ResetConfirmationToken(targets)
	input.CharLimit = 64
	_ = input.Focus()
	return resetPromptModel{
		targets: targets,
		options: ports.ResetOptions{Mode: ports.WipeModeAll, Reboot: true},
		preview: preview,
		warning: warning,
		blocked: blocked,
		focus:   resetFocusConfirm,
		input:   input,
	}
}

func (m resetPromptModel) update(message tea.KeyPressMsg) (resetPromptModel, tea.Cmd) {
	switch message.String() {
	case "tab":
		m.focus = (m.focus + 1) % resetFocusCount
		m.syncFocus()
		return m, nil
	case "shift+tab":
		m.focus = (m.focus + resetFocusCount - 1) % resetFocusCount
		m.syncFocus()
		return m, nil
	}
	if m.focus == resetFocusConfirm {
		var command tea.Cmd
		m.input, command = m.input.Update(message)
		return m, command
	}
	switch m.focus {
	case resetFocusMode:
		switch message.String() {
		case " ", "space", "right", "l":
			m.options.Mode = (m.options.Mode + 1) % ports.WipeMode(3)
		case "left", "h":
			m.options.Mode = (m.options.Mode + ports.WipeMode(2)) % ports.WipeMode(3)
		}
	case resetFocusGraceful:
		if message.String() == " " || message.String() == "space" {
			m.options.Graceful = !m.options.Graceful
		}
	case resetFocusReboot:
		if message.String() == " " || message.String() == "space" {
			m.options.Reboot = !m.options.Reboot
		}
	}
	return m, nil
}

func (m *resetPromptModel) syncFocus() {
	if m.focus == resetFocusConfirm {
		_ = m.input.Focus()
		return
	}
	m.input.Blur()
}

func (m resetPromptModel) modeLabel() string {
	if int(m.options.Mode) < len(resetModeLabels) {
		return resetModeLabels[m.options.Mode]
	}
	return resetModeLabels[0]
}

func (m resetPromptModel) view(_ contentSize) string {
	var builder strings.Builder
	builder.WriteString("RESET " + strings.Join(m.targets, ", ") + "\n\n")
	builder.WriteString(m.renderPreview())
	builder.WriteString("\n")
	builder.WriteString(m.renderFields())
	if m.warning != "" {
		builder.WriteString("\nWARNING: " + m.warning)
	}
	if m.blocked != "" {
		builder.WriteString("\nBLOCKED: " + m.blocked)
	}
	if m.err != "" {
		builder.WriteString("\nERROR: " + m.err)
	}
	builder.WriteString("\n\n" + m.input.View())
	builder.WriteString("\ntype \"" + application.ResetConfirmationToken(m.targets) + "\" to confirm · tab to move · space toggles · esc cancels")
	return builder.String()
}

func (m resetPromptModel) renderPreview() string {
	// The overlay previews the first target's disk classification. Bulk
	// targets are usually identical (same provider image), and the reducer
	// re-checks inventory per target before allowing the wipe.
	for _, target := range m.targets {
		preview, ok := m.preview[target]
		if !ok {
			continue
		}
		if !preview.Known {
			line := "DISKS  " + target + ": inventory unknown"
			if preview.Err != "" {
				line += " (" + preview.Err + ")"
			}
			return line + "\n"
		}
		system := preview.SystemDisk
		if system == "" {
			system = "-"
		}
		user := "-"
		if len(preview.UserDisks) > 0 {
			user = strings.Join(preview.UserDisks, ", ")
		}
		return fmt.Sprintf("DISKS  %s: system %s · user %s\n", target, system, user)
	}
	return "DISKS  inventory unknown\n"
}

func (m resetPromptModel) renderFields() string {
	marker := func(index int) string {
		if m.focus == index {
			return ">"
		}
		return " "
	}
	onOff := func(value bool) string {
		if value {
			return "on"
		}
		return "off"
	}
	return fmt.Sprintf("%s MODE     %s\n%s GRACEFUL %s (leave etcd before reset)\n%s REBOOT   %s (on = reboot, off = halt)\n",
		marker(resetFocusMode), m.modeLabel(),
		marker(resetFocusGraceful), onOff(m.options.Graceful),
		marker(resetFocusReboot), onOff(m.options.Reboot),
	)
}
