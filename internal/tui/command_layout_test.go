package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandInputAppearsBelowHeaderAboveTable(t *testing.T) {
	m := readyDirectRootModel(t)
	m, _ = updateDirect(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = updateDirect(m, keyPress(':'))
	for _, key := range "services" {
		m, _ = updateDirect(m, keyPress(key))
	}
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	require.Len(t, lines, 30)
	promptRow, tableRow := -1, -1
	for i, line := range lines {
		if strings.Contains(line, ">services") {
			promptRow = i
		}
		if strings.Contains(line, "NAME") && strings.Contains(line, "ROLE") {
			tableRow = i
		}
	}
	require.Equal(t, k9sHeaderHeight+1, promptRow, "input belongs immediately under the header inside a border")
	require.Greater(t, tableRow, promptRow)
	assert.Contains(t, lines[promptRow], "services")
	assert.Contains(t, lines[promptRow-1], "┌")
	assert.Contains(t, lines[promptRow+1], "└")
	assert.NotContains(t, strings.Join(lines[len(lines)-2:], "\n"), "services")
	m, _ = updateDirect(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.False(t, m.palette.active)
	assert.NotContains(t, m.View().Content, "services")
}

func TestCommandInputFitsTerminalAndKeepsLongQueryVisible(t *testing.T) {
	for _, size := range []struct{ width, height int }{{80, 24}, {20, 10}, {5, 4}, {1, 1}, {0, 0}} {
		m := readyDirectRootModel(t)
		m, _ = updateDirect(m, tea.WindowSizeMsg{Width: size.width, Height: size.height})
		m, _ = updateDirect(m, keyPress(':'))
		for range 60 {
			m, _ = updateDirect(m, keyPress('x'))
		}
		for _, key := range "TAIL" {
			m, _ = updateDirect(m, keyPress(key))
		}
		content := m.View().Content
		if size.width >= 20 && size.height >= 10 {
			assert.Contains(t, ansi.Strip(content), "TAIL", "horizontal scrolling must keep the end of a long command visible")
		}
		if size.height == 0 {
			assert.Empty(t, content)
			continue
		}
		lines := strings.Split(content, "\n")
		require.Len(t, lines, size.height)
		for _, line := range lines {
			assert.LessOrEqual(t, ansi.StringWidth(line), size.width)
		}
	}
}

func TestCommandInputKeepsCursorVisibleAfterResize(t *testing.T) {
	m := readyDirectRootModel(t)
	m, _ = updateDirect(m, keyPress(':'))
	for _, key := range strings.Repeat("x", 50) + "TAIL" {
		m, _ = updateDirect(m, keyPress(key))
	}
	m, _ = updateDirect(m, tea.WindowSizeMsg{Width: 20, Height: 10})
	assert.Contains(t, ansi.Strip(m.View().Content), "TAIL")
	assert.Equal(t, 54, m.palette.input.Position(), "resize must preserve the edit position")
}
