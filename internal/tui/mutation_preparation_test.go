package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/stretchr/testify/assert"
)

func TestNodePreparationFailureClearsProgressAndShowsError(t *testing.T) {
	for _, kind := range []application.ActionKind{application.ActionShutdown, application.ActionUpgrade} {
		t.Run(string(kind), func(t *testing.T) {
			m := directUpgradeModel(t, nil)
			m.mutation.operation = 1
			m.mutation.busy = true
			m.mutation.notice = "Preparing mutation…"
			m, _ = updateDirect(m, directNodePrepared{operation: 1, generation: m.generation(), request: application.NodeActionRequest{Kind: kind}, err: context.DeadlineExceeded})
			assert.False(t, m.mutation.busy)
			assert.Empty(t, m.mutation.notice)
			content := ansi.Strip(m.View().Content)
			assert.NotContains(t, content, "Preparing mutation")
			assert.Contains(t, content, "preparation timed out")
			assert.Contains(t, content, "no action was sent")
		})
	}
}

func TestNodePreparationFailureDoesNotExposeBackendSecrets(t *testing.T) {
	m := directUpgradeModel(t, nil)
	m.mutation.operation = 1
	m.mutation.busy = true
	m.mutation.notice = "Preparing mutation…"
	m, _ = updateDirect(m, directNodePrepared{operation: 1, generation: m.generation(), request: application.NodeActionRequest{Kind: application.ActionShutdown}, err: errors.New("Bearer secret-token")})
	assert.Empty(t, m.mutation.notice)
	assert.NotContains(t, ansi.Strip(m.View().Content), "secret-token")
	assert.Contains(t, m.view.notice, "preparation failed")
	assert.Contains(t, m.view.notice, "no action was sent")
}
