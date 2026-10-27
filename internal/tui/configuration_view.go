package tui

import (
	"fmt"
	"strings"

	"github.com/m11s-io/t9s/internal/application"
)

func renderConfigurationView(model configurationModel, size contentSize) string {
	lines := make([]string, 0, max(0, size.Height))
	appendLine := func(line string) {
		if len(lines) < size.Height {
			lines = append(lines, line)
		}
	}
	appendLine("Talos configuration · same file bytes are sent to every selected node")
	appendLine("")
	switch model.phase {
	case configurationEditing:
		appendLine("File path: " + model.path.View())
		appendLine("")
		appendLine("TARGETS  ·  ↑/↓ move, Space select, Tab changes section")
		rows := max(0, size.Height-len(lines)-5)
		start, end := resourceWindow(len(model.nodes), model.targetCursor, rows)
		for index := start; index < end; index++ {
			node := model.nodes[index]
			mark := "[ ]"
			if model.selected[node.ID] {
				mark = "[x]"
			}
			cursor := " "
			if model.field == configurationTargetsField && index == model.targetCursor {
				cursor = ">"
			}
			appendLine(fmt.Sprintf("%s %s %s (%s)", cursor, mark, sanitizeUntrustedText(node.DisplayName()), nodeRoleDisplayValue(node)))
		}
		mode, modeSelected := model.mode()
		modeLine := "MODE  ·  ←/→ choose · Tab changes section"
		if modeSelected {
			modeLine += "  [" + string(mode) + "]"
		} else {
			modeLine += "  [choose explicitly]"
		}
		appendLine(modeLine)
		appendLine("Enter advances · Enter on MODE previews · Esc cancels")
	case configurationPreparing:
		appendLine(model.err)
		appendLine("No apply request is sent during preview.")
	case configurationReviewing:
		appendLine("FILE: " + sanitizeUntrustedText(model.basename))
		appendLine("SHA-256: " + model.assessment.DigestPrefix)
		appendLine("MODE: " + string(model.assessment.Mode))
		appendLine("TARGETS:")
		for _, preview := range model.assessment.Previews {
			line := "  " + sanitizeUntrustedText(preview.Target) + "  " + string(preview.Status)
			if preview.Status == application.ConfigurationPreviewSucceeded {
				line += " · resolved mode " + string(preview.Mode)
				if preview.WarningsKnown {
					line += fmt.Sprintf(" · %d warning(s)", preview.WarningCount)
				}
			} else if preview.Error != "" {
				line += " · " + sanitizeUntrustedText(preview.Error)
			}
			appendLine(line)
		}
		appendLine("Talos v1.14.5 does not include warning text in dry-run responses. Diff and warning text are hidden to protect secrets.")
		if model.assessment.Blocked != "" {
			appendLine("APPLY BLOCKED: " + sanitizeUntrustedText(model.assessment.Blocked))
		} else {
			appendLine("Type: " + model.assessment.RequiredPhrase + " · Enter to continue")
		}
	case configurationConfirming:
		appendLine("FILE: " + sanitizeUntrustedText(model.basename))
		appendLine("MODE: " + string(model.assessment.Mode))
		appendLine("TARGETS: " + strings.Join(sanitizeTargets(model.assessment.Targets), ", "))
		appendLine("Talos applies nodes independently; partial results are possible.")
		appendLine(model.confirmation.View())
		appendLine("Esc returns to preview")
	case configurationApplying, configurationComplete:
		appendLine("FILE: " + sanitizeUntrustedText(model.basename) + " · MODE: " + string(model.assessment.Mode))
		for _, result := range model.results {
			line := fmt.Sprintf("%s  %s", sanitizeUntrustedText(result.Target), result.Outcome)
			if result.Err != nil {
				line += " · " + sanitizeUntrustedText(result.Err.Error())
			}
			if result.Warning != "" {
				line += " · " + sanitizeUntrustedText(result.Warning)
			}
			appendLine(line)
		}
		if model.phase == configurationApplying {
			appendLine("Applying sequentially · Esc requests cancellation; remote results may be partial")
		}
	}
	if model.err != "" && model.phase != configurationPreparing {
		appendLine("Notice: " + sanitizeUntrustedText(model.err))
	}
	for len(lines) < size.Height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func sanitizeTargets(targets []string) []string {
	result := make([]string, len(targets))
	for index, target := range targets {
		result[index] = sanitizeUntrustedText(target)
	}
	return result
}
