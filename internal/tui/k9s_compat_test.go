package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestK9sCompatibilityActionMatrix(t *testing.T) {
	tests := []struct {
		name string
		view viewKind
		keys []string
	}{
		{name: "root resources", view: viewNodes, keys: []string{"?", ":", "/", "d", "r", "p", "k", "n", "e", "s", "m", "f"}},
		{name: "services add Talos logs", view: viewServices, keys: []string{"?", ":", "/", "d", "r", "l"}},
		{name: "detail uses read-only back navigation", view: viewServiceDetail, keys: []string{"?", ":", "q/Esc"}},
		{name: "help is a child view", view: viewHelp, keys: []string{"q/Esc"}},
		{name: "logs use familiar follow controls", view: viewServiceLogs, keys: []string{"?", ":", "/", "s", "w", "C", "r", "q/Esc"}},
		{name: "dmesg streams like logs", view: viewDmesg, keys: []string{"?", ":", "/", "s", "w", "C", "r", "q/Esc"}},
		{name: "healthcheck streams like dmesg", view: viewClusterHealth, keys: []string{"?", ":", "/", "C", "r", "q/Esc"}},
		{name: "netstat is a read-only list", view: viewNetstat, keys: []string{"?", ":", "/", "r"}},
		{name: "mounts is a read-only list", view: viewMounts, keys: []string{"?", ":", "/", "r"}},
		{name: "memory is a read-only list", view: viewMemory, keys: []string{"?", ":", "/", "r"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := make([]string, 0, len(test.keys))
			for _, hint := range actionHints(test.view, false) {
				actual = append(actual, hint.Key)
			}
			assert.Equal(t, test.keys, actual)
		})
	}
}

func TestK9sCompatibilityDocumentsReadOnlyTalosDeviations(t *testing.T) {
	serviceHints := renderActionHints(actionHints(viewServices, false))
	assert.Contains(t, serviceHints, "<d> Detail", "Talos services use Enter/d for the same read-only detail")
	for _, destructive := range []string{"Delete", "Kill", "Drain", "Edit"} {
		assert.NotContains(t, serviceHints, destructive, "t9s deliberately omits Kubernetes-pod-style destructive actions; Talos-level service start/stop/restart (S/T/R, WritesEnabled-gated) is a deliberate, separate addition")
	}
}
