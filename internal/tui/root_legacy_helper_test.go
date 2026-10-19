package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/testkit"
)

// readyRootModel remains for non-18a2 feature fixtures. Task18a4 will migrate
// the remaining CPU, navigation, resource, and time tests that consume it.
func readyRootModel() model {
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return nil, nil
		}},
	})
	return newModel(context.Background(), false, application.Model{
		ContextName: "prod",
		Nodes: application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{
			ID: "cp-1", Name: "cp-1", Role: domain.NodeRoleControl, Kubernetes: domain.KubernetesUnknown,
		}}}},
	}, runner)
}

// updateRoot and enterCommand remain shared helpers for fixture families that
// still exercise the legacy root until their scheduled migration.
func updateRoot(root model, message tea.Msg) (model, tea.Cmd) {
	updated, command := root.Update(message)
	return updated.(model), command
}

func enterCommand(t *testing.T, root model, value string) model {
	t.Helper()
	root, _ = updateRoot(root, keyPress(':'))
	for _, character := range value {
		root, _ = updateRoot(root, keyPress(character))
	}
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	return root
}
