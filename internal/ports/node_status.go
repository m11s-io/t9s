package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type NodeStatusReader interface {
	List(context.Context, string) (domain.NodeStatusSnapshot, error)
}
