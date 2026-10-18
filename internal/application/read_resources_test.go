package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceReadsUseExplicitScope(t *testing.T) {
	var gotNode, gotKind, gotID string
	kindReader := &testkit.FakeResourceKindReader{ListFunc: func(context.Context) (domain.ResourceKindSet, error) {
		return domain.ResourceKindSet{Kinds: []domain.ResourceKindSnapshot{{Type: "Secrets.runtime.talos.dev", Sensitive: true}}}, nil
	}}
	reader := &testkit.FakeResourceInstanceReader{
		ListFunc: func(_ context.Context, node, kind string) (domain.ResourceInstanceSet, error) {
			gotNode, gotKind = node, kind
			return domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{Namespace: "runtime", Type: kind, ID: "machine-a", Phase: "running"}}}, nil
		},
		GetFunc: func(_ context.Context, node, kind, id string) (domain.ResourceInstanceSnapshot, error) {
			gotNode, gotKind, gotID = node, kind, id
			return domain.ResourceInstanceSnapshot{Namespace: "runtime", Type: kind, ID: id, YAML: "metadata: complete"}, nil
		},
	}
	session := openReadTestSession(t, &testkit.FakeSession{ResourceKindReader: kindReader, ResourceInstanceReader: reader})
	kinds, err := session.ResourceKinds(context.Background())
	require.NoError(t, err)
	require.Len(t, kinds.Kinds, 1)
	assert.True(t, kinds.Kinds[0].Sensitive)
	listed, err := session.Resources(context.Background(), "worker-2", "MachineStatuses.runtime.talos.dev")
	require.NoError(t, err)
	assert.Equal(t, "worker-2", gotNode)
	assert.Equal(t, "MachineStatuses.runtime.talos.dev", gotKind)
	assert.Equal(t, "machine-a", listed.Instances[0].ID)
	resource, err := session.Resource(context.Background(), "worker-2", "MachineStatuses.runtime.talos.dev", "machine-a")
	require.NoError(t, err)
	assert.Equal(t, "worker-2", gotNode)
	assert.Equal(t, "MachineStatuses.runtime.talos.dev", gotKind)
	assert.Equal(t, "machine-a", gotID)
	assert.Equal(t, "metadata: complete", resource.YAML)
}

func TestResourceListDoesNotLoadYAML(t *testing.T) {
	getCalls := 0
	reader := &testkit.FakeResourceInstanceReader{
		ListFunc: func(context.Context, string, string) (domain.ResourceInstanceSet, error) {
			return domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{Namespace: "runtime", Type: "MachineStatuses.runtime.talos.dev", ID: "machine-a", Phase: "running"}}}, nil
		},
		GetFunc: func(context.Context, string, string, string) (domain.ResourceInstanceSnapshot, error) {
			getCalls++
			return domain.ResourceInstanceSnapshot{YAML: "should not be requested"}, nil
		},
	}
	session := openReadTestSession(t, &testkit.FakeSession{ResourceInstanceReader: reader})
	listed, err := session.Resources(context.Background(), "worker-2", "MachineStatuses.runtime.talos.dev")
	require.NoError(t, err)
	require.Len(t, listed.Instances, 1)
	assert.Equal(t, "running", listed.Instances[0].Phase)
	assert.Empty(t, listed.Instances[0].YAML)
	assert.Zero(t, getCalls)
}

func TestResourceReadCancellation(t *testing.T) {
	entered := make(chan struct{})
	adapter := &testkit.FakeSession{ResourceInstanceReader: &testkit.FakeResourceInstanceReader{
		ListFunc: func(ctx context.Context, node, kind string) (domain.ResourceInstanceSet, error) {
			assert.Equal(t, "worker-2", node)
			assert.Equal(t, "MachineStatuses.runtime.talos.dev", kind)
			close(entered)
			<-ctx.Done()
			return domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{ID: "partial"}}}, ctx.Err()
		},
	}}
	session := openReadTestSession(t, adapter)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := session.Resources(ctx, "worker-2", "MachineStatuses.runtime.talos.dev"); done <- err }()
	<-entered
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestMissingResourceCapabilityIsExplicit(t *testing.T) {
	session := openReadTestSession(t, &testkit.FakeSession{})
	_, err := session.ResourceKinds(context.Background())
	require.ErrorIs(t, err, application.ErrCapabilityUnavailable)
	_, err = session.Resources(context.Background(), "node", "kind")
	require.ErrorIs(t, err, application.ErrCapabilityUnavailable)
	_, err = session.Resource(context.Background(), "node", "kind", "id")
	require.ErrorIs(t, err, application.ErrCapabilityUnavailable)
}

func TestResourceReadsPreservePartialValues(t *testing.T) {
	readErr := errors.New("partial resource read")
	partial := domain.ResourceInstanceSnapshot{Namespace: "runtime", ID: "machine-a", Phase: "unknown", YAML: "partial yaml"}
	session := openReadTestSession(t, &testkit.FakeSession{ResourceInstanceReader: &testkit.FakeResourceInstanceReader{
		GetFunc: func(context.Context, string, string, string) (domain.ResourceInstanceSnapshot, error) {
			return partial, readErr
		},
	}})
	value, err := session.Resource(context.Background(), "node", "kind", "machine-a")
	require.ErrorIs(t, err, readErr)
	assert.Equal(t, partial, value)
}

var _ ports.ResourceInstanceReader = (*testkit.FakeResourceInstanceReader)(nil)
