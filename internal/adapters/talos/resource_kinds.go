package talos

import (
	"context"
	"fmt"
	"sort"

	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

type resourceKindClient interface {
	Kinds(ctx context.Context) ([]*meta.ResourceDefinition, error)
}

type machineryResourceKindClient struct{ client *talosclient.Client }

func (c machineryResourceKindClient) Kinds(ctx context.Context) ([]*meta.ResourceDefinition, error) {
	list, err := safe.StateListAll[*meta.ResourceDefinition](ctx, c.client.COSI)
	if err != nil {
		return nil, err
	}
	var definitions []*meta.ResourceDefinition
	for definition := range list.All() {
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

type resourceKindReader struct {
	client resourceKindClient
}

func newResourceKindReader(client resourceKindClient) ports.ResourceKindReader {
	return &resourceKindReader{client: client}
}

func (r *resourceKindReader) List(ctx context.Context) (domain.ResourceKindSet, error) {
	definitions, err := r.client.Kinds(ctx)
	if err != nil {
		return domain.ResourceKindSet{}, fmt.Errorf("list resource kinds: %w", err)
	}

	kinds := make([]domain.ResourceKindSnapshot, len(definitions))
	for index, definition := range definitions {
		spec := definition.TypedSpec()
		kinds[index] = domain.ResourceKindSnapshot{
			Type:             spec.Type,
			DisplayType:      spec.DisplayType,
			DefaultNamespace: spec.DefaultNamespace,
			Aliases:          append([]string(nil), spec.Aliases...),
			Sensitive:        spec.Sensitivity != meta.NonSensitive,
		}
	}
	sort.SliceStable(kinds, func(i, j int) bool {
		return kinds[i].Type < kinds[j].Type
	})

	return domain.ResourceKindSet{Kinds: kinds}, nil
}
