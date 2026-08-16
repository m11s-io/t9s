package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type EtcdReader interface {
	List(ctx context.Context, controlPlaneNodes []string) (domain.EtcdSet, error)
}
