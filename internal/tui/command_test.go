package tui

import (
	"context"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
)

func TestCommandResolutionIsClosedOverKnownNames(t *testing.T) {
	assert.Equal(t, commandNodes, resolveCommand("nodes"))
	assert.Equal(t, commandNodes, resolveCommand("no"))
	assert.Equal(t, commandContexts, resolveCommand("contexts"))
	assert.Equal(t, commandContexts, resolveCommand("ctx"))
	assert.Equal(t, commandUnknown, resolveCommand(" nodes"))
	assert.Equal(t, commandUnknown, resolveCommand("rm -rf anything"))
}

func TestResolveCommandAcceptsResourcesWithAndWithoutArgument(t *testing.T) {
	assert.Equal(t, commandResources, resolveCommand("resources"))
	assert.Equal(t, commandResources, resolveCommand("res"))
	assert.Equal(t, commandResources, resolveCommand("resources MachineStatus"))
	assert.Equal(t, commandUnknown, resolveCommand(" resources MachineStatus"), "leading whitespace must invalidate the command, matching every other command's strictness")

	argument, ok := resourcesCommandArgument("resources MachineStatus")
	assert.True(t, ok)
	assert.Equal(t, "MachineStatus", argument)

	_, ok = resourcesCommandArgument("nodes")
	assert.False(t, ok)
}

func TestCommandUnknownShowsNoticeWithoutInvokingPorts(t *testing.T) {
	var catalogCalls atomic.Int32
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			catalogCalls.Add(1)
			return nil, nil
		}},
	})
	root := newModel(t.Context(), false, application.Model{Route: application.RouteNodes}, runner)

	root, _ = updateRoot(root, keyPress(':'))
	for _, character := range "rm -rf anything" {
		root, _ = updateRoot(root, keyPress(character))
	}
	root, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Nil(t, command)
	assert.Contains(t, root.View().Content, `Unknown command "rm -rf anything"`)
	assert.Equal(t, application.RouteNodes, root.application.Route)
	assert.Zero(t, catalogCalls.Load())
}

func TestCommandWhitespaceIsUnknownRatherThanEmpty(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Route: application.RouteNodes}, nil)
	root, _ = updateRoot(root, keyPress(':'))
	root, _ = updateRoot(root, keyPress(' '))

	root, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Nil(t, command)
	assert.Contains(t, root.View().Content, `Unknown command " "`)
}

func TestCommandNodesClosesPaletteAndRetainsNodesRoute(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Route: application.RouteNodes}, nil)
	root, _ = updateRoot(root, keyPress(':'))
	for _, character := range "nodes" {
		root, _ = updateRoot(root, keyPress(character))
	}

	root, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Nil(t, command)
	assert.Equal(t, application.RouteNodes, root.application.Route)
	assert.NotContains(t, root.View().Content, "COMMAND :")
}

func TestCommandEmptyAndEscapeCloseWithoutChangingState(t *testing.T) {
	for name, closeKey := range map[string]tea.KeyPressMsg{
		"empty":  {Code: tea.KeyEnter},
		"escape": {Code: tea.KeyEsc},
	} {
		t.Run(name, func(t *testing.T) {
			initial := application.Model{Route: application.RouteNodes, ContextName: "prod", Notice: "keep"}
			root := newModel(t.Context(), false, initial, nil)
			root, _ = updateRoot(root, keyPress(':'))

			root, command := updateRoot(root, closeKey)

			assert.Nil(t, command)
			assert.Equal(t, initial, root.application)
			assert.NotContains(t, root.View().Content, "COMMAND :")
		})
	}
}

func updateRoot(root model, message tea.Msg) (model, tea.Cmd) {
	updated, command := root.Update(message)
	return updated.(model), command
}
