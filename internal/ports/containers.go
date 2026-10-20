package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type ContainerReader interface {
	List(context.Context, domain.ContainerRequest) (domain.ContainerSet, error)
	OpenImages(context.Context, domain.ImageRequest) (ImageStream, error)
}

type ImageStream interface {
	Next(context.Context) (domain.ImageBatch, error)
	Close() error
}
