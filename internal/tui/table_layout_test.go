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

func TestWriteTableCellsUsesDisplayWidthForTruncationAndPadding(t *testing.T) {
	var output strings.Builder
	writeTableCells(&output, []string{"界界界", "\x1b[31mhealthy\x1b[0m"}, []int{5, 4})

	rendered := output.String()
	require.Equal(t, 10, ansi.StringWidth(rendered))
	assert.Contains(t, ansi.Strip(rendered), "界界…")
	assert.Contains(t, ansi.Strip(rendered), "hea…")
}
