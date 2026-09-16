package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func memoryTestState() application.MemoryState {
	return application.MemoryState{
		Status: application.Ready,
		Node:   "cp-1",
		Value: domain.MemorySnapshot{
			TotalBytes:     1024,
			FreeBytes:      100,
			AvailableBytes: 256,
			UsedBytes:      768,
			BuffersBytes:   16,
			CachedBytes:    32,
			SwapTotalBytes: 512,
			SwapFreeBytes:  500,
			DirtyBytes:     4,
			SlabBytes:      8,
		},
	}
}

func renderMemory(width int, state application.MemoryState) string {
	return newMemoryModel(state).view(width)
}

func TestMemoryRenderSemanticColumns(t *testing.T) {
	rendered := renderMemory(80, memoryTestState())
	lines := strings.Split(rendered, "\n")
	require.GreaterOrEqual(t, len(lines), 2)
	assert.Equal(t,
		[]string{"METRIC", "VALUE"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	joined := ansi.Strip(rendered)
	assert.Contains(t, joined, "Total")
	assert.Contains(t, joined, formatBytes(1024))
	assert.Contains(t, joined, "Used")
}

func TestMemoryRenderEmptyState(t *testing.T) {
	loading := newMemoryModel(application.MemoryState{Status: application.Loading})
	assert.Equal(t, "Loading memory…", loading.viewSized(contentSize{Width: 80, Height: 10}))

	empty := newMemoryModel(application.MemoryState{Status: application.Ready})
	assert.Equal(t, "No memory data", empty.viewSized(contentSize{Width: 80, Height: 10}))
}

func TestMemoryFilterMatchesMetric(t *testing.T) {
	memory := newMemoryModel(memoryTestState())
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('s'), keyPress('w'), keyPress('a'), keyPress('p'),
		{Code: tea.KeyEnter},
	} {
		memory = memory.update(message)
	}

	rendered := memory.view(80)

	assert.Contains(t, rendered, "Swap total")
	assert.NotContains(t, rendered, "Total")
	assert.NotContains(t, rendered, "Cached")
}
