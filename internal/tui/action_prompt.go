package tui

import (
	"fmt"
	"strings"

	"github.com/m11s-io/t9s/internal/application"
)

func renderPendingActionPrompt(pending application.PendingAction) string {
	verb := "Reboot"
	if pending.Kind == application.ActionShutdown {
		verb = "Shutdown"
	}
	prompt := verb + " " + strings.Join(pending.Targets, ", ") + "?"
	if pending.Warning != "" {
		prompt += " [" + pending.Warning + "]"
	}
	return prompt + " (y/n)"
}

func renderActionResults(results []application.ActionResult) string {
	succeeded := 0
	var failures []string
	for _, result := range results {
		if result.Err == "" {
			succeeded++
			continue
		}
		failures = append(failures, result.Target+": "+result.Err)
	}
	summary := fmt.Sprintf("%d/%d succeeded", succeeded, len(results))
	if len(failures) > 0 {
		summary += " (" + strings.Join(failures, "; ") + ")"
	}
	return summary
}
