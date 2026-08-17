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

	assert.Equal(t, "Upgrade to ghcr.io/siderolabs/installer:v1.13.3 cp-1? (y/n)", prompt)
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
