package tui

import (
	"fmt"
	"strings"

	"github.com/m11s-io/t9s/internal/application"
)

// pendingActionWarningBudget bounds how many runes of a PendingAction.Warning
// renderPendingActionPrompt will embed. It is sized so that even the longest
// realistic warning (the etcd quorum-loss message, ~70 runes) plus the verb,
// target count, and "(y/n)" cue stay comfortably under an 80-column
// terminal — the narrowest width t9s is expected to run in — after the
// footer's own fitK9sCell (header.go) hard-truncates the line with no
// ellipsis. This is a fixed budget, not general text-wrapping: it exists
// solely so the confirm cue and the risk detail both survive truncation.
const pendingActionWarningBudget = 40

// renderPendingActionPrompt renders the confirm-prompt footer line. The
// footer is hard-truncated from the end at the terminal width, so the parts
// an operator most needs to see — the quorum/control-plane warning and the
// "(y/n)" confirm cue — are placed first, and the target list (arbitrarily
// long for a bulk action) is the most elidable part: it is dropped in favor
// of a count when a warning is present, or placed last when there is no
// warning and the line is short enough not to need truncation.
// renderEtcdSnapshotNotice turns the standalone snapshot state into a footer
// notice, so a rejected path or a completed backup reports its path/size
// instead of only the generic action-results counter.
func renderEtcdSnapshotNotice(state application.EtcdSnapshotState) string {
	switch state.Status {
	case application.Failed:
		if state.Err != "" {
			return "snapshot failed: " + state.Err
		}
	case application.Ready:
		return fmt.Sprintf("snapshot %s (%d bytes)", state.Result.Path, state.Result.Size)
	}

	return ""
}

func renderPendingActionPrompt(pending application.PendingAction) string {
	if pending.Blocked != "" {
		return fmt.Sprintf("!! %s — action refused (n to cancel)", truncateWarningTail(pending.Blocked, pendingActionWarningBudget))
	}
	verb := "Reboot"
	switch pending.Kind {
	case application.ActionShutdown:
		verb = "Shutdown"
	case application.ActionRollback:
		verb = "Rollback"
	case application.ActionUpgrade:
		verb = "Upgrade to " + truncateWarningTail(pending.Image, 24)
	}
	if pending.Warning != "" {
		warning := truncateWarningTail(pending.Warning, pendingActionWarningBudget)
		target := fmt.Sprintf("%d node(s)", len(pending.Targets))
		if pending.Kind == application.ActionUpgrade && len(pending.Targets) == 1 {
			target = pending.Targets[0]
		}
		return fmt.Sprintf("!! %s — %s %s? (y/n)", warning, verb, target)
	}
	return verb + " " + strings.Join(pending.Targets, ", ") + "? (y/n)"
}

func renderPendingEtcdActionPrompt(pending application.PendingEtcdAction) string {
	if pending.Blocked != "" {
		return fmt.Sprintf("!! %s — action refused (n to cancel)", truncateWarningTail(pending.Blocked, pendingActionWarningBudget))
	}
	verb := "Defragment"
	switch pending.Kind {
	case application.EtcdActionDisarmAlarms:
		verb = "Disarm alarms"
	case application.EtcdActionSnapshot:
		verb = "Snapshot"
	case application.EtcdActionRemoveMember:
		verb = "Remove member"
	case application.EtcdActionLeaveCluster:
		verb = "Leave cluster"
	}
	target := pending.MemberHostname
	if target == "" {
		target = pending.Node
	}
	// Destructive membership actions make the mandatory pre-removal snapshot
	// explicit so the operator knows a backup is taken first.
	suffix := ""
	if pending.SnapshotPath != "" {
		suffix = " after snapshot to " + pending.SnapshotPath
	}
	if pending.Warning != "" {
		return fmt.Sprintf("!! %s — %s %s%s? (y/n)", truncateWarningTail(pending.Warning, pendingActionWarningBudget), verb, target, suffix)
	}
	return verb + " " + target + suffix + "? (y/n)"
}

func renderPendingServiceActionPrompt(pending application.PendingServiceAction) string {
	if pending.Blocked != "" {
		return fmt.Sprintf("!! %s — action refused (n to cancel)", truncateWarningTail(pending.Blocked, pendingActionWarningBudget))
	}
	verb := "Start"
	switch pending.Kind {
	case application.ServiceActionStop:
		verb = "Stop"
	case application.ServiceActionRestart:
		verb = "Restart"
	}
	target := pending.Service + "@" + pending.Node
	if pending.Warning != "" {
		warning := truncateWarningTail(pending.Warning, pendingActionWarningBudget)
		return fmt.Sprintf("!! %s — %s %s? (y/n)", warning, verb, target)
	}
	return verb + " " + target + "? (y/n)"
}

// truncateWarningTail keeps the tail of a long warning — where the concrete
// risk detail (e.g. "below quorum (need N)") lives — rather than the head,
// prefixing an ellipsis when it cuts. The alternative, keeping the head and
// letting the footer's own end-truncation cut the tail, would silently drop
// exactly the detail that matters most.
func truncateWarningTail(warning string, budget int) string {
	runes := []rune(warning)
	if len(runes) <= budget || budget <= 1 {
		return warning
	}
	return "…" + string(runes[len(runes)-(budget-1):])
}

func renderActionResults(results []application.ActionResult, total int) string {
	succeeded := 0
	var failures []string
	for _, result := range results {
		if result.Err == "" {
			succeeded++
			continue
		}
		failures = append(failures, result.Target+": "+result.Err)
	}
	if total <= 0 {
		total = len(results)
	}
	summary := fmt.Sprintf("%d/%d succeeded", succeeded, total)
	if len(failures) > 0 {
		summary += " (" + strings.Join(failures, "; ") + ")"
	}
	return summary
}
