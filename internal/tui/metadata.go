package tui

import (
	"strconv"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/version"
)

type shellMetadata struct {
	Context         string
	Cluster         string
	EndpointSummary string
	NodeSummary     string
	TalosVersion    string
	Health          string
	Mode            string
	AppVersion      string
}

func deriveShellMetadata(model application.Model) shellMetadata {
	mode := "[RO]"
	if model.WritesEnabled {
		mode = "[RW]"
	}
	metadata := shellMetadata{Context: model.ContextName, Mode: mode, AppVersion: version.Version}
	for _, clusterContext := range model.Contexts {
		if clusterContext.Name != model.ContextName {
			continue
		}
		metadata.Cluster = clusterContext.Cluster
		if len(clusterContext.Endpoints) > 0 {
			metadata.EndpointSummary = strconv.Itoa(len(clusterContext.Endpoints))
		}
		break
	}

	nodes := model.Nodes.Value.Nodes
	if len(nodes) > 0 {
		ready := 0
		versions := make(map[string]struct{})
		for _, node := range nodes {
			if node.Health == domain.HealthHealthy {
				ready++
			}
			if node.Version != "" {
				versions[node.Version] = struct{}{}
			}
		}
		metadata.NodeSummary = strconv.Itoa(ready) + "/" + strconv.Itoa(len(nodes))
		switch len(versions) {
		case 1:
			for version := range versions {
				metadata.TalosVersion = version
			}
		case 2:
			metadata.TalosVersion = "mixed"
		default:
			if len(versions) > 2 {
				metadata.TalosVersion = "mixed"
			}
		}
	}

	switch model.Nodes.Status {
	case application.Loading, application.Idle:
		metadata.Health = "Loading"
	case application.Failed:
		metadata.Health = "Unavailable"
	case application.Partial:
		metadata.Health = "Degraded"
	case application.Ready:
		metadata.Health = "Healthy"
		for _, node := range nodes {
			if node.Health != domain.HealthHealthy {
				metadata.Health = "Degraded"
				break
			}
		}
	}
	return metadata
}
