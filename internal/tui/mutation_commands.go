package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

// directMutationPrompt is frontend-owned and stores the concrete prepared
// backend plan. Its assessment is a snapshot used only to render the prompt.
type directMutationPrompt struct {
	operation   uint64
	generation  uint64
	node        *application.PreparedNodeAction
	service     *application.PreparedServiceAction
	assessment  application.Assessment
	session     *application.Session
	serviceKind application.ServiceActionKind
	nodeKind    application.ActionKind
	label       string
}

type directMutationState struct {
	prompt    *directMutationPrompt
	busy      bool
	operation uint64
	results   []application.TargetResult
	total     int
	notice    string
}

type directServicePrepared struct {
	operation, generation uint64
	plan                  *application.PreparedServiceAction
	request               application.ServiceActionRequest
	err                   error
}

type directNodePrepared struct {
	operation, generation uint64
	plan                  *application.PreparedNodeAction
	request               application.NodeActionRequest
	err                   error
}

type directMutationProgress struct {
	operation, generation uint64
	result                application.TargetResult
}

type directMutationFinished struct {
	operation, generation uint64
	result                application.MutationResult
	err                   error
}

type directMutationWork struct {
	operation, generation uint64
	plan                  *directMutationPrompt
	ctx                   context.Context
	session               *application.Session
	events                chan directMutationWorkItem
}

type directMutationWorkItem struct {
	result   *application.TargetResult
	finished *directMutationFinished
}

func directPrepareServiceCommand(session *application.Session, ctx context.Context, operation, generation uint64, request application.ServiceActionRequest) tea.Cmd {
	return func() tea.Msg {
		if ctx == nil || ctx.Err() != nil {
			return nil
		}
		plan, err := session.PrepareServiceAction(ctx, request)
		return directServicePrepared{operation: operation, generation: generation, plan: plan, request: request, err: err}
	}
}

func directPrepareNodeCommand(session *application.Session, ctx context.Context, operation, generation uint64, request application.NodeActionRequest) tea.Cmd {
	return func() tea.Msg {
		if ctx == nil || ctx.Err() != nil {
			return nil
		}
		plan, err := session.PrepareNodeAction(ctx, request)
		return directNodePrepared{operation: operation, generation: generation, plan: plan, request: request, err: err}
	}
}

func directMutationStartCommand(work directMutationWork) tea.Cmd {
	return func() tea.Msg {
		if work.ctx == nil || work.ctx.Err() != nil {
			return nil
		}
		go func() {
			plan := work.plan
			var result application.MutationResult
			var err error
			if plan.node != nil {
				result, err = work.session.ExecuteNodeAction(work.ctx, plan.node, application.Confirmation{Approved: true}, func(target application.TargetResult) {
					select {
					case work.events <- directMutationWorkItem{result: &target}:
					default:
					}
				})
			} else if plan.service != nil {
				result, err = work.session.ExecuteServiceAction(work.ctx, plan.service, application.Confirmation{Approved: true})
			}
			finished := directMutationFinished{operation: work.operation, generation: work.generation, result: result, err: err}
			work.events <- directMutationWorkItem{finished: &finished}
		}()
		return directMutationAwaitCommand(work)()
	}
}

func directMutationAwaitCommand(work directMutationWork) tea.Cmd {
	return func() tea.Msg {
		select {
		case item := <-work.events:
			if item.result != nil {
				return directMutationProgress{operation: work.operation, generation: work.generation, result: *item.result}
			}
			if item.finished != nil {
				return *item.finished
			}
			return nil
		}
	}
}

func mutationErrorMessage(err error) string {
	switch {
	case errors.Is(err, application.ErrAssessmentChanged):
		return "assessment changed — prepare the action again"
	case errors.Is(err, application.ErrActionBlocked):
		return "action blocked by current cluster state"
	case errors.Is(err, application.ErrWritesDisabled):
		return "writes are disabled for this session"
	case errors.Is(err, application.ErrMutationBusy):
		return "another mutation is active"
	case errors.Is(err, application.ErrConfirmationRequired):
		return "confirmation is required"
	case errors.Is(err, context.Canceled):
		return "request canceled; remote outcome may be unconfirmed"
	case err != nil:
		return "request failed; check cluster state before retrying"
	default:
		return ""
	}
}

func renderDirectMutationPrompt(prompt *directMutationPrompt) string {
	if prompt == nil {
		return ""
	}
	a := prompt.assessment
	if a.Blocked != "" {
		return "!! " + truncateWarningTail(a.Blocked, pendingActionWarningBudget) + " — action refused (n to cancel)"
	}
	verb, target := "Action", ""
	if prompt.service != nil {
		switch prompt.serviceKind {
		case application.ServiceActionStart:
			verb = "Start"
		case application.ServiceActionStop:
			verb = "Stop"
		case application.ServiceActionRestart:
			verb = "Restart"
		}
		target = prompt.label
	} else if prompt.node != nil {
		verb = "Reboot"
		if prompt.nodeKind == application.ActionShutdown {
			verb = "Shutdown"
		}
		if prompt.nodeKind == application.ActionRollback {
			verb = "Rollback"
		}
		target = strings.Join(a.Targets, ", ")
	}
	if a.Warning != "" {
		return "!! " + truncateWarningTail(a.Warning, pendingActionWarningBudget) + " — " + verb + " " + target + "? (y/n)"
	}
	return verb + " " + target + "? (y/n)"
}

func appendMutationResult(results []application.TargetResult, incoming application.TargetResult) []application.TargetResult {
	for _, current := range results {
		if current.Target == incoming.Target {
			return results
		}
	}
	return append(results, incoming)
}

func renderMutationResults(results []application.TargetResult, total int) string {
	accepted, completed, failed, unconfirmed := 0, 0, 0, 0
	details := make([]string, 0, len(results))
	for _, result := range results {
		status := "failed"
		switch result.Outcome {
		case domain.OperationAccepted:
			accepted++
			status = "accepted"
		case domain.OperationCompleted:
			completed++
			status = "completed"
		case domain.OperationUnconfirmed:
			unconfirmed++
			status = "unconfirmed; check state before retrying"
		default:
			failed++
		}
		details = append(details, result.Target+" "+status)
	}
	if total <= 0 {
		total = len(results)
	}
	parts := make([]string, 0, 3)
	if accepted > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d accepted; completion not verified", accepted, total))
	}
	if completed > 0 {
		parts = append(parts, fmt.Sprintf("%d completed", completed))
	}
	if unconfirmed > 0 {
		parts = append(parts, fmt.Sprintf("%d unconfirmed; check state before retrying", unconfirmed))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("0/%d results", total)
	}
	return strings.Join(parts, "; ") + " — " + strings.Join(details, ", ")
}
