package tui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDmesgViewRendersLinesAndFollows(t *testing.T) {
	model := newDirectDmesgModel().setStreamState(directReady, []string{"kernel: boot", "kernel: ready"}, "", false)

	rendered := model.viewSized(contentSize{Width: 80, Height: 10})

	assert.Contains(t, rendered, "DMESG")
	assert.Contains(t, rendered, "kernel: boot")
	assert.Contains(t, rendered, "kernel: ready")
}

func TestDmesgClearRequestedMapsToClearDmesg(t *testing.T) {
	root := newModel(context.Background(), nil, LaunchOptions{})
	root.splash = false
	root.views = root.views.push(viewFrame{Kind: viewDmesg, Label: "cp-1 > dmesg"})
	root.dmesg = newDirectDmesgModel().setStreamState(directReady, []string{"line"}, "", false)

	root, command := updateDirect(root, shiftKeyPress('C'))
	assert.NotNil(t, command)
	root, _ = updateDirect(root, command())

	assert.Empty(t, root.dmesg.state.Lines, "C must clear the direct dmesg display state")
}
