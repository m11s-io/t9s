package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type NodeReader interface {
	List(context.Context) (domain.NodeSet, error)
}
