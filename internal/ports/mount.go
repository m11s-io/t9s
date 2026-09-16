package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type MountReader interface {
	List(ctx context.Context, node string) (domain.MountSet, error)
}
