package tui

import "strings"

type actionHint struct {
	Key   string
	Label string
}

func actionHints(kind viewKind) []actionHint {
	global := []actionHint{{Key: "?", Label: "Help"}, {Key: ":", Label: "Command"}}
	switch kind {
	case viewNodes, viewServices:
		hints := append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "d", Label: "Detail"},
			actionHint{Key: "r", Label: "Refresh"},
		)
		if kind == viewServices {
			hints = append(hints, actionHint{Key: "l", Label: "Logs"})
		} else {
			hints = append(hints,
				actionHint{Key: "p", Label: "Processes"},
				actionHint{Key: "k", Label: "Disks"},
				actionHint{Key: "n", Label: "Network"},
			)
		}
		return hints
	case viewEvents:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "r", Label: "Refresh"},
		)
	case viewEtcd:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "r", Label: "Refresh"},
		)
	case viewServiceLogs:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "s", Label: "Follow"},
			actionHint{Key: "w", Label: "Wrap"},
			actionHint{Key: "C", Label: "Clear"},
			actionHint{Key: "r", Label: "Reconnect"},
			actionHint{Key: "q/Esc", Label: "Back"},
		)
	case viewProcesses:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "d", Label: "Detail"},
			actionHint{Key: "r", Label: "Refresh"},
		)
	case viewDisks:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "d", Label: "Detail"},
			actionHint{Key: "r", Label: "Refresh"},
		)
	case viewNetwork:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "d", Label: "Detail"},
			actionHint{Key: "r", Label: "Refresh"},
		)
	case viewOverview:
		return append(global, actionHint{Key: "r", Label: "Refresh"})
	case viewProblems:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "d", Label: "Detail"},
			actionHint{Key: "r", Label: "Refresh"},
		)
	case viewResourceKinds, viewResourceInstances:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "d", Label: "Detail"},
			actionHint{Key: "r", Label: "Refresh"},
		)
	case viewResourceDetail:
		return append(global,
			actionHint{Key: "r", Label: "Refresh"},
			actionHint{Key: "q/Esc", Label: "Back"},
		)
	case viewNodeDetail, viewServiceDetail, viewProcessDetail, viewDiskDetail, viewLinkDetail:
		return append(global, actionHint{Key: "q/Esc", Label: "Back"})
	case viewHelp:
		return []actionHint{{Key: "q/Esc", Label: "Back"}}
	default:
		return global
	}
}

func renderActionHints(hints []actionHint) string {
	items := make([]string, 0, len(hints))
	for _, hint := range hints {
		items = append(items, "<"+hint.Key+"> "+hint.Label)
	}
	return strings.Join(items, "  ")
}
