package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type ClusterHealthReader interface {
	Open(context.Context, domain.ClusterHealthRequest) (ClusterHealthStream, error)
}

type ClusterHealthStream interface {
	Next(context.Context) (domain.ClusterHealthProgress, error)
	Close() error
}
