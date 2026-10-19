package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

func destructiveIntentInput() textinput.Model {
	input := textinput.New()
	input.Prompt = "TYPE :"
	input.CharLimit = 128
	_ = input.Focus()
	return input
}

func (m *directModel) prepareDirectNodeMutation(request application.NodeActionRequest) tea.Cmd {
	operation := m.nextMutationOperation()
	m.mutation.operation = operation
	m.mutation.busy = true
	m.mutation.notice = "Preparing mutation…"
	m.mutation.results = nil
	m.view.notice = ""
	return directPrepareNodeCommand(m.session, m.sessionCtx, operation, m.generation(), request)
}

func (m *directModel) updateDirectDestructivePrompt(message tea.KeyPressMsg) (tea.Cmd, bool) {
	prompt := m.mutation.prompt
	if prompt == nil || (prompt.nodeKind != application.ActionReset && prompt.nodeKind != application.ActionWipeDevice) {
		return nil, false
	}
	key := message.String()
	if prompt.assessment.Blocked != "" {
		if key == "n" || key == "N" || key == "esc" {
			m.mutation.prompt = nil
			m.view.notice = "Action canceled"
		}
		return nil, true
	}
	if prompt.intentReady {
		return nil, false
	}
	fields := 2
	if prompt.nodeKind == application.ActionReset {
		fields = 4
	}
	if (key == "n" || key == "N") && prompt.field != fields-1 {
		m.mutation.prompt = nil
		m.view.notice = "Action canceled"
		return nil, true
	}
	switch key {
	case "tab":
		prompt.field = (prompt.field + 1) % fields
		if prompt.field == fields-1 {
			_ = prompt.intent.Focus()
		} else {
			prompt.intent.Blur()
		}
		return nil, true
	case "shift+tab":
		prompt.field = (prompt.field + fields - 1) % fields
		if prompt.field == fields-1 {
			_ = prompt.intent.Focus()
		} else {
			prompt.intent.Blur()
		}
		return nil, true
	case "esc":
		m.mutation.prompt = nil
		m.view.notice = "Action canceled"
		return nil, true
	case "enter":
		if prompt.needsPrepare {
			request := application.NodeActionRequest{Kind: prompt.nodeKind, Targets: append([]string(nil), prompt.assessment.Targets...)}
			if prompt.reset != nil {
				options := *prompt.reset
				options.UserDisks = append([]string(nil), options.UserDisks...)
				options.SystemPartitions = append([]string(nil), options.SystemPartitions...)
				request.Reset = &options
			}
			if prompt.deviceWipe != nil {
				options := *prompt.deviceWipe
				request.DeviceWipe = &options
			}
			m.mutation.prompt = nil
			m.mutation.busy = true
			m.mutation.notice = "Preparing mutation…"
			return directPrepareNodeCommand(prompt.session, m.sessionCtx, prompt.operation, prompt.generation, request), true
		}
		if prompt.intent.Value() != prompt.assessment.RequiredToken || prompt.assessment.RequiredToken == "" {
			prompt.err = "typed confirmation does not match the prepared assessment"
			return nil, true
		}
		prompt.intentReady = true
		prompt.err = ""
		prompt.confirm = application.Confirmation{Approved: true, Token: prompt.intent.Value()}
		prompt.intent.Blur()
		m.view.notice = ""
		return nil, true
	}
	changed := false
	if prompt.nodeKind == application.ActionReset && prompt.reset != nil {
		switch prompt.field {
		case 0:
			switch key {
			case " ", "space", "right", "l":
				prompt.reset.Mode = (prompt.reset.Mode + 1) % ports.WipeMode(3)
				changed = true
			case "left", "h":
				prompt.reset.Mode = (prompt.reset.Mode + ports.WipeMode(2)) % ports.WipeMode(3)
				changed = true
			}
		case 1:
			if key == " " || key == "space" {
				prompt.reset.Graceful = !prompt.reset.Graceful
				changed = true
			}
		case 2:
			if key == " " || key == "space" {
				prompt.reset.Reboot = !prompt.reset.Reboot
				changed = true
			}
		case 3:
			var command tea.Cmd
			prompt.intent, command = prompt.intent.Update(message)
			return command, true
		}
	} else if prompt.nodeKind == application.ActionWipeDevice && prompt.deviceWipe != nil {
		if prompt.field == 0 {
			if key == " " || key == "space" || key == "right" || key == "left" {
				if prompt.deviceWipe.Method == ports.DeviceWipeFast {
					prompt.deviceWipe.Method = ports.DeviceWipeZeroes
				} else {
					prompt.deviceWipe.Method = ports.DeviceWipeFast
				}
				changed = true
			}
		} else {
			var command tea.Cmd
			prompt.intent, command = prompt.intent.Update(message)
			return command, true
		}
	}
	if changed {
		prompt.needsPrepare = true
		prompt.intentReady = false
		prompt.intent.Reset()
	}
	return nil, true
}

