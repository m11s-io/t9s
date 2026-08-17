package tui

import (
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/stretchr/testify/assert"
)

func TestRenderPendingActionPromptRollbackVerb(t *testing.T) {
	prompt := renderPendingActionPrompt(application.PendingAction{Kind: application.ActionRollback, Targets: []string{"cp-1"}})

	assert.Equal(t, "Rollback cp-1? (y/n)", prompt)
}

func TestRenderPendingActionPromptUpgradeVerbIncludesImage(t *testing.T) {
	prompt := renderPendingActionPrompt(application.PendingAction{
		Kind:    application.ActionUpgrade,
		Targets: []string{"cp-1"},
		Image:   "ghcr.io/siderolabs/installer:v1.13.3",
	})

	// The image is budgeted via truncateWarningTail (same as warnings), so a
	// long image reference keeps its tail — where the version lives — with
	// an ellipsis prefix rather than being embedded verbatim.
	assert.Equal(t, "Upgrade to …olabs/installer:v1.13.3 cp-1? (y/n)", prompt)
}

func TestRenderPendingActionPromptUpgradeStaysBoundedWithLongWarning(t *testing.T) {
	prompt := renderPendingActionPrompt(application.PendingAction{
		Kind:    application.ActionUpgrade,
		Targets: []string{"cp-1"},
		Image:   "ghcr.io/siderolabs/installer:v1.13.3",
		Warning: "control-plane node(s); would drop etcd to 1/3 — below quorum (need 2)",
	})

	// The confirm cue and the image's version tag must survive even when a
	// realistic long quorum warning is also present — this is the whole
	// point of budgeting the image via truncateWarningTail instead of
	// embedding it verbatim, which could push the line arbitrarily long.
	assert.Contains(t, prompt, "(y/n)")
	assert.Contains(t, prompt, "v1.13.3")
	assert.LessOrEqual(t, len([]rune(prompt)), 100, "the image and warning are each individually budgeted (24 and pendingActionWarningBudget runes) plus a short fixed-text overhead, so the whole line must stay bounded rather than growing without limit the way an unbounded image reference would")
}

func TestRenderPendingServiceActionPromptFormatsServiceAtNode(t *testing.T) {
	prompt := renderPendingServiceActionPrompt(application.PendingServiceAction{Kind: application.ServiceActionRestart, Node: "cp-1", Service: "etcd"})

	assert.Equal(t, "Restart etcd@cp-1? (y/n)", prompt)
}

func TestRenderPendingServiceActionPromptIncludesWarning(t *testing.T) {
	prompt := renderPendingServiceActionPrompt(application.PendingServiceAction{
		Kind:    application.ServiceActionStop,
		Node:    "cp-1",
		Service: "etcd",
		Warning: "control-plane node(s); would drop etcd to 1/3 — below quorum (need 2)",
	})

	assert.Contains(t, prompt, "!!")
	assert.Contains(t, prompt, "Stop etcd@cp-1? (y/n)")
}
