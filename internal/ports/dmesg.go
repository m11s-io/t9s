package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type DmesgReader interface {
	Open(context.Context, domain.DmesgRequest) (DmesgStream, error)
}

type DmesgStream interface {
	Next(context.Context) (domain.DmesgBatch, error)
	Close() error
}
