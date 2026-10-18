package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCommandNavigationClosesDiagnosticStream(t *testing.T) {
	stream := &testkit.FakeDmesgStream{}
	app, _ := application.NewModel("prod")
	app, _ = application.Update(app, application.SessionOpened{Generation: app.Generation, Session: &testkit.FakeSession{DmesgReader: &testkit.FakeDmesgReader{OpenFunc: func(context.Context, domain.DmesgRequest) (ports.DmesgStream, error) { return stream, nil }}}})
	app, open := application.Update(app, application.OpenDmesg{Request: domain.DmesgRequest{Node: "worker", Follow: true}})
	require.NotNil(t, open)
	app, _ = application.Update(app, open(t.Context(), application.Dependencies{}))
	root := newModel(t.Context(), false, app, application.NewRunner(application.Dependencies{}))
	root.views = newViewStack(viewFrame{Kind: viewNodes}).push(viewFrame{Kind: viewDmesg})
	root, _ = updateRoot(root, keyPress(':'))
	for _, r := range "nodes" {
		root, _ = updateRoot(root, keyPress(r))
	}
	root, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	root = drainFiniteRootCommand(t, root, command)
	assert.Equal(t, viewNodes, root.views.top().Kind)
	assert.True(t, stream.Closed())
	assert.Equal(t, application.DmesgState{}, root.application.Dmesg)
}
