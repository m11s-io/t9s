package tui

import (
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func deriveDirectTestMetadata(contextName string, contexts []domain.ClusterContext, writesEnabled bool, status application.LoadStatus, nodes domain.NodeSet) shellMetadata {
	return deriveShellMetadata(contextName, contexts, writesEnabled, status, application.HealthInput{
		Nodes: nodes, NodesKnown: status == application.Ready || status == application.Partial,
	})
}

func TestDeriveShellMetadataUsesActiveContextAndNodeSnapshot(t *testing.T) {
	contexts := []domain.ClusterContext{
		{Name: "dev", Cluster: "development"},
		{Name: "prod", Cluster: "production", Endpoints: []string{"10.0.0.1", "10.0.0.2"}, Nodes: []string{"cp-1", "cp-2", "worker-1"}},
	}
	nodes := domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Health: domain.HealthHealthy, Version: "v1.13.2"},
		{Name: "cp-2", Health: domain.HealthHealthy, Version: "v1.13.3"},
		{Name: "worker-1", Health: domain.HealthUnhealthy, Version: "v1.13.3"},
	}}

	assert.Equal(t, shellMetadata{
		Context: "prod", Cluster: "production", EndpointSummary: "2", NodeSummary: "2/3",
		TalosVersion: "mixed", Health: "Degraded", Mode: "[RO]", AppVersion: "dev",
	}, deriveDirectTestMetadata("prod", contexts, false, application.Partial, nodes))
}

func TestDeriveShellMetadataDoesNotInventUnavailableValues(t *testing.T) {
	assert.Equal(t, shellMetadata{Context: "prod", Health: "Loading", Mode: "[RO]", AppVersion: "dev"}, deriveDirectTestMetadata("prod", nil, false, application.Loading, domain.NodeSet{}))
}

func TestDeriveShellMetadataReportsUniformTalosVersion(t *testing.T) {
	nodes := domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Health: domain.HealthHealthy, Version: "v1.13.6"}, {Health: domain.HealthHealthy, Version: "v1.13.6"},
	}}

	metadata := deriveDirectTestMetadata("", nil, false, application.Ready, nodes)
	assert.Equal(t, "v1.13.6", metadata.TalosVersion)
	assert.Equal(t, "Healthy", metadata.Health)
	assert.Equal(t, "2/2", metadata.NodeSummary)
}

func TestDeriveShellMetadataShowsReadWriteWhenWritesEnabled(t *testing.T) {
	metadata := deriveDirectTestMetadata("prod", nil, true, application.Loading, domain.NodeSet{})
	assert.Equal(t, "[RW]", metadata.Mode)
}

func TestDeriveShellMetadataShowsReadOnlyByDefault(t *testing.T) {
	metadata := deriveDirectTestMetadata("prod", nil, false, application.Loading, domain.NodeSet{})
	assert.Equal(t, "[RO]", metadata.Mode)
}
