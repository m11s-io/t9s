package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type DiskReader interface {
	List(ctx context.Context, node string) (domain.DiskSet, error)
}
