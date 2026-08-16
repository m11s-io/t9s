package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActionHintsIncludesWriteActionsWhenEnabled(t *testing.T) {
	hints := actionHints(viewNodes, true)

	keys := make([]string, len(hints))
	for i, hint := range hints {
		keys[i] = hint.Key
	}
	assert.Contains(t, keys, "R")
	assert.Contains(t, keys, "X")
	assert.Contains(t, keys, "space")
}

func TestActionHintsOmitsWriteActionsByDefault(t *testing.T) {
	hints := actionHints(viewNodes, false)

	for _, hint := range hints {
		assert.NotEqual(t, "R", hint.Key)
		assert.NotEqual(t, "X", hint.Key)
	}
}
