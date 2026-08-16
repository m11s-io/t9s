package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type ServiceLogReader interface {
	Open(context.Context, domain.LogRequest) (ServiceLogStream, error)
}

type ServiceLogStream interface {
	Next(context.Context) (domain.LogBatch, error)
	Close() error
}
