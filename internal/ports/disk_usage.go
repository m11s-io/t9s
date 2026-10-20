package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type DiskUsageReader interface {
	Open(context.Context, domain.DiskUsageRequest) (DiskUsageStream, error)
}

type DiskUsageStream interface {
	Next(context.Context) (domain.DiskUsageBatch, error)
	Close() error
}
