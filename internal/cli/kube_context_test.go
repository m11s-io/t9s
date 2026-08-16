package cli

import (
	"context"
	"testing"

	"github.com/m11s-io/t9s/internal/config"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveKubeContextUsesExplicitMappingWithoutCallingCatalog(t *testing.T) {
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		t.Fatal("catalog must not be called when an explicit mapping already resolves the context")
		return nil, nil
	}}
	associations := config.Associations{Items: []config.Association{{KubernetesContext: "prod-eu", TalosContext: "prod"}}}

	talosContext, openPicker, err := resolveKubeContext(t.Context(), catalog, associations, "prod-eu")

	require.NoError(t, err)
	assert.Equal(t, "prod", talosContext)
	assert.False(t, openPicker)
}

func TestResolveKubeContextFallsBackToExactNameMatch(t *testing.T) {
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return []domain.ClusterContext{{Name: "dev"}, {Name: "prod-eu"}}, nil
	}}

	talosContext, openPicker, err := resolveKubeContext(t.Context(), catalog, config.Associations{}, "prod-eu")

	require.NoError(t, err)
	assert.Equal(t, "prod-eu", talosContext)
	assert.False(t, openPicker)
}

func TestResolveKubeContextRequestsPickerWhenNothingMatches(t *testing.T) {
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return []domain.ClusterContext{{Name: "dev"}, {Name: "staging"}}, nil
	}}

	talosContext, openPicker, err := resolveKubeContext(t.Context(), catalog, config.Associations{}, "prod-eu")

	require.NoError(t, err)
	assert.Equal(t, "", talosContext)
	assert.True(t, openPicker)
}

func TestResolveKubeContextPropagatesCatalogError(t *testing.T) {
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return nil, assert.AnError
	}}

	_, _, err := resolveKubeContext(t.Context(), catalog, config.Associations{}, "prod-eu")

	assert.Error(t, err)
}
