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

func deriveShellMetadata(contextName string, contexts []domain.ClusterContext, writesEnabled bool, nodeStatus application.LoadStatus, healthInput application.HealthInput) shellMetadata {
	mode := "[RO]"
	if writesEnabled {
		mode = "[RW]"
	}
	metadata := shellMetadata{Context: contextName, Mode: mode, AppVersion: version.Version}
	for _, clusterContext := range contexts {
		if clusterContext.Name != contextName {
			continue
		}
		metadata.Cluster = clusterContext.Cluster
		if len(clusterContext.Endpoints) > 0 {
			metadata.EndpointSummary = strconv.Itoa(len(clusterContext.Endpoints))
		}
		break
	}

	if len(healthInput.Nodes.Nodes) > 0 {
		ready := 0
		versions := make(map[string]struct{})
		for _, node := range healthInput.Nodes.Nodes {
			if node.Health == domain.HealthHealthy {
				ready++
			}
			if node.Version != "" {
				versions[node.Version] = struct{}{}
			}
		}
		metadata.NodeSummary = strconv.Itoa(ready) + "/" + strconv.Itoa(len(healthInput.Nodes.Nodes))
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

	switch nodeStatus {
	case application.Loading, application.Idle:
		metadata.Health = "Loading"
	case application.Failed:
		metadata.Health = "Unavailable"
	case application.Partial:
		metadata.Health = "Degraded"
	case application.Ready:
		metadata.Health = "Healthy"
		for _, node := range healthInput.Nodes.Nodes {
			if node.Health != domain.HealthHealthy {
				metadata.Health = "Degraded"
				break
			}
		}
		for _, diagnosis := range application.EvaluateObservations(healthInput) {
			if diagnosis.Severity == domain.SeverityCritical || diagnosis.Severity == domain.SeverityWarning {
				metadata.Health = "Degraded"
				break
			}
			if diagnosis.Severity == domain.SeverityUnknown {
				metadata.Health = "Degraded"
			}
		}
	}
	return metadata
}
