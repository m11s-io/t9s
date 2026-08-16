package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestK9sTableSelectionFillsTheInnerRowWithoutMarker(t *testing.T) {
	row := renderSelectedRow("cp-1  etcd", 30, true, defaultK9sSkin())
	unselected := renderSelectedRow("cp-1  etcd", 30, false, defaultK9sSkin())

	assert.Equal(t, 30, ansi.StringWidth(row))
	assert.NotContains(t, ansi.Strip(row), ">")
	assert.Equal(t, "cp-1  etcd", strings.TrimSpace(ansi.Strip(row)))
	assert.Contains(t, row, "\x1b[38;2;0;0;0;48;2;0;255;255m")
	assert.NotEqual(t, unselected, row, "the selected row must be visibly distinct")
}

func TestK9sUnselectedRowUsesTheSameDensity(t *testing.T) {
	row := renderSelectedRow("cp-1  etcd", 30, false, defaultK9sSkin())
	assert.Equal(t, 30, ansi.StringWidth(row))
	assert.Equal(t, "cp-1  etcd", strings.TrimSpace(ansi.Strip(row)))
}
