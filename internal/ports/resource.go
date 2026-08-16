package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type ResourceKindReader interface {
	List(ctx context.Context) (domain.ResourceKindSet, error)
}

type ResourceInstanceReader interface {
	List(ctx context.Context, node, kindType string) (domain.ResourceInstanceSet, error)
	Get(ctx context.Context, node, kindType, id string) (domain.ResourceInstanceSnapshot, error)
}
