package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceFrameHasExactDimensionsAndEmbeddedTitle(t *testing.T) {
	frame := layoutResourceFrame(40, 6, "services(all)[3]")
	rendered := renderResourceFrame(frame, "HEADER\nrow one\nrow two", defaultK9sSkin())
	lines := strings.Split(rendered, "\n")

	require.Len(t, lines, 6)
	for _, line := range lines {
		assert.Equal(t, 40, ansi.StringWidth(line))
	}
	assert.Contains(t, lines[0], "services(all)[3]")
	assert.Equal(t, 38, frame.InnerWidth)
	assert.Equal(t, 4, frame.InnerHeight)
	assert.True(t, strings.HasPrefix(ansi.Strip(lines[0]), "┌"))
	assert.True(t, strings.HasSuffix(ansi.Strip(lines[5]), "┘"))
}

func TestResourceFrameClipsContentToInnerDimensions(t *testing.T) {
	rendered := renderResourceFrame(layoutResourceFrame(20, 4, "nodes(all)[1]"), "one\ntwo\nthree", defaultK9sSkin())
	assert.NotContains(t, rendered, "three")
}
