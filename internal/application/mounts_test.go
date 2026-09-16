package application_test

import (
	"context"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenMountsLoadsTheRequestedNode(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withMountReader(model, &testkit.FakeMountReader{
		ListFunc: func(_ context.Context, node string) (domain.MountSet, error) {
			assert.Equal(t, "cp-1", node)
			return domain.MountSet{Mounts: []domain.MountSnapshot{{Filesystem: "/dev/sda1", MountedOn: "/"}}}, nil
		},
	})

	model, effect := application.Update(model, application.OpenMounts{Node: "cp-1"})
	assert.Equal(t, application.Loading, model.Mounts.Status)
	assert.Equal(t, "cp-1", model.Mounts.Node)
	require.NotNil(t, effect)

	message := effect(t.Context(), application.Dependencies{})
	loaded, ok := message.(application.MountsLoaded)
	require.True(t, ok)
	assert.Equal(t, "cp-1", loaded.Node)
	assert.Len(t, loaded.Mounts.Mounts, 1)
}

func TestRefreshMountsRefetchesTheRememberedNode(t *testing.T) {
	calls := []string{}
	model := application.Model{Generation: 1}
	model = withMountReader(model, &testkit.FakeMountReader{
		ListFunc: func(_ context.Context, node string) (domain.MountSet, error) {
			calls = append(calls, node)
			return domain.MountSet{}, nil
		},
	})
	var openEffect application.Effect
	model, openEffect = application.Update(model, application.OpenMounts{Node: "cp-1"})
	require.NotNil(t, openEffect)
	openEffect(t.Context(), application.Dependencies{})

	_, effect := application.Update(model, application.RefreshMounts{})
	require.NotNil(t, effect)
	effect(t.Context(), application.Dependencies{})

	require.Len(t, calls, 2, "OpenMounts fetches once, RefreshMounts fetches again")
	assert.Equal(t, "cp-1", calls[1])
}

func TestMountsLoadedIgnoresResultForAnotherNode(t *testing.T) {
	model := application.Model{Generation: 1, Mounts: application.MountState{Status: application.Loading, Node: "cp-2"}}

	model, _ = application.Update(model, application.MountsLoaded{Generation: 1, Node: "cp-1", Mounts: domain.MountSet{Mounts: []domain.MountSnapshot{{Filesystem: "/dev/sda1"}}}})

	assert.Equal(t, application.Loading, model.Mounts.Status, "a late result for a previously opened node must not overwrite the current node")
	assert.Empty(t, model.Mounts.Value.Mounts)
}

func TestMountsFailedSetsFailedStatus(t *testing.T) {
	model := application.Model{Generation: 1, Mounts: application.MountState{Node: "cp-1"}}

	model, effect := application.Update(model, application.MountsFailed{Generation: 1, Node: "cp-1", Err: assert.AnError})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.Mounts.Status)
	assert.Equal(t, "mounts unavailable", model.Mounts.Err)
	assert.Equal(t, "cp-1", model.Mounts.Node, "failure must not lose track of which node was open")
}

func TestMountsFailedIgnoresResultForAnotherNode(t *testing.T) {
	model := application.Model{Generation: 1, Mounts: application.MountState{Status: application.Loading, Node: "cp-2"}}

	model, _ = application.Update(model, application.MountsFailed{Generation: 1, Node: "cp-1", Err: assert.AnError})

	assert.Equal(t, application.Loading, model.Mounts.Status, "a stale failure for another node must not fail the current view")
}

func TestSelectContextResetsMounts(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", Mounts: application.MountState{
		Status: application.Ready,
		Node:   "cp-1",
		Value:  domain.MountSet{Mounts: []domain.MountSnapshot{{Filesystem: "/dev/sda1"}}},
	}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, application.MountState{}, model.Mounts)
}

func withMountReader(model application.Model, reader ports.MountReader) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Mounts: reader})
	return model
}
