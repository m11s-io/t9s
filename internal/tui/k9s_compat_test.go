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
		{name: "root resources", view: viewNodes, keys: []string{"?", ":", "/", "d", "r", "p", "k", "n"}},
		{name: "services add Talos logs", view: viewServices, keys: []string{"?", ":", "/", "d", "r", "l"}},
		{name: "detail uses read-only back navigation", view: viewServiceDetail, keys: []string{"?", ":", "q/Esc"}},
		{name: "help is a child view", view: viewHelp, keys: []string{"q/Esc"}},
		{name: "logs use familiar follow controls", view: viewServiceLogs, keys: []string{"?", ":", "/", "s", "w", "C", "r", "q/Esc"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := make([]string, 0, len(test.keys))
			for _, hint := range actionHints(test.view) {
				actual = append(actual, hint.Key)
			}
			assert.Equal(t, test.keys, actual)
		})
	}
}

func TestK9sCompatibilityDocumentsReadOnlyTalosDeviations(t *testing.T) {
	serviceHints := renderActionHints(actionHints(viewServices))
	assert.Contains(t, serviceHints, "<d> Detail", "Talos services use Enter/d for the same read-only detail")
	for _, destructive := range []string{"Delete", "Kill", "Drain", "Edit"} {
		assert.NotContains(t, serviceHints, destructive, "t9s deliberately omits unsupported destructive Kubernetes actions")
	}
}
