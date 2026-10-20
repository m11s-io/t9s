package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
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
	request  directRequestStamp
	nodes    domain.NodeSet
	services domain.ServiceSet
	err      error
}

type directEventsLoaded struct {
	request directRequestStamp
	events  domain.EventSet
	err     error
}

type directEventFollowOpened struct {
	request directRequestStamp
	stream  ports.EventStream
	err     error
}

type directEventFollowBatch struct {
	request directRequestStamp
	batch   domain.EventSet
	err     error
}

type directClusterHealthOpened struct {
	request directRequestStamp
	stream  ports.ClusterHealthStream
	err     error
}

type directClusterHealthBatch struct {
	request  directRequestStamp
	progress domain.ClusterHealthProgress
	err      error
}

type directEtcdLoaded struct {
	request directRequestStamp
	etcd    domain.EtcdSet
	err     error
}

type directKubernetesNodesLoaded struct {
	request directRequestStamp
	nodes   map[string]domain.KubernetesNodeSnapshot
	err     error
}

type directNodeStatusesLoaded struct {
	request directRequestStamp
	values  domain.NodeStatusSet
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
		nodes, services, err := session.NodeServices(ctx)
		return directNodeServicesLoaded{request: request.stamp, nodes: nodes, services: services, err: err}
	})
}

func directEventsCommand(session *application.Session, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		events, err := session.Events(ctx)
		return directEventsLoaded{request: request.stamp, events: events, err: err}
	})
}

func directEtcdCommand(session *application.Session, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		etcd, err := session.Etcd(ctx)
		return directEtcdLoaded{request: request.stamp, etcd: etcd, err: err}
	})
}

func directKubernetesNodesCommand(session *application.Session, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		nodes, err := session.KubernetesNodes(ctx)
		return directKubernetesNodesLoaded{request: request.stamp, nodes: nodes, err: err}
	})
}

func directNodeStatusesCommand(session *application.Session, request directRequest, nodes []domain.NodeSnapshot) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.NodeStatuses(ctx, nodes)
		return directNodeStatusesLoaded{request: request.stamp, values: values, err: err}
	})
}
