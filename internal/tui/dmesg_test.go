package tui

import (
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestDmesgViewRendersLinesAndFollows(t *testing.T) {
	model := newDmesgModel(application.DmesgState{
		Status: application.Ready,
		Lines:  []string{"kernel: boot", "kernel: ready"},
	})

	rendered := model.viewSized(contentSize{Width: 80, Height: 10})

	assert.Contains(t, rendered, "DMESG")
	assert.Contains(t, rendered, "kernel: boot")
	assert.Contains(t, rendered, "kernel: ready")
}

func TestDmesgClearRequestedMapsToClearDmesg(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Dmesg: application.DmesgState{
		Status:  application.Ready,
		Request: domain.DmesgRequest{Node: "cp-1", Follow: true, Tail: true},
		Lines:   []string{"line"},
	}}, application.NewRunner(application.Dependencies{}))
	root.splash = false
	root.views = root.views.push(viewFrame{Kind: viewDmesg, Label: "cp-1 > dmesg"})
	root.dmesg = newDmesgModel(root.application.Dmesg)

	root, _ = updateRoot(root, shiftKeyPress('C'))

	assert.Nil(t, root.application.Dmesg.Lines, "C must route to ClearDmesg")
}
