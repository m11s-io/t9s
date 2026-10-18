package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type directContextsLoaded struct {
	request  directRequestStamp
	contexts []domain.ClusterContext
	err      error
}

type directSessionOpened struct {
	request     directRequestStamp
	contextName string
	session     *application.Session
	err         error
}

type directNodeServicesLoaded struct {
	request directRequestStamp
	nodes   domain.NodeSet
	err     error
}

type directSelectContext struct{ name string }

func directCommand(ctx context.Context, run func(context.Context) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if ctx == nil || ctx.Err() != nil {
			return nil
		}
		return run(ctx)
	}
}

func directContextsCommand(client *application.Client, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		contexts, err := client.Contexts(ctx)
		return directContextsLoaded{request: request.stamp, contexts: contexts, err: err}
	})
}

func directOpenSessionCommand(client *application.Client, request directRequest, contextName string, options application.Options) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		session, err := client.Open(ctx, contextName, options)
		if ctx.Err() != nil {
			if session != nil {
				_ = session.Close()
			}
			return directSessionOpened{request: request.stamp, contextName: contextName, err: ctx.Err()}
		}
		return directSessionOpened{request: request.stamp, contextName: contextName, session: session, err: err}
	})
}

func directNodeServicesCommand(session *application.Session, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		nodes, _, err := session.NodeServices(ctx)
		return directNodeServicesLoaded{request: request.stamp, nodes: nodes, err: err}
	})
}
