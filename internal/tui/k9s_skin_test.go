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
	assert.True(t, skin.actionText.GetFaint(), "menu labels should use K9s' dim text style")
	assert.False(t, skin.actionKey.GetFaint(), "shortcut keys should remain emphasized")
	assert.Equal(t, lipgloss.Color("#00FFFF"), skin.title.GetForeground())
	assert.Equal(t, lipgloss.Color("#FF00FF"), skin.titleHighlight.GetForeground())
	assert.Equal(t, lipgloss.Color("#FFFFFF"), skin.titleCounter.GetForeground())
}

func TestDefaultK9sSkinLeavesNormalSurfacesToTerminalBackground(t *testing.T) {
	skin := defaultK9sSkin()
	for _, style := range []lipgloss.Style{
		skin.body, skin.metadataLabel, skin.metadataValue, skin.actionKey, skin.actionText,
		skin.logo, skin.frame, skin.focusedFrame, skin.title, skin.titleHighlight,
		skin.titleCounter, skin.tableHeader, skin.tableRow, skin.prompt, skin.flashInfo, skin.flashError,
	} {
		assert.Equal(t, lipgloss.NoColor{}, style.GetBackground())
	}
}
