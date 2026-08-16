package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type KubernetesNodeReader interface {
	List(ctx context.Context) (map[string]domain.KubernetesNodeSnapshot, error)
}

type KubernetesResolver interface {
	Resolve(ctx context.Context, talosContext string) (KubernetesNodeReader, error)
}
