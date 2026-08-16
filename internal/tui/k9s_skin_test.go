package tui

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
)

func TestDefaultK9sSkinUsesStockFrameAndSelectionRoles(t *testing.T) {
	skin := defaultK9sSkin()

	assert.Equal(t, lipgloss.Color("#1E90FF"), skin.frame.GetBorderTopForeground())
	assert.Equal(t, lipgloss.Color("#87CEFA"), skin.focusedFrame.GetBorderTopForeground())
	assert.Equal(t, lipgloss.Color("#00FFFF"), skin.selected.GetBackground())
	assert.Equal(t, lipgloss.Color("#000000"), skin.selected.GetForeground())
	assert.Equal(t, lipgloss.Color("#00FFFF"), skin.crumb.GetBackground())
	assert.Equal(t, lipgloss.Color("#000000"), skin.crumb.GetForeground())
	assert.Equal(t, lipgloss.Color("#FFA500"), skin.metadataLabel.GetForeground())
	assert.Equal(t, lipgloss.Color("#1E90FF"), skin.actionKey.GetForeground())
}
