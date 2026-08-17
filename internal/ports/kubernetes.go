package ports

import (
	"context"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
)

type KubernetesNodeReader interface {
	List(ctx context.Context) (map[string]domain.KubernetesNodeSnapshot, error)
}

type KubernetesResolver interface {
	Resolve(ctx context.Context, talosContext string) (KubernetesNodeReader, error)
}

type KubernetesMaintenance interface {
	CordonAndDrain(ctx context.Context, node string, timeout time.Duration, progress func(string)) error
	WaitReady(ctx context.Context, node string, timeout time.Duration) error
	Uncordon(ctx context.Context, node string, progress func(string)) error
}
