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
	assert.Contains(t, ansi.Strip(lines[0]), "services(all)[3]")
	assert.Equal(t, 38, frame.InnerWidth)
	assert.Equal(t, 4, frame.InnerHeight)
	assert.True(t, strings.HasPrefix(ansi.Strip(lines[0]), "┌"))
	assert.True(t, strings.HasSuffix(ansi.Strip(lines[5]), "┘"))
}

func TestResourceFrameColorsTitleScopeAndCount(t *testing.T) {
	skin := defaultK9sSkin()
	rendered := renderResourceFrame(layoutResourceFrame(40, 6, "nodes(all)[12]"), "", skin)
	line := strings.Split(rendered, "\n")[0]

	want := skin.title.Render(" nodes") +
		skin.titleHighlight.Render("(all)") +
		skin.titleCounter.Render("[12]") +
		skin.title.Render(" ")
	assert.Contains(t, line, want)
}

func TestDetailTitlesIncludeAndHighlightSelectedObject(t *testing.T) {
	tests := []struct {
		name  string
		frame viewFrame
		want  string
	}{
		{name: "node", frame: viewFrame{Kind: viewNodeDetail, Label: "worker-1"}, want: "node(worker-1)"},
		{name: "service", frame: viewFrame{Kind: viewServiceDetail, Label: "api@node-a"}, want: "service(api@node-a)"},
		{name: "process", frame: viewFrame{Kind: viewProcessDetail, ProcessPID: 42}, want: "process(42)"},
		{name: "disk", frame: viewFrame{Kind: viewDiskDetail, DeviceName: "sda"}, want: "disk(sda)"},
		{name: "link", frame: viewFrame{Kind: viewLinkDetail, LinkName: "eth0"}, want: "link(eth0)"},
		{name: "resource", frame: viewFrame{Kind: viewResourceDetail, ResourceKind: "pods", ResourceNamespace: "default", ResourceID: "api-1"}, want: "pods(default/api-1)"},
	}

	skin := defaultK9sSkin()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := model{views: newViewStack(test.frame)}
			title := directResourceTitle(model, test.frame.Kind)
			assert.Equal(t, test.want, title)

			line := strings.Split(renderResourceFrame(layoutResourceFrame(80, 6, title), "", skin), "\n")[0]
			start := strings.IndexByte(test.want, '(')
			assert.Contains(t, line,
				skin.title.Render(" "+test.want[:start])+skin.titleHighlight.Render(test.want[start:])+skin.title.Render(" "),
			)
		})
	}
}

func TestResourceFrameClipsContentToInnerDimensions(t *testing.T) {
	rendered := renderResourceFrame(layoutResourceFrame(20, 4, "nodes(all)[1]"), "one\ntwo\nthree", defaultK9sSkin())
	assert.NotContains(t, rendered, "three")
}

func TestResourceFrameTitleSanitizesUntrustedScope(t *testing.T) {
	rendered := renderResourceFrameTitle("processes(worker\x1b[2Jnode)[3]", defaultK9sSkin())

	assert.NotContains(t, rendered, "\x1b[2J")
	assert.Equal(t, "processes(workernode)[3]", ansi.Strip(rendered))
}
