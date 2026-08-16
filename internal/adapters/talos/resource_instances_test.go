package talos

import (
	"context"
	"errors"
	"testing"

	cosiresource "github.com/cosi-project/runtime/pkg/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubResource struct {
	metadata cosiresource.Metadata
	spec     any
}

func (r stubResource) Metadata() *cosiresource.Metadata { return &r.metadata }
func (r stubResource) Spec() any                        { return r.spec }
func (r stubResource) DeepCopy() cosiresource.Resource  { return r }

func newStubResource(namespace, typ, id string) stubResource {
	return stubResource{
		metadata: cosiresource.NewMetadata(cosiresource.Namespace(namespace), cosiresource.Type(typ), cosiresource.ID(id), cosiresource.VersionUndefined),
		spec:     map[string]string{"stage": "running"},
	}
}

type fakeResourceInstanceClient struct {
	namespace, canonicalType string
	resolveErr               error
	instances                []cosiresource.Resource
	listErr                  error
	instance                 cosiresource.Resource
	getErr                   error
}

func (c *fakeResourceInstanceClient) Resolve(context.Context, string) (string, string, error) {
	return c.namespace, c.canonicalType, c.resolveErr
}

func (c *fakeResourceInstanceClient) List(context.Context, string, string, string) ([]cosiresource.Resource, error) {
	return c.instances, c.listErr
}

func (c *fakeResourceInstanceClient) Get(context.Context, string, string, string, string) (cosiresource.Resource, error) {
	return c.instance, c.getErr
}

func TestResourceInstanceReaderListResolvesAndSortsByID(t *testing.T) {
	client := &fakeResourceInstanceClient{
		namespace: "runtime", canonicalType: "MachineStatuses.runtime.talos.dev",
		instances: []cosiresource.Resource{
			newStubResource("runtime", "MachineStatuses.runtime.talos.dev", "b"),
			newStubResource("runtime", "MachineStatuses.runtime.talos.dev", "a"),
		},
	}
	reader := newResourceInstanceReader(client)

	set, err := reader.List(t.Context(), "", "MachineStatus")

	require.NoError(t, err)
	require.Len(t, set.Instances, 2)
	assert.Equal(t, "a", set.Instances[0].ID)
	assert.Equal(t, "runtime", set.Instances[0].Namespace)
	assert.Contains(t, set.Instances[0].YAML, "stage")
	assert.Equal(t, "b", set.Instances[1].ID)
}

func TestResourceInstanceReaderListReturnsErrorWhenResolveFails(t *testing.T) {
	client := &fakeResourceInstanceClient{resolveErr: errors.New("not registered")}
	reader := newResourceInstanceReader(client)

	_, err := reader.List(t.Context(), "", "NotAType")

	assert.Error(t, err)
}

func TestResourceInstanceReaderGetReturnsYAML(t *testing.T) {
	client := &fakeResourceInstanceClient{
		namespace: "runtime", canonicalType: "MachineStatuses.runtime.talos.dev",
		instance: newStubResource("runtime", "MachineStatuses.runtime.talos.dev", "machine"),
	}
	reader := newResourceInstanceReader(client)

	instance, err := reader.Get(t.Context(), "cp-1", "MachineStatus", "machine")

	require.NoError(t, err)
	assert.Equal(t, "machine", instance.ID)
	assert.Equal(t, "runtime", instance.Namespace)
	assert.Contains(t, instance.YAML, "stage")
}
