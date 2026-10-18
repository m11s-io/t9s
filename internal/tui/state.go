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
	services    directServiceState
	events      directEventState
	etcd        directEtcdState
	kubernetes  directKubernetesState
}

type directLoadStatus string

const (
	directLoading directLoadStatus = "loading"
	directReady   directLoadStatus = "ready"
	directPartial directLoadStatus = "partial"
	directFailed  directLoadStatus = "failed"
)

type directServiceState struct {
	Status directLoadStatus
	Value  domain.ServiceSet
	Err    string
}

type directEventState struct {
	Status directLoadStatus
	Value  domain.EventSet
	Err    string
}

type directEtcdState struct {
	Status directLoadStatus
	Value  domain.EtcdSet
	Err    string
}

func directEtcdLoadStatus(etcd domain.EtcdSet) directLoadStatus {
	for _, member := range etcd.Members {
		if !member.StatusKnown || len(member.Errors) > 0 {
			return directPartial
		}
	}
	return directReady
}

type directKubernetesState struct {
	Status directLoadStatus
	Nodes  map[string]domain.KubernetesNodeSnapshot
	Err    string
}

func directNodeLoadStatus(nodes domain.NodeSet) directLoadStatus {
	for _, node := range nodes.Nodes {
		if node.Problem != "" {
			return directPartial
		}
	}
	return directReady
}

func directServiceLoadStatus(services domain.ServiceSet) directLoadStatus {
	if len(services.Problems) > 0 {
		return directPartial
	}
	return directReady
}

// directNodeState temporarily aliases the node renderer's presentation
// contract. Task 18 can remove it after that renderer consumes direct state.
type directNodeState = application.NodeState

type directViewState struct {
	notice string
}
