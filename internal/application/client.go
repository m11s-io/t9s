package application

import (
	"context"
	"errors"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

// Options are immutable session settings supplied by the trusted caller.
type Options struct {
	WritesEnabled bool
}

// Client owns the configured backend dependencies and opens independent sessions.
type Client struct {
	contextCatalog     ports.ContextCatalog
	sessionFactory     ports.SessionFactory
	kubernetesResolver ports.KubernetesResolver
}

func NewClient(dependencies Dependencies) *Client {
	return &Client{
		contextCatalog:     dependencies.ContextCatalog,
		sessionFactory:     dependencies.SessionFactory,
		kubernetesResolver: dependencies.KubernetesResolver,
	}
}

func (c *Client) Contexts(ctx context.Context) ([]domain.ClusterContext, error) {
	if c == nil || c.contextCatalog == nil {
		return nil, errors.New("context catalog is not configured")
	}
	return c.contextCatalog.List(ctx)
}

func (c *Client) Open(ctx context.Context, contextName string, options Options) (*Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || c.sessionFactory == nil {
		return nil, errors.New("session factory is not configured")
	}

	lifetimeCtx, lifetimeCancel := context.WithCancel(ctx)
	adapter, err := c.sessionFactory.Open(lifetimeCtx, contextName)
	if err != nil {
		if adapter != nil {
			closeAdapter(adapter)
		}
		lifetimeCancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		if adapter != nil {
			closeAdapter(adapter)
		}
		lifetimeCancel()
		return nil, err
	}
	if adapter == nil {
		lifetimeCancel()
		return nil, errors.New("session factory returned no session")
	}

	var kubernetesNodes ports.KubernetesNodeReader
	if c.kubernetesResolver != nil {
		kubernetesNodes, err = c.kubernetesResolver.Resolve(lifetimeCtx, contextName)
		if ctx.Err() != nil {
			closeAdapter(adapter)
			lifetimeCancel()
			return nil, ctx.Err()
		}
		if err != nil {
			kubernetesNodes = nil
		}
	}
	if err := ctx.Err(); err != nil {
		closeAdapter(adapter)
		lifetimeCancel()
		return nil, err
	}

	session := &Session{
		sdkSession:      adapter,
		kubernetesNodes: kubernetesNodes,
		lifetimeCtx:     lifetimeCtx,
		lifetimeCancel:  lifetimeCancel,
		writesEnabled:   options.WritesEnabled,
	}
	context.AfterFunc(lifetimeCtx, func() { _ = session.Close() })
	return session, nil
}

func closeAdapter(adapter ports.Session) {
	if adapter != nil {
		go func() { _ = adapter.Close() }()
	}
}
