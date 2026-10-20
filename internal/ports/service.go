package ports

import (
	"context"
	"github.com/m11s-io/t9s/internal/domain"
)

type ServiceReader interface {
	List(context.Context) (domain.ServiceSet, error)
	Detail(context.Context, string, string) (domain.ServiceDetail, error)
}
