package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type directNodeProgress struct {
	request directRequestStamp
	ctx     context.Context
	updates <-chan tea.Msg
	nodes   domain.NodeSet
}

// Run the read outside Update; each message schedules the next receive, as in
// Bubble Tea's realtime example. Cancellation releases blocked sends/receives.
func directInitialNodeServicesCommand(session *application.Session, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		updates := make(chan tea.Msg)
		go func() {
			defer close(updates)
			nodes, services, err := session.NodeServicesProgress(ctx, func(nodes domain.NodeSet) {
				select {
				case updates <- directNodeProgress{request: request.stamp, ctx: ctx, updates: updates, nodes: nodes}:
				case <-ctx.Done():
				}
			})
			select {
			case updates <- directNodeServicesLoaded{request: request.stamp, nodes: nodes, services: services, err: err}:
			case <-ctx.Done():
			}
		}()
		return waitForNodeProgress(ctx, updates)()
	})
}

func waitForNodeProgress(ctx context.Context, updates <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		select {
		case message := <-updates:
			return message
		case <-ctx.Done():
			return nil
		}
	}
}
