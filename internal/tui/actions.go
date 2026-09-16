package tui

import "strings"

type actionHint struct {
	Key   string
	Label string
}

func actionHints(kind viewKind, writesEnabled bool) []actionHint {
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
			if writesEnabled {
				hints = append(hints,
					actionHint{Key: "S", Label: "Start"},
					actionHint{Key: "T", Label: "Stop"},
					actionHint{Key: "R", Label: "Restart"},
				)
			}
		} else {
			hints = append(hints,
				actionHint{Key: "p", Label: "Processes"},
				actionHint{Key: "k", Label: "Disks"},
				actionHint{Key: "n", Label: "Network"},
				actionHint{Key: "e", Label: "Dmesg"},
				actionHint{Key: "s", Label: "Netstat"},
				actionHint{Key: "m", Label: "Mounts"},
				actionHint{Key: "f", Label: "Memory"},
			)
			if writesEnabled {
				hints = append(hints,
					actionHint{Key: "space", Label: "Mark"},
					actionHint{Key: "R", Label: "Reboot"},
					actionHint{Key: "X", Label: "Shutdown"},
					actionHint{Key: "B", Label: "Rollback"},
					actionHint{Key: "W", Label: "Reset"},
					actionHint{Key: "U", Label: "Upgrade"},
				)
			}
		}
		return hints
	case viewEvents:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "r", Label: "Refresh"},
		)
	case viewEtcd:
		hints := append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "r", Label: "Refresh"},
		)
		if writesEnabled {
			hints = append(hints,
				actionHint{Key: "s", Label: "Snapshot"},
				actionHint{Key: "R", Label: "Remove"},
				actionHint{Key: "L", Label: "Leave"},
				actionHint{Key: "d", Label: "Defragment"},
				actionHint{Key: "A", Label: "Disarm"},
				actionHint{Key: "F", Label: "Forfeit"},
			)
		}
		return hints
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
		hints := append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "d", Label: "Detail"},
			actionHint{Key: "r", Label: "Refresh"},
		)
		if writesEnabled {
			hints = append(hints, actionHint{Key: "W", Label: "Wipe"})
		}
		return hints
	case viewNetwork:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "d", Label: "Detail"},
			actionHint{Key: "r", Label: "Refresh"},
		)
	case viewDmesg:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "s", Label: "Follow"},
			actionHint{Key: "w", Label: "Wrap"},
			actionHint{Key: "C", Label: "Clear"},
			actionHint{Key: "r", Label: "Reconnect"},
			actionHint{Key: "q/Esc", Label: "Back"},
		)
	case viewClusterHealth:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "C", Label: "Clear"},
			actionHint{Key: "r", Label: "Reconnect"},
			actionHint{Key: "q/Esc", Label: "Back"},
		)
	case viewNetstat:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "r", Label: "Refresh"},
		)
	case viewMounts:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
			actionHint{Key: "r", Label: "Refresh"},
		)
	case viewMemory:
		return append(global,
			actionHint{Key: "/", Label: "Filter"},
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
