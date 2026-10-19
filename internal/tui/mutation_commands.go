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
	fields := 2
	if prompt.nodeKind == application.ActionReset {
		fields = 4
		if message.Code == tea.KeyPgDown || message.Code == tea.KeyPgUp {
			pageSize := m.destructivePreviewPageSize()
			maxOffset := max(0, len(resetPreviewRows(prompt.assessment.ResetPreview))-pageSize)
			if message.Code == tea.KeyPgDown {
				prompt.previewOffset = min(maxOffset, prompt.previewOffset+pageSize)
			} else {
				prompt.previewOffset = max(0, prompt.previewOffset-pageSize)
			}
			return nil, true
		}
	}
	if prompt.intentReady {
		return nil, false
	}
	if (key == "n" || key == "N") && (prompt.field != fields-1 || prompt.assessment.Blocked != "") {
		m.mutation.prompt = nil
		m.view.notice = "Action canceled"
		return nil, true
	}
	if prompt.assessment.Blocked != "" && prompt.field == fields-1 && key != "tab" && key != "shift+tab" && key != "enter" && key != "esc" {
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
		if prompt.assessment.Blocked != "" {
			return nil, true
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

func (m *directModel) destructivePreviewPageSize() int {
	layout := layoutShell(m.width, m.height)
	innerHeight := layoutResourceFrame(layout.Width, layout.ContentHeight, "reset").InnerHeight
	return max(1, innerHeight-7)
}

func resetPreviewRows(previews []application.ResetPreview) []string {
	rows := make([]string, 0, len(previews)*2)
	for _, preview := range previews {
		node := sanitizeUntrustedText(preview.Node)
		if !preview.Known {
			rows = append(rows, "DISKS  "+node+": inventory unknown")
			continue
		}
		system := preview.SystemDisk
		if system == "" {
			system = "unknown"
		}
		rows = append(rows, "DISKS  "+node+": system "+sanitizeUntrustedText(system))
		if len(preview.UserDisks) == 0 {
			rows = append(rows, "DISKS  "+node+": user disks none reported")
			continue
		}
		for _, device := range preview.UserDisks {
			rows = append(rows, "DISKS  "+node+": user "+sanitizeUntrustedText(device))
		}
	}
	return rows
}

// directMutationPrompt is frontend-owned and stores the concrete prepared
// backend plan. Its assessment is a snapshot used only to render the prompt.
type directMutationPrompt struct {
	operation     uint64
	generation    uint64
	node          *application.PreparedNodeAction
	service       *application.PreparedServiceAction
	etcd          *application.PreparedEtcdAction
	etcdKind      application.EtcdActionKind
	assessment    application.Assessment
	session       *application.Session
	serviceKind   application.ServiceActionKind
	nodeKind      application.ActionKind
	label         string
	reset         *ports.ResetOptions
	deviceWipe    *ports.DeviceWipeOptions
	intent        textinput.Model
	intentReady   bool
	confirm       application.Confirmation
	needsPrepare  bool
	field         int
	previewOffset int
	err           string
}

type directMutationState struct {
	prompt    *directMutationPrompt
	busy      bool
	operation uint64
	results   []application.TargetResult
	snapshot  *domain.EtcdSnapshotResult
	total     int
	notice    string
}

func cloneEtcdSnapshotResult(result *domain.EtcdSnapshotResult) *domain.EtcdSnapshotResult {
	if result == nil {
		return nil
	}
	copy := *result
	return &copy
}

type directServicePrepared struct {
	operation, generation uint64
	plan                  *application.PreparedServiceAction
	request               application.ServiceActionRequest
	err                   error
}

type directEtcdPrepared struct {
	operation, generation uint64
	plan                  *application.PreparedEtcdAction
	request               application.EtcdActionRequest
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

func directPrepareEtcdCommand(session *application.Session, ctx context.Context, operation, generation uint64, request application.EtcdActionRequest) tea.Cmd {
	return func() tea.Msg {
		if ctx == nil || ctx.Err() != nil {
			return nil
		}
		plan, err := session.PrepareEtcdAction(ctx, request)
		return directEtcdPrepared{operation: operation, generation: generation, plan: plan, request: request, err: err}
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
			} else if plan.etcd != nil {
				result, err = work.session.ExecuteEtcdAction(work.ctx, plan.etcd, plan.confirm)
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
	if prompt.etcd != nil {
		verb, target := etcdActionLabel(prompt.etcdKind), strings.Join(prompt.assessment.Targets, ", ")
		if prompt.assessment.SnapshotPath != "" {
			verb += "; snapshot to " + sanitizeUntrustedText(prompt.assessment.SnapshotPath)
		}
		if prompt.etcdKind == application.EtcdActionSnapshot || prompt.etcdKind == application.EtcdActionRemoveMember || prompt.etcdKind == application.EtcdActionLeaveCluster {
			if prompt.assessment.Blocked != "" {
				return "!! " + truncateWarningTail(prompt.assessment.Blocked, pendingActionWarningBudget) + " — action refused (n to cancel)"
			}
		}
		if prompt.assessment.Warning != "" {
			return "!! " + truncateWarningTail(prompt.assessment.Warning, pendingActionWarningBudget) + " — " + verb + " " + target + "? (y/n)"
		}
		return verb + " " + target + "? (y/n)"
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

func etcdActionLabel(kind application.EtcdActionKind) string {
	switch kind {
	case application.EtcdActionSnapshot:
		return "Snapshot"
	case application.EtcdActionDefragment:
		return "Defragment"
	case application.EtcdActionDisarmAlarms:
		return "Disarm alarms"
	case application.EtcdActionRemoveMember:
		return "Remove member"
	case application.EtcdActionLeaveCluster:
		return "Leave cluster"
	case application.EtcdActionForfeitLeadership:
		return "Forfeit leadership"
	default:
		return "Etcd action"
	}
}

func renderDirectMutationOverlay(prompt *directMutationPrompt, innerHeight int) string {
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
		rows := resetPreviewRows(prompt.assessment.ResetPreview)
		if len(rows) == 0 {
			rows = []string{"DISKS  inventory unknown"}
		}
		pageSize := max(1, innerHeight-7)
		maxOffset := max(0, len(rows)-pageSize)
		offset := min(maxOffset, max(0, prompt.previewOffset))
		pageCount := max(1, (len(rows)+pageSize-1)/pageSize)
		page := min(pageCount, (offset+pageSize-1)/pageSize+1)
		lines = append(lines, fmt.Sprintf("TARGETS %d · SCOPES Page %d/%d (PgUp/PgDn)", len(prompt.assessment.Targets), page, pageCount))
		lines = append(lines, fmt.Sprintf("MODE%s %s · GRACEFUL%s %s · REBOOT%s %s", modeMark, label, gracefulMark, boolLabel(prompt.reset.Graceful), rebootMark, boolLabel(prompt.reset.Reboot)))
		end := min(len(rows), offset+pageSize)
		lines = append(lines, rows[offset:end]...)
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
	status := ""
	if prompt.assessment.Blocked != "" {
		status = "BLOCKED: " + truncateWarningTail(prompt.assessment.Blocked, pendingActionWarningBudget)
	} else if prompt.assessment.Warning != "" {
		status = "WARNING: " + truncateWarningTail(prompt.assessment.Warning, pendingActionWarningBudget)
	}
	if prompt.err != "" {
		status = "ERROR: " + prompt.err
	}
	if prompt.nodeKind == application.ActionReset {
		lines = append(lines, "STATUS  "+status)
		lines = append(lines, "Type \""+sanitizeUntrustedText(prompt.assessment.RequiredToken)+"\" then press Enter")
		lines = append(lines, "Tab fields · Space/arrows options · PgUp/PgDn scopes")
		lines = append(lines, prompt.intent.View())
		if prompt.assessment.Blocked != "" {
			lines = append(lines, "Approval unavailable while blocked; revise options and prepare")
		} else if prompt.intentReady {
			lines = append(lines, "Prepared assessment accepted. Press y to execute or n/Esc to cancel.")
		} else {
			lines = append(lines, "After token Enter, then confirm with y/n")
		}
	} else {
		if status != "" {
			lines = append(lines, status)
		}
		if prompt.assessment.Blocked != "" {
			lines = append(lines, "Action refused. Press n or Esc to cancel.")
		} else if prompt.intentReady {
			lines = append(lines, "", "Prepared assessment accepted. Press y to execute or n/Esc to cancel.")
		} else {
			lines = append(lines, "", "Type \""+sanitizeUntrustedText(prompt.assessment.RequiredToken)+"\" and press Enter. Then confirm with y/n.")
			lines = append(lines, "Tab selects wipe method or typed intent; Space changes method.")
			lines = append(lines, prompt.intent.View())
		}
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

func renderEtcdMutationSummary(results []application.TargetResult, total int, snapshot *domain.EtcdSnapshotResult, failure string) string {
	parts := make([]string, 0, 3)
	if len(results) > 0 {
		parts = append(parts, renderMutationResults(results, total))
	}
	if snapshot != nil {
		path := sanitizeUntrustedText(snapshot.Path)
		status := "completed"
		for _, result := range results {
			if result.Target == snapshot.Node && result.Outcome == domain.OperationUnconfirmed {
				status = "unconfirmed; verify the snapshot file"
				break
			}
		}
		metadata := "snapshot " + status + ": " + path
		if snapshot.Size > 0 {
			metadata += " (" + formatBytes(snapshot.Size) + ")"
		}
		if snapshot.SHA256 != "" {
			metadata += " · SHA256 " + sanitizeUntrustedText(snapshot.SHA256)
		}
		parts = append(parts, metadata)
	}
	if failure != "" {
		parts = append(parts, failure)
	}
	return strings.Join(parts, "; ")
}
