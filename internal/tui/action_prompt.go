package tui

// pendingActionWarningBudget bounds risk text so the confirmation cue stays
// visible in the active mutation prompt.
const pendingActionWarningBudget = 40

// truncateWarningTail preserves the concrete risk detail at the end of a warning.
func truncateWarningTail(warning string, budget int) string {
	runes := []rune(warning)
	if len(runes) <= budget || budget <= 1 {
		return warning
	}
	return "…" + string(runes[len(runes)-(budget-1):])
}
