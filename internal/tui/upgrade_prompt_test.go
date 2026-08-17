package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestNewUpgradePromptModelPrefillsInput(t *testing.T) {
	m := newUpgradePromptModel("cp-1", "ghcr.io/siderolabs/installer:v1.13.2")

	assert.Equal(t, "cp-1", m.target)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.2", m.input.Value())
}

func TestNewUpgradePromptModelAllowsBlankPrefill(t *testing.T) {
	m := newUpgradePromptModel("cp-1", "")

	assert.Empty(t, m.input.Value())
}

func TestUpgradePromptModelUpdateAppendsTypedText(t *testing.T) {
	m := newUpgradePromptModel("cp-1", "ghcr.io/siderolabs/installer:v1.13.2")

	m, _ = m.update(tea.KeyPressMsg{Code: 'x', Text: "x"})

	assert.Contains(t, m.input.Value(), "x")
}

func TestUpgradePromptModelViewReturnsInputView(t *testing.T) {
	m := newUpgradePromptModel("cp-1", "ghcr.io/siderolabs/installer:v1.13.2")

	assert.Contains(t, m.view(), "ghcr.io/siderolabs/installer:v1.13.2")
}
