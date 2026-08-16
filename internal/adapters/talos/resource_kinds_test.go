package talos

import (
	"context"
	"errors"
	"testing"

	cosiresource "github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/typed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeResourceKindClient struct {
	kinds []*meta.ResourceDefinition
	err   error
}

func (c *fakeResourceKindClient) Kinds(context.Context) ([]*meta.ResourceDefinition, error) {
	return c.kinds, c.err
}

// newTestResourceDefinition builds a *meta.ResourceDefinition directly via
// typed.NewResource, the same final construction step
// meta.NewResourceDefinition itself uses internally — but skips
// meta.NewResourceDefinition's own spec.Fill() call, which requires Type's
// name segment to be a real plural English word recognized by an external
// pluralization library and mutates DisplayType/Aliases as a side effect.
// This test only exercises resourceKindReader.List's field-by-field
// conversion, which reads TypedSpec() directly and never touches
// Metadata(), so an arbitrary, unvalidated spec is sufficient and avoids
// coupling this test's fixture data to a third-party pluralizer's word list.
func newTestResourceDefinition(typeName, displayType, namespace string, sensitive bool) *meta.ResourceDefinition {
	sensitivity := meta.NonSensitive
	if sensitive {
		sensitivity = meta.Sensitive
	}
	spec := meta.ResourceDefinitionSpec{
		Type:             typeName,
		DisplayType:      displayType,
		DefaultNamespace: namespace,
		Aliases:          []string{displayType},
		Sensitivity:      sensitivity,
	}
	return typed.NewResource[meta.ResourceDefinitionSpec, meta.ResourceDefinitionExtension](
		cosiresource.NewMetadata(meta.NamespaceName, meta.ResourceDefinitionType, typeName, cosiresource.VersionUndefined),
		spec,
	)
}

func TestResourceKindReaderListConvertsAndSortsByType(t *testing.T) {
	client := &fakeResourceKindClient{kinds: []*meta.ResourceDefinition{
		newTestResourceDefinition("ServiceDefinitions.v1alpha1.talos.dev", "Service", "runtime", false),
		newTestResourceDefinition("MachineStatuses.runtime.talos.dev", "MachineStatus", "runtime", false),
		newTestResourceDefinition("SecretsBundle.v1alpha1.talos.dev", "SecretsBundle", "controlplane", true),
	}}
	reader := newResourceKindReader(client)

	set, err := reader.List(t.Context())

	require.NoError(t, err)
	require.Len(t, set.Kinds, 3)
	assert.Equal(t, "MachineStatuses.runtime.talos.dev", set.Kinds[0].Type)
	assert.Equal(t, "MachineStatus", set.Kinds[0].DisplayType)
	assert.False(t, set.Kinds[0].Sensitive)
	assert.Equal(t, "SecretsBundle.v1alpha1.talos.dev", set.Kinds[1].Type)
	assert.True(t, set.Kinds[1].Sensitive)
}

func TestResourceKindReaderListReturnsErrorWhenClientFails(t *testing.T) {
	client := &fakeResourceKindClient{err: errors.New("unreachable")}
	reader := newResourceKindReader(client)

	_, err := reader.List(t.Context())

	assert.Error(t, err)
}
