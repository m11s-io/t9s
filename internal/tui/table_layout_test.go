package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tableLayoutFixture struct {
	name string
	role string
}

func TestCalculateColumnWidthsUsesMinimumsAndGrowsOneColumn(t *testing.T) {
	columns := []tableColumn[tableLayoutFixture]{
		{header: "NAME", minWidth: 4, grow: true, value: func(row tableLayoutFixture) string { return row.name }},
		{header: "ROLE", minWidth: 4, value: func(row tableLayoutFixture) string { return row.role }},
	}

	assert.Equal(t, []int{4, 4}, calculateColumnWidths(5, columns), "narrow layouts retain current minimum widths")
	assert.Equal(t, []int{9, 4}, calculateColumnWidths(selectionWidth+columnSpacing+4+4+5, columns))
	assert.Equal(t, []string{"NAME", "ROLE"}, tableHeaders(columns))
	assert.Equal(t, []string{"control-plane", "cp"}, tableRowValues(tableLayoutFixture{name: "control-plane", role: "cp"}, columns))
}

func TestContentColumnWidthsKeepColumnsNearTheirValuesAndFitAvailableWidth(t *testing.T) {
	columns := []tableColumn[tableLayoutFixture]{
		{header: "NAME", minWidth: 4, value: func(row tableLayoutFixture) string { return row.name }},
		{header: "ROLE", minWidth: 4, value: func(row tableLayoutFixture) string { return row.role }},
	}
	rows := []tableLayoutFixture{{name: "control-plane", role: "cp"}}

	widths := contentColumnWidths(30, columns, rows)

	assert.Equal(t, []int{13, 4}, widths)
	assert.LessOrEqual(t, selectionWidth+columnSpacing+widths[0]+widths[1], 30)
	assert.Equal(t, []int{4, 4}, contentColumnWidths(10, columns, rows), "minimum widths are preserved when the terminal is too narrow")
}

func TestContentColumnWidthsGrowsMarkedColumnAfterSizingContent(t *testing.T) {
	columns := []tableColumn[tableLayoutFixture]{
		{header: "NAME", minWidth: 4, value: func(row tableLayoutFixture) string { return row.name }},
		{header: "PATH", minWidth: 4, grow: true, value: func(row tableLayoutFixture) string { return row.role }},
	}

	assert.Equal(t, []int{4, 24}, contentColumnWidths(30, columns, []tableLayoutFixture{{name: "cp", role: "/"}}))
}

func TestWriteTableCellsUsesDisplayWidthForTruncationAndPadding(t *testing.T) {
	var output strings.Builder
	writeTableCells(&output, []string{"界界界", "\x1b[31mhealthy\x1b[0m"}, []int{5, 4})

	rendered := output.String()
	require.Equal(t, 10, ansi.StringWidth(rendered))
	assert.Contains(t, ansi.Strip(rendered), "界界…")
	assert.Contains(t, ansi.Strip(rendered), "hea…")
}

func TestWriteTableCellsIgnoresValuesBeyondWidths(t *testing.T) {
	var output strings.Builder

	assert.NotPanics(t, func() {
		writeTableCells(&output, []string{"first", "second", "third"}, []int{5, 6})
	}, "a caller-supplied values slice longer than widths must not panic")

	assert.Equal(t, []string{"first", "second"}, strings.Fields(output.String()))
}

func TestWriteTableCellsSanitizesUntrustedContentBeforeMeasuring(t *testing.T) {
	var output strings.Builder
	writeTableCells(&output, []string{"safe\rEVIL", "abc\x1b[2Jxyz"}, []int{20, 20})

	rendered := output.String()
	assert.NotContains(t, rendered, "\r")
	assert.NotContains(t, rendered, "\x1b[")
	assert.Contains(t, rendered, "safeEVIL")
	assert.Contains(t, rendered, "abcxyz")
}