// directMutationPrompt is frontend-owned and stores the concrete prepared
// backend plan. Its assessment is a snapshot used only to render the prompt.
type directMutationPrompt struct {
	operation    uint64
	generation   uint64
	node         *application.PreparedNodeAction
	service      *application.PreparedServiceAction
	assessment   application.Assessment
	session      *application.Session
	serviceKind  application.ServiceActionKind
	nodeKind     application.ActionKind
	label        string
	reset        *ports.ResetOptions
	deviceWipe   *ports.DeviceWipeOptions
	intent       textinput.Model
	intentReady  bool
	confirm      application.Confirmation
	needsPrepare bool
	field        int
	err          string
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
				result, err = work.session.ExecuteNodeAction(work.ctx, plan.node, plan.confirm, func(target application.TargetResult) {
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
	destructive := prompt.nodeKind == application.ActionReset || prompt.nodeKind == application.ActionWipeDevice
	if a.Blocked != "" && !destructive {
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
		if prompt.nodeKind == application.ActionReset {
			verb = "Reset"
		}
		if prompt.nodeKind == application.ActionWipeDevice {
			verb = "Wipe device"
			target = prompt.deviceWipe.Device + " on " + prompt.deviceWipe.Node
		}
		target = strings.Join(a.Targets, ", ")
	}
	if prompt.nodeKind == application.ActionWipeDevice && prompt.deviceWipe != nil {
		target = prompt.deviceWipe.Device + " on " + prompt.deviceWipe.Node
	}
	if prompt.nodeKind == application.ActionReset || prompt.nodeKind == application.ActionWipeDevice {
		if a.Blocked != "" {
			return "!! " + truncateWarningTail(a.Blocked, pendingActionWarningBudget) + " — action refused (n to cancel)"
		}
		if prompt.intentReady {
			return verb + " " + target + "? (y/n)"
		}
		return verb + " " + target + " · complete typed intent in action panel"
	}
	if a.Warning != "" {
		return "!! " + truncateWarningTail(a.Warning, pendingActionWarningBudget) + " — " + verb + " " + target + "? (y/n)"
	}
	return verb + " " + target + "? (y/n)"
}

func renderDirectMutationOverlay(prompt *directMutationPrompt) string {
	var lines []string
	if prompt.nodeKind == application.ActionReset && prompt.reset != nil {
		mode := []string{"ALL", "SYSTEM_DISK", "USER_DISKS"}
		label := "UNKNOWN"
		if int(prompt.reset.Mode) < len(mode) {
			label = mode[prompt.reset.Mode]
		}
		modeMark, gracefulMark, rebootMark := " ", " ", " "
		if !prompt.intentReady {
			switch prompt.field {
			case 0:
				modeMark = "*"
			case 1:
				gracefulMark = "*"
			case 2:
				rebootMark = "*"
			}
		}
		lines = append(lines, "TARGETS  "+strings.Join(prompt.assessment.Targets, ", "))
		lines = append(lines, fmt.Sprintf("MODE%s      %s", modeMark, label))
		lines = append(lines, fmt.Sprintf("GRACEFUL%s  %s (leave etcd before reset)", gracefulMark, boolLabel(prompt.reset.Graceful)))
		lines = append(lines, fmt.Sprintf("REBOOT%s    %s (off halts after reset)", rebootMark, boolLabel(prompt.reset.Reboot)))
		lines = append(lines, "")
		if len(prompt.assessment.ResetPreview) == 0 {
			lines = append(lines, "DISKS  inventory unknown")
		}
		for _, preview := range prompt.assessment.ResetPreview {
			if !preview.Known {
				lines = append(lines, "DISKS  "+sanitizeUntrustedText(preview.Node)+": inventory unknown")
				continue
			}
			system := preview.SystemDisk
			if system == "" {
				system = "unknown"
			}
			users := strings.Join(preview.UserDisks, ", ")
			if users == "" {
				users = "none reported"
			}
			lines = append(lines, "DISKS  "+sanitizeUntrustedText(preview.Node)+": system "+sanitizeUntrustedText(system)+" · user "+sanitizeUntrustedText(users))
		}
	} else if prompt.deviceWipe != nil {
		method := "fast"
		if prompt.deviceWipe.Method == ports.DeviceWipeZeroes {
			method = "zeroes"
		}
		mark := " "
		if !prompt.intentReady && prompt.field == 0 {
			mark = "*"
		}
		lines = append(lines, "NODE    "+sanitizeUntrustedText(prompt.deviceWipe.Node))
		lines = append(lines, "DEVICE  "+sanitizeUntrustedText(prompt.deviceWipe.Device)+" (selected from visible inventory)")
		lines = append(lines, "METHOD"+mark+" "+method+" (zeroes writes across the whole device)")
	}
	if prompt.assessment.Warning != "" {
		lines = append(lines, "WARNING: "+truncateWarningTail(prompt.assessment.Warning, pendingActionWarningBudget))
	}
	if prompt.assessment.Blocked != "" {
		lines = append(lines, "BLOCKED: "+truncateWarningTail(prompt.assessment.Blocked, pendingActionWarningBudget))
		lines = append(lines, "Action refused. Press n or Esc to cancel.")
	} else if prompt.intentReady {
		lines = append(lines, "", "Prepared assessment accepted. Press y to execute or n/Esc to cancel.")
	} else {
		lines = append(lines, "", "Type \""+sanitizeUntrustedText(prompt.assessment.RequiredToken)+"\" and press Enter. Then confirm with y/n.")
		if prompt.nodeKind == application.ActionReset {
			lines = append(lines, "Tab moves between reset options and typed intent; Space or arrows adjust options.")
		} else {
			lines = append(lines, "Tab selects wipe method or typed intent; Space changes method.")
		}
		lines = append(lines, prompt.intent.View())
	}
	if prompt.err != "" {
		lines = append(lines, "ERROR: "+prompt.err)
	}
	return strings.Join(lines, "\n")
}

func boolLabel(value bool) string {
	if value {
		return "on"
	}
	return "off"
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
