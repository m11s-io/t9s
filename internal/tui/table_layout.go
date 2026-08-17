package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type tableColumn[T any] struct {
	header   string
	minWidth int
	value    func(T) string
	grow     bool
}

func calculateColumnWidths[T any](width int, columns []tableColumn[T]) []int {
	widths := make([]int, len(columns))
	used := selectionWidth + columnSpacing*max(0, len(columns)-1)
	growIndex := -1
	for index, column := range columns {
		widths[index] = column.minWidth
		used += column.minWidth
		if column.grow {
			growIndex = index
		}
	}
	if remaining := width - used; remaining > 0 && growIndex >= 0 {
		widths[growIndex] += remaining
	}
	return widths
}

func tableHeaders[T any](columns []tableColumn[T]) []string {
	values := make([]string, len(columns))
	for index, column := range columns {
		values[index] = column.header
	}
	return values
}

func tableRowValues[T any](value T, columns []tableColumn[T]) []string {
	values := make([]string, len(columns))
	for index, column := range columns {
		values[index] = column.value(value)
	}
	return values
}

// sanitizeUntrustedText strips ANSI escape sequences and neutralizes
// remaining C0 control characters (and DEL) so the result is safe to measure
// with ansi.StringWidth and safe to render without corrupting the terminal
// state (e.g. \r/\b repositioning the cursor, \x0e switching character
// sets). Tabs are converted to a single space rather than dropped, since
// dropping them would misrepresent the content; expanding to a tab stop is
// not done here because callers only need an accurate, stable width, not
// column-alignment fidelity. Content sourced from Talos (log lines, and
// table cells like node/service/event names) is untrusted and must be
// sanitized before it is measured or rendered.
func sanitizeUntrustedText(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, ansi.Strip(value))
}

func writeTableCells(output *strings.Builder, values []string, widths []int) {
	for index, value := range values {
		if index >= len(widths) {
			break
		}
		if index > 0 {
			output.WriteByte(' ')
		}
		cell := ansi.Truncate(sanitizeUntrustedText(value), widths[index], "…")
		output.WriteString(cell)
		if padding := widths[index] - ansi.StringWidth(cell); padding > 0 && index < len(values)-1 {
			output.WriteString(strings.Repeat(" ", padding))
		}
	}
}
