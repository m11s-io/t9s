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

func writeTableCells(output *strings.Builder, values []string, widths []int) {
	for index, value := range values {
		if index > 0 {
			output.WriteByte(' ')
		}
		cell := ansi.Truncate(value, widths[index], "…")
		output.WriteString(cell)
		if padding := widths[index] - ansi.StringWidth(cell); padding > 0 && index < len(values)-1 {
			output.WriteString(strings.Repeat(" ", padding))
		}
	}
}
