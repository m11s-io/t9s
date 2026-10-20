package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type directKubernetesUpgradePrepared struct {
	operation, generation uint64
	plan                  *application.PreparedKubernetesUpgrade
	err                   string
}
type directKubernetesUpgradeOpened struct {
	operation, generation uint64
	stream                ports.KubernetesUpgradeStream
	err                   string
}
type directKubernetesUpgradeEvent struct {
	operation, generation uint64
	event                 domain.KubernetesUpgradeEvent
}
type directKubernetesUpgradeFinished struct {
	operation, generation uint64
	result                domain.KubernetesUpgradeResult
	err                   string
}

func directKubernetesUpgradePrepareCommand(session *application.Session, request directRequest, operation, generation uint64, target string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		plan, err := session.PrepareKubernetesUpgrade(ctx, target)
		if err != nil {
			return directKubernetesUpgradePrepared{operation: operation, generation: generation, err: err.Error()}
		}
		return directKubernetesUpgradePrepared{operation: operation, generation: generation, plan: plan}
	})
}

func directKubernetesUpgradeStartCommand(session *application.Session, request directRequest, operation, generation uint64, plan *application.PreparedKubernetesUpgrade, confirmation application.Confirmation, snapshotPath string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		stream, err := session.ExecuteKubernetesUpgrade(ctx, plan, confirmation, snapshotPath)
		if err != nil {
			return directKubernetesUpgradeOpened{operation: operation, generation: generation, err: "Unable to start upgrade; review the session and prepared plan"}
		}
		return directKubernetesUpgradeOpened{operation: operation, generation: generation, stream: stream}
	})
}

func directKubernetesUpgradeAwaitCommand(stream ports.KubernetesUpgradeStream, operation, generation uint64) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-stream.Events()
		if ok {
			return directKubernetesUpgradeEvent{operation: operation, generation: generation, event: event}
		}
		result, err := stream.Wait(context.Background())
		message := ""
		if err != nil {
			message = "Upgrade ended with a partial or unconfirmed outcome; inspect the cluster before retrying"
		}
		return directKubernetesUpgradeFinished{operation: operation, generation: generation, result: result, err: message}
	}
}
