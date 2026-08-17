package ports

import "context"

type ServiceController interface {
	Start(ctx context.Context, node, service string) error
	Stop(ctx context.Context, node, service string) error
	Restart(ctx context.Context, node, service string) error
}
