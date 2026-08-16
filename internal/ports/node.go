package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type NodeReader interface {
	List(context.Context) (domain.NodeSet, error)
}

type RebootMode int

const (
	RebootDefault RebootMode = iota
	RebootPowercycle
)

type NodeController interface {
	Reboot(ctx context.Context, target string, mode RebootMode) error
	Shutdown(ctx context.Context, target string, force bool) error
}
