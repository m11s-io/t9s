package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
)

// Closing a diagnostic cancels its request even when its RPC has not opened yet.
// Cluster snapshots are shared by overview/problems and remain session-scoped.
func (m model) releaseClosedViews(previous viewStack) (model, tea.Cmd) {
	var commands []tea.Cmd
	for _, frame := range previous {
		if slices.ContainsFunc(m.views, func(current viewFrame) bool { return current.Kind == frame.Kind }) {
			continue
		}
		var keys []string
		var closeMessage application.Message
		switch frame.Kind {
		case viewServiceLogs:
			closeMessage = application.CloseServiceLogs{}
		case viewDmesg:
			closeMessage = application.CloseDmesg{}
		case viewClusterHealth:
			closeMessage = application.CloseClusterHealth{}
		case viewEvents:
			if m.application.Events.Following {
				closeMessage = application.StopEventFollow{}
			}
		}
		if closeMessage != nil {
			var effect application.Effect
			m.application, effect = application.Update(m.application, closeMessage)
			m = m.syncScreens(closeMessage)
			commands = append(commands, m.command(effect))
		}
		switch frame.Kind {
		case viewNodes:
			keys = []string{"node-prompt"}
		case viewEtcd:
			keys = []string{"snapshot-prompt"}
		case viewProcesses:
			keys = []string{"processes"}
		case viewDisks:
			keys = []string{"disks"}
		case viewNetwork:
			keys = []string{"network"}
		case viewNetstat:
			keys = []string{"netstat"}
		case viewMounts:
			keys = []string{"mounts"}
		case viewMemory:
			keys = []string{"memory"}
		case viewResourceKinds:
			keys = []string{"kinds"}
		case viewResourceInstances:
			keys = []string{"instances"}
		case viewResourceDetail:
			keys = []string{"detail"}
		}
		for _, key := range keys {
			m.application, _ = application.Update(m.application, application.CloseRead{Key: key})
		}
	}
	return m, tea.Batch(commands...)
}
