package talos

import (
	"context"
	"fmt"
	"sort"

	cosiresource "github.com/cosi-project/runtime/pkg/resource"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"gopkg.in/yaml.v3"
)

type resourceInstanceClient interface {
	Resolve(ctx context.Context, kindType string) (namespace string, canonicalType string, err error)
	List(ctx context.Context, node, namespace, canonicalType string) ([]cosiresource.Resource, error)
	Get(ctx context.Context, node, namespace, canonicalType, id string) (cosiresource.Resource, error)
}

type machineryResourceInstanceClient struct{ client *talosclient.Client }

func (c machineryResourceInstanceClient) Resolve(ctx context.Context, kindType string) (string, string, error) {
	namespace := cosiresource.Namespace("")
	definition, err := c.client.ResolveResourceKind(ctx, &namespace, cosiresource.Type(kindType))
	if err != nil {
		return "", "", err
	}
	return string(namespace), definition.Metadata().ID(), nil
}

func (c machineryResourceInstanceClient) List(ctx context.Context, node, namespace, canonicalType string) ([]cosiresource.Resource, error) {
	if node != "" {
		ctx = talosclient.WithNode(ctx, node)
	}
	list, err := c.client.COSI.List(ctx, cosiresource.NewMetadata(cosiresource.Namespace(namespace), cosiresource.Type(canonicalType), "", cosiresource.VersionUndefined))
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (c machineryResourceInstanceClient) Get(ctx context.Context, node, namespace, canonicalType, id string) (cosiresource.Resource, error) {
	if node != "" {
		ctx = talosclient.WithNode(ctx, node)
	}
	return c.client.COSI.Get(ctx, cosiresource.NewMetadata(cosiresource.Namespace(namespace), cosiresource.Type(canonicalType), cosiresource.ID(id), cosiresource.VersionUndefined))
}

type resourceInstanceReader struct {
	client resourceInstanceClient
}

func newResourceInstanceReader(client resourceInstanceClient) ports.ResourceInstanceReader {
	return &resourceInstanceReader{client: client}
}

func convertResourceInstance(r cosiresource.Resource) (domain.ResourceInstanceSnapshot, error) {
	marshaled, err := cosiresource.MarshalYAML(r)
	if err != nil {
		return domain.ResourceInstanceSnapshot{}, fmt.Errorf("marshal resource yaml: %w", err)
	}
	rendered, err := yaml.Marshal(marshaled)
	if err != nil {
		return domain.ResourceInstanceSnapshot{}, fmt.Errorf("encode resource yaml: %w", err)
	}
	metadata := r.Metadata()
	return domain.ResourceInstanceSnapshot{
		Namespace: string(metadata.Namespace()),
		Type:      string(metadata.Type()),
		ID:        string(metadata.ID()),
		Version:   metadata.Version().String(),
		Phase:     metadata.Phase().String(),
		YAML:      string(rendered),
	}, nil
}

func (r *resourceInstanceReader) List(ctx context.Context, node, kindType string) (domain.ResourceInstanceSet, error) {
	namespace, canonicalType, err := r.client.Resolve(ctx, kindType)
	if err != nil {
		return domain.ResourceInstanceSet{}, fmt.Errorf("resolve resource kind %q: %w", kindType, err)
	}
	items, err := r.client.List(ctx, node, namespace, canonicalType)
	if err != nil {
		return domain.ResourceInstanceSet{}, fmt.Errorf("list resource instances: %w", err)
	}

	instances := make([]domain.ResourceInstanceSnapshot, len(items))
	for index, item := range items {
		instance, err := convertResourceInstance(item)
		if err != nil {
			return domain.ResourceInstanceSet{}, err
		}
		instances[index] = instance
	}
	sort.SliceStable(instances, func(i, j int) bool {
		return instances[i].ID < instances[j].ID
	})

	return domain.ResourceInstanceSet{Instances: instances}, nil
}

func (r *resourceInstanceReader) Get(ctx context.Context, node, kindType, id string) (domain.ResourceInstanceSnapshot, error) {
	namespace, canonicalType, err := r.client.Resolve(ctx, kindType)
	if err != nil {
		return domain.ResourceInstanceSnapshot{}, fmt.Errorf("resolve resource kind %q: %w", kindType, err)
	}
	item, err := r.client.Get(ctx, node, namespace, canonicalType, id)
	if err != nil {
		return domain.ResourceInstanceSnapshot{}, fmt.Errorf("get resource instance: %w", err)
	}
	return convertResourceInstance(item)
}
