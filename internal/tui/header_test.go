package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestK9sHeaderUsesSevenRowsAndRightAlignedLogo(t *testing.T) {
	header := renderK9sHeader(layoutK9sHeader(160), shellMetadata{
		Context: "mgmt", Cluster: "management", NodeSummary: "6/6", TalosVersion: "v1.13.2", Health: "Healthy", Mode: "[RO]",
	}, actionHints(viewServices, false), defaultK9sSkin())

	lines := strings.Split(header, "\n")
	assert.Len(t, lines, 7)
	for _, line := range lines {
		assert.Equal(t, 160, ansi.StringWidth(line))
	}
	assert.Contains(t, lines[0], "Context:")
	assert.Contains(t, lines[0], "mgmt [RO]")
	assert.Contains(t, header, "<r>")
	assert.Contains(t, header, "Refresh")
	assert.Contains(t, header, "T9S")
	assert.Equal(t, 26, ansi.StringWidth(strings.Split(renderT9SLogo(defaultK9sSkin()), "\n")[0]))
}

func TestK9sHeaderShowsAppVersionInMetadataRow(t *testing.T) {
	header := renderK9sHeader(layoutK9sHeader(160), shellMetadata{
		Context: "mgmt", Health: "Healthy", Mode: "[RO]", AppVersion: "1.2.3",
	}, actionHints(viewServices, false), defaultK9sSkin())

	lines := strings.Split(header, "\n")
	assert.Contains(t, lines[3], "T9s Rev:")
	assert.Contains(t, lines[3], "1.2.3")
}

func TestK9sHeaderHidesLogoBeforeActionsOnNarrowScreens(t *testing.T) {
	header := renderK9sHeader(layoutK9sHeader(80), shellMetadata{Context: "test", Health: "Healthy", Mode: "[RO]"}, actionHints(viewServices, false), defaultK9sSkin())

	assert.NotContains(t, header, "T9S")
	assert.Contains(t, header, "<r>")
	for _, line := range strings.Split(header, "\n") {
		assert.Equal(t, 80, ansi.StringWidth(line))
	}
}

func TestK9sHeaderNeverExceedsTinyWidth(t *testing.T) {
	header := renderK9sHeader(layoutK9sHeader(20), shellMetadata{Context: "very-long-context", Mode: "[RO]"}, actionHints(viewServices, false), defaultK9sSkin())
	for _, line := range strings.Split(header, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 20)
	}
}
