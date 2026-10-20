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
	assert.Equal(t, 26, ansi.StringWidth(strings.Split(renderT9SLogo(defaultK9sSkin()), "\n")[0]))
}

func TestK9sHeaderAddsOneCellGutter(t *testing.T) {
	header := renderK9sHeader(layoutK9sHeader(160), shellMetadata{Context: "mgmt", Mode: "[RO]"}, nil, defaultK9sSkin())
	assert.True(t, strings.HasPrefix(ansi.Strip(strings.Split(header, "\n")[0]), " Context:"))
}

func TestT9SLogoRestoresOriginalLettering(t *testing.T) {
	logo := strings.Split(renderT9SLogo(defaultK9sSkin()), "\n")
	got := make([]string, len(logo))
	for index, line := range logo {
		got[index] = strings.TrimRight(ansi.Strip(line), " ")
	}

	assert.Equal(t, []string{
		" _______    ___     _____",
		"|__   __|  / _ \\   / ____|",
		"   | |    | (_) | | (___",
		"   | |     \\__, |  \\___ \\",
		"   | |       / /    ___) |",
		"   |_|      /_/    |_____/",
	}, got)
}

func TestT9SLogoOmitsRedundantTextFooter(t *testing.T) {
	logo := renderT9SLogo(defaultK9sSkin())

	assert.Len(t, strings.Split(logo, "\n"), 6)
	assert.NotContains(t, logo, "T9S")
}

func TestK9sHeaderActionColumnsStayCompact(t *testing.T) {
	hints := []actionHint{
		{Key: "a", Label: "Alpha"},
		{Key: "b", Label: "Alpha"},
		{Key: "c", Label: "Alpha"},
		{Key: "d", Label: "Alpha"},
		{Key: "e", Label: "Alpha"},
		{Key: "f", Label: "Alpha"},
		{Key: "g", Label: "Alpha"},
		{Key: "h", Label: "Beta"},
	}

	lines := renderHeaderActions(hints, 80, defaultK9sSkin())

	assert.Equal(t, "<a> Alpha   <g> Alpha", ansi.Strip(lines[0]))
	assert.Equal(t, "<b> Alpha   <h> Beta", strings.TrimRight(ansi.Strip(lines[1]), " "))
	assert.Empty(t, ansi.Strip(lines[6]))
}

func TestK9sHeaderActionColumnsHaveReadableSeparation(t *testing.T) {
	lines := renderHeaderActions(actionHints(viewNodes, true), 149, defaultK9sSkin())
	markLine := ansi.Strip(lines[2])
	upgradeLine := ansi.Strip(lines[1])
	spaceKey := strings.Index(markLine, "<space>")
	markLabel := strings.Index(markLine, "Mark")
	upgradeKey := strings.Index(upgradeLine, "<U>")
	upgradeLabel := strings.Index(upgradeLine, "Upgrade")

	assert.GreaterOrEqual(t, spaceKey, 32)
	assert.GreaterOrEqual(t, upgradeKey, 50)
	assert.Equal(t, 8, markLabel-spaceKey)
	assert.Equal(t, 8, upgradeLabel-upgradeKey)
}

func TestK9sHeaderUsesTerminalDefaultBackground(t *testing.T) {
	header := renderK9sHeader(layoutK9sHeader(160), shellMetadata{
		Context: "mgmt", Health: "Healthy", Mode: "[RW]", AppVersion: "1.2.3",
	}, actionHints(viewNodes, true), defaultK9sSkin())

	for _, line := range strings.Split(header, "\n") {
		assert.NotContains(t, line, "\x1b[48;2;0;0;0m", "header should not force black behind text or padding")
	}
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
