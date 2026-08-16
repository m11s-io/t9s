package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type NetworkReader interface {
	List(ctx context.Context, node string) (domain.NetworkSet, error)
}
