package tui

import (
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
type directClusterState struct {
	contexts    []domain.ClusterContext
	contextName string
	nodes       directNodeState
	services    directServiceState
	events      directEventState
	etcd        directEtcdState
	kubernetes  directKubernetesState
	nodeStatus  directNodeStatusState
}

// directNodeState contains the node data and presentation status owned by the
// TUI. The backend returns domain snapshots and does not know about this state.
type directNodeState struct {
	Status directLoadStatus
	Value  domain.NodeSet
	Err    string
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

type directNodeStatusState struct {
	Status directLoadStatus
	Value  domain.NodeStatusSet
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

// resourceBrowserState is a small TUI fixture/input shape for the resource
// screens. Live results and load status are owned by each screen model.
type resourceBrowserState struct {
	KindsStatus     directLoadStatus
	KindsErr        string
	Kinds           domain.ResourceKindSet
	InstancesStatus directLoadStatus
	InstancesErr    string
	Instances       domain.ResourceInstanceSet
	SelectedKind    string
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

type directViewState struct {
	notice string
}

func fallback(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
