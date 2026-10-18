package tui

import (
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

// LaunchOptions contains TUI-only startup choices. Write permission is copied
// into each backend session as trusted configuration.
type LaunchOptions struct {
	ContextName       string
	NodeFocus         string
	OpenContextPicker bool
	WritesEnabled     bool
}

// directClusterState is the single root-owned store for shared cluster data.
// The node renderer still consumes application.NodeState during migration.
type directClusterState struct {
	contexts    []domain.ClusterContext
	contextName string
	nodes       directNodeState
}

// directNodeState temporarily aliases the node renderer's presentation
// contract. Task 18 can remove it after that renderer consumes direct state.
type directNodeState = application.NodeState

type directViewState struct {
	notice string
}
