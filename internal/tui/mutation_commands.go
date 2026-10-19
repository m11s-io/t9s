package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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

func (m *model) prepareDirectNodeMutation(request application.NodeActionRequest) tea.Cmd {
	m.mutation.upgrade = directUpgradeDisplayState{}
	m.mutation.upgradeResult = nil
	m.mutation.upgradeErr = nil
	operation := m.nextMutationOperation()
	m.mutation.operation = operation
	m.mutation.busy = true
	m.mutation.notice = "Preparing mutation…"
	m.mutation.results = nil
	m.view.notice = ""
	return directPrepareNodeCommand(m.session, m.sessionCtx, operation, m.generation(), request)
}

func (m *model) updateDirectDestructivePrompt(message tea.KeyPressMsg) (tea.Cmd, bool) {
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

func (m *model) destructivePreviewPageSize() int {
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
	operation          uint64
	generation         uint64
	node               *application.PreparedNodeAction
	service            *application.PreparedServiceAction
	etcd               *application.PreparedEtcdAction
	etcdKind           application.EtcdActionKind
	etcdMemberID       uint64
	etcdMemberHostname string
	assessment         application.Assessment
	session            *application.Session
	serviceKind        application.ServiceActionKind
	nodeKind           application.ActionKind
	label              string
	reset              *ports.ResetOptions
	deviceWipe         *ports.DeviceWipeOptions
	intent             textinput.Model
	intentReady        bool
	confirm            application.Confirmation
	needsPrepare       bool
	field              int
	previewOffset      int
	err                string
}

type directMutationState struct {
	prompt         *directMutationPrompt
	busy           bool
	operation      uint64
	results        []application.TargetResult
	snapshot       *domain.EtcdSnapshotResult
	etcdKind       application.EtcdActionKind
	receiptVisible bool
	receiptOffset  int
	total          int
	notice         string
	upgrade        directUpgradeDisplayState
	upgradeResult  *ports.UpgradeResult
	upgradeErr     error
	upgradePending bool
}

// directUpgradeDisplayState owns only presentation fields needed by the direct
// TUI. The complete backend terminal stays separately available in
// upgradeResult and upgradeErr.
type directUpgradeDisplayState struct {
	Active  bool
	Target  string
	Event   ports.UpgradeEvent
	Err     string
	Warning string
	Outcome domain.OperationOutcome
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
	memberID              uint64
	memberHostname        string
	err                   error
}

type directNodePrepared struct {
	operation, generation uint64
	plan                  *application.PreparedNodeAction
	request               application.NodeActionRequest
	err                   error
}

type directCurrentInstallImage struct {
	operation, generation uint64
	request               directRequestStamp
	target, image         string
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
	upgradeTarget         string
	upgrade               *ports.UpgradeResult
	upgradeStartErr       error
	upgradeWaitErr        error
}

type directUpgradeProgress struct {
	operation, generation uint64
	target                string
	event                 ports.UpgradeEvent
}

type directMutationCanceled struct {
	operation, generation uint64
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
	upgrade  *directUpgradeProgress
}

func directCurrentInstallImageCommand(session *application.Session, request directRequest, operation, generation uint64, target string) tea.Cmd {
	return func() tea.Msg {
		if request.ctx == nil || request.ctx.Err() != nil {
			return nil
		}
		image, err := session.CurrentInstallImage(request.ctx, target)
		return directCurrentInstallImage{operation: operation, generation: generation, request: request.stamp, target: target, image: image, err: err}
	}
}

func (m *model) beginDirectUpgrade(target string) tea.Cmd {
	operation := m.nextMutationOperation()
	m.mutation.operation = operation
	m.mutation.busy = true
	m.mutation.upgradePending = true
	m.mutation.notice = "Reading current install image…"
	m.mutation.results = nil
	m.mutation.upgrade = directUpgradeDisplayState{Target: target}
	m.mutation.upgradeResult = nil
	m.mutation.upgradeErr = nil
	m.view.notice = ""
	request, _ := m.requests.beginWithContext("upgrade-image", m.generation(), m.sessionCtx)
	return directCurrentInstallImageCommand(m.session, request, operation, m.generation(), target)
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

func directPrepareEtcdCommand(session *application.Session, ctx context.Context, operation, generation uint64, request application.EtcdActionRequest, member domain.EtcdMemberSnapshot) tea.Cmd {
	return func() tea.Msg {
		if ctx == nil || ctx.Err() != nil {
			return nil
		}
		plan, err := session.PrepareEtcdAction(ctx, request)
		return directEtcdPrepared{operation: operation, generation: generation, plan: plan, request: request, memberID: member.MemberID, memberHostname: member.Hostname, err: err}
	}
}

func directMutationStartCommand(work directMutationWork) tea.Cmd {
	return func() tea.Msg {
		if work.ctx == nil || work.ctx.Err() != nil {
			return nil
		}
		go func() {
			plan := work.plan
			if plan.node != nil && plan.nodeKind == application.ActionUpgrade {
				runDirectUpgrade(work, plan)
				return
			}
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
			sendDirectMutationItem(work, directMutationWorkItem{finished: &finished})
		}()
		return directMutationAwaitCommand(work)()
	}
}

func runDirectUpgrade(work directMutationWork, plan *directMutationPrompt) {
	target := strings.Join(plan.assessment.Targets, ", ")
	stream, startErr := work.session.ExecuteUpgrade(work.ctx, plan.node, plan.confirm)
	if startErr != nil || stream == nil {
		if startErr == nil {
			startErr = application.ErrCapabilityUnavailable
		}
		finished := directMutationFinished{operation: work.operation, generation: work.generation, upgradeTarget: target, upgradeStartErr: startErr}
		sendDirectMutationItem(work, directMutationWorkItem{finished: &finished})
		return
	}
	wait := make(chan directUpgradeWaitResult, 1)
	go func() {
		terminal, waitErr := stream.Wait(context.Background())
		wait <- directUpgradeWaitResult{result: terminal, err: waitErr}
	}()
	results := stream.Results()
	finish := func(got directUpgradeWaitResult) {
		finished := directUpgradeFinished(work, target, got)
		sendDirectMutationItem(work, directMutationWorkItem{finished: &finished})
	}
	for {
		select {
		case progress, ok := <-results:
			if !ok {
				finish(<-wait)
				return
			}
			if progress.Event != nil {
				event := *progress.Event
				item := directMutationWorkItem{upgrade: &directUpgradeProgress{operation: work.operation, generation: work.generation, target: target, event: event}}
				select {
				case work.events <- item:
				default:
				}
			}
		case got := <-wait:
			finish(got)
			return
		case <-work.ctx.Done():
			stream.Cancel()
			finish(<-wait)
			return
		}
	}
}

func directMutationAwaitCommand(work directMutationWork) tea.Cmd {
	return func() tea.Msg {
		ctx := work.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		select {
		case item := <-work.events:
			if item.result != nil {
				return directMutationProgress{operation: work.operation, generation: work.generation, result: *item.result}
			}
			if item.upgrade != nil {
				return *item.upgrade
			}
			if item.finished != nil {
				return *item.finished
			}
			return nil
		case <-ctx.Done():
			return directMutationCanceled{operation: work.operation, generation: work.generation}
		}
	}
}

type directUpgradeWaitResult struct {
	result ports.UpgradeResult
	err    error
}

func directUpgradeFinished(work directMutationWork, target string, got directUpgradeWaitResult) directMutationFinished {
	terminal := got.result
	return directMutationFinished{operation: work.operation, generation: work.generation, upgradeTarget: target, upgrade: &terminal, upgradeWaitErr: got.err}
}

func sendDirectMutationItem(work directMutationWork, item directMutationWorkItem) {
	select {
	case work.events <- item:
	case <-work.ctx.Done():
	}
}

func upgradeEventValue(event *ports.UpgradeEvent) ports.UpgradeEvent {
	if event == nil {
		return ports.UpgradeEvent{}
	}
	return *event
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

func directUpgradeAdmissionNotice(target string, err error) string {
	reason := "upgrade was not started"
	switch {
	case errors.Is(err, application.ErrAssessmentChanged):
		reason = "assessment changed — prepare the action again"
	case errors.Is(err, application.ErrActionBlocked):
		reason = "action blocked by current cluster state"
	case errors.Is(err, application.ErrWritesDisabled):
		reason = "writes are disabled for this session"
	case errors.Is(err, application.ErrMutationBusy):
		reason = "another mutation is active"
	case errors.Is(err, application.ErrConfirmationRequired):
		reason = "confirmation is required"
	case errors.Is(err, context.Canceled):
		reason = "request canceled before upgrade dispatch"
	}
	return "Upgrade " + sanitizeUntrustedText(fallback(target)) + " not started: " + reason
}

func renderDirectMutationPrompt(prompt *directMutationPrompt) string {
	if prompt == nil {
		return ""
	}
	if prompt.etcd != nil {
		if prompt.assessment.Blocked != "" {
			return "Action blocked · n cancel"
		}
		return "y confirm · n cancel"
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
		if prompt.nodeKind == application.ActionUpgrade {
			verb = "Upgrade to " + sanitizeUntrustedText(prompt.label)
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

func renderEtcdMutationSummary(results []application.TargetResult, kind application.EtcdActionKind, snapshot *domain.EtcdSnapshotResult, failure string) string {
	if len(results) == 0 {
		if snapshot != nil && kind == application.EtcdActionSnapshot {
			return "Snapshot completed"
		}
		if snapshot != nil {
			return etcdActionLabel(kind) + " failed after the snapshot; " + failure
		}
		return failure
	}
	result := results[len(results)-1]
	target := sanitizeUntrustedText(result.Target)
	var outcome string
	switch result.Outcome {
	case domain.OperationAccepted:
		outcome = "accepted; completion not verified"
	case domain.OperationCompleted:
		outcome = "completed"
	case domain.OperationUnconfirmed:
		outcome = "unconfirmed; check state before retrying"
	default:
		outcome = "failed"
	}
	summary := etcdActionLabel(kind) + " " + target + " " + outcome
	if result.Outcome != domain.OperationUnconfirmed && failure != "" {
		summary += "; " + failure
	}
	return summary
}

func etcdPanelLines(values []string, width int) []string {
	lines := make([]string, 0, len(values))
	for _, value := range values {
		value = sanitizeUntrustedText(value)
		if width <= 0 {
			lines = append(lines, value)
			continue
		}
		lines = append(lines, strings.Split(ansi.Hardwrap(value, width, true), "\n")...)
	}
	return lines
}

func renderDirectEtcdMutationOverlay(prompt *directMutationPrompt, width int) string {
	lines := []string{"ACTION: " + etcdActionLabel(prompt.etcdKind)}
	if prompt.etcdMemberHostname != "" {
		lines = append(lines, fmt.Sprintf("MEMBER: %s (ID %d)", prompt.etcdMemberHostname, prompt.etcdMemberID))
	} else if len(prompt.assessment.Targets) > 0 {
		lines = append(lines, "TARGET: "+strings.Join(prompt.assessment.Targets, ", "))
	}
	if prompt.assessment.Blocked != "" {
		lines = append(lines, "BLOCKED: "+prompt.assessment.Blocked)
	} else if prompt.assessment.Warning != "" {
		lines = append(lines, "WARNING: "+prompt.assessment.Warning)
	}
	if prompt.assessment.SnapshotPath != "" {
		lines = append(lines, "SNAPSHOT: "+prompt.assessment.SnapshotPath)
	}
	if prompt.assessment.Blocked != "" {
		lines = append(lines, "Action blocked · n/Esc cancel")
	} else {
		lines = append(lines, "y confirm · n/Esc cancel")
	}
	return strings.Join(etcdPanelLines(lines, width), "\n")
}

func etcdReceiptLines(state directMutationState, width int) []string {
	lines := []string{"RESULT: " + state.notice, "SNAPSHOT RECEIPT"}
	snapshot := state.snapshot
	status := "completed"
	for _, result := range state.results {
		if result.Target == snapshot.Node && result.Outcome == domain.OperationUnconfirmed {
			status = "unconfirmed; verify the snapshot file"
			break
		}
	}
	lines = append(lines,
		"Status: "+status,
		"Node: "+snapshot.Node,
		"Path: "+snapshot.Path,
		"Size: "+formatBytes(snapshot.Size),
		"SHA256: "+snapshot.SHA256,
	)
	return etcdPanelLines(lines, width)
}

func renderDirectEtcdReceiptOverlay(state directMutationState, width, height int) string {
	lines := etcdReceiptLines(state, width)
	if height <= 0 {
		return ""
	}
	pageSize := max(1, height-1)
	maxOffset := max(0, len(lines)-pageSize)
	offset := min(maxOffset, max(0, state.receiptOffset))
	end := min(len(lines), offset+pageSize)
	visible := append([]string(nil), lines[offset:end]...)
	if len(lines) > pageSize {
		visible = append(visible, fmt.Sprintf("Receipt %d-%d/%d · PgUp/PgDn", offset+1, end, len(lines)))
	}
	return strings.Join(visible, "\n")
}

func (m model) updateDirectMutationResult(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case directCurrentInstallImage:
		if message.generation != m.generation() || message.operation != m.mutation.operation || !m.requests.current(message.request) {
			return m, nil
		}
		m.requests.cancelSlot("upgrade-image")
		m.mutation.busy = false
		m.mutation.upgradePending = false
		if m.views.top().Kind != viewNodes {
			m.mutation.notice = ""
			return m, nil
		}
		prompt := newUpgradePromptModel(message.target, message.image)
		prompt.operation, prompt.generation = message.operation, message.generation
		m.palette = m.palette.close()
		m.upgradePrompt = &prompt
		if message.err != nil {
			m.upgradePrompt.notice = "current install image unavailable"
			m.view.notice = "current install image is unavailable; enter an image to continue"
		} else {
			m.view.notice = ""
		}
		return m, prompt.input.Focus()
	case directServicePrepared:
		if message.generation != m.generation() || message.operation != m.mutation.operation {
			return m, nil
		}
		m.mutation.busy = false
		if message.err != nil || message.plan == nil {
			m.view.notice = mutationErrorMessage(message.err)
			return m, nil
		}
		m.palette = m.palette.close()
		m.mutation.prompt = &directMutationPrompt{operation: message.operation, generation: message.generation, service: message.plan, serviceKind: message.request.Kind, label: message.request.Service + "@" + message.request.Node, assessment: message.plan.Assessment(), session: m.session}
		return m, nil
	case directNodePrepared:
		if message.generation != m.generation() || message.operation != m.mutation.operation {
			return m, nil
		}
		if message.request.Kind == application.ActionUpgrade {
			m.requests.cancelSlot("upgrade-prepare")
			m.mutation.upgradePending = false
			if m.views.top().Kind != viewNodes {
				m.mutation.busy = false
				m.mutation.notice = ""
				return m, nil
			}
		}
		m.mutation.busy = false
		if message.err != nil || message.plan == nil {
			if message.request.Kind == application.ActionUpgrade {
				m.mutation.notice = ""
			}
			m.view.notice = mutationErrorMessage(message.err)
			return m, nil
		}
		m.palette = m.palette.close()
		prompt := &directMutationPrompt{
			operation: message.operation, generation: message.generation, node: message.plan, nodeKind: message.request.Kind,
			assessment: message.plan.Assessment(), session: m.session,
			confirm: application.Confirmation{Approved: message.request.Kind != application.ActionReset && message.request.Kind != application.ActionWipeDevice},
		}
		if message.request.Kind == application.ActionUpgrade {
			prompt.label = message.request.Image
		}
		if message.request.Reset != nil {
			options := *message.request.Reset
			options.UserDisks = append([]string(nil), options.UserDisks...)
			options.SystemPartitions = append([]string(nil), options.SystemPartitions...)
			prompt.reset = &options
		}
		if message.request.DeviceWipe != nil {
			options := *message.request.DeviceWipe
			prompt.deviceWipe = &options
		}
		if message.request.Kind == application.ActionReset || message.request.Kind == application.ActionWipeDevice {
			prompt.intent = destructiveIntentInput()
			if message.request.Kind == application.ActionReset {
				prompt.field = 3
			} else {
				prompt.field = 1
			}
		}
		m.mutation.prompt = prompt
		return m, nil
	case directEtcdPrepared:
		if message.generation != m.generation() || message.operation != m.mutation.operation {
			return m, nil
		}
		m.mutation.busy = false
		m.mutation.notice = ""
		if message.err != nil || message.plan == nil {
			m.mutation.etcdKind = ""
			m.view.notice = mutationErrorMessage(message.err)
			return m, nil
		}
		m.palette = m.palette.close()
		m.mutation.prompt = &directMutationPrompt{
			operation: message.operation, generation: message.generation, etcd: message.plan, etcdKind: message.request.Kind,
			assessment: message.plan.Assessment(), session: m.session, confirm: application.Confirmation{Approved: true},
			label: message.request.MemberHostname, etcdMemberID: message.memberID, etcdMemberHostname: message.memberHostname,
		}
		return m, nil
	case directMutationProgress:
		if message.generation != m.generation() || message.operation != m.mutation.operation || !m.mutation.busy {
			return m, nil
		}
		m.mutation.results = appendMutationResult(m.mutation.results, message.result)
		m.mutation.notice = renderMutationResults(m.mutation.results, m.mutation.total)
		return m, directMutationAwaitCommand(m.mutationWork)
	case directUpgradeProgress:
		if message.generation != m.generation() || message.operation != m.mutation.operation || !m.mutation.busy {
			return m, nil
		}
		m.mutation.upgrade = directUpgradeDisplayState{Active: true, Target: message.target, Event: message.event}
		m.mutation.notice = renderDirectUpgradeNotice(m.mutation.upgrade)
		return m, directMutationAwaitCommand(m.mutationWork)
	case directMutationCanceled:
		// A session switch or root shutdown can stop the TUI waiter before
		// backend cleanup has published its retained terminal result. The
		// session still owns that wait and mutation lease.
		return m, nil
	case directMutationFinished:
		if message.generation != m.generation() || message.operation != m.mutation.operation {
			return m, nil
		}
		m.mutation.busy = false
		if message.upgradeStartErr != nil {
			m.mutation.upgradePending = false
			m.mutation.upgradeErr = message.upgradeStartErr
			m.mutation.upgrade = directUpgradeDisplayState{Target: message.upgradeTarget}
			m.mutation.notice = directUpgradeAdmissionNotice(message.upgradeTarget, message.upgradeStartErr)
			m.mutationWork = directMutationWork{}
			return m, nil
		}
		if message.upgrade != nil || message.upgradeWaitErr != nil {
			m.mutation.upgradePending = false
			m.mutation.upgradeResult = message.upgrade
			m.mutation.upgradeErr = message.upgradeWaitErr
			state := directUpgradeDisplayState{Target: message.upgradeTarget}
			if message.upgrade != nil {
				state.Event = upgradeEventValue(message.upgrade.Event)
				switch message.upgrade.Outcome {
				case ports.UpgradeOutcomeAppliedWithRecoveryWarning:
					state.Warning = "upgrade applied; node recovery is incomplete — check node readiness and scheduling manually"
				case ports.UpgradeOutcomeApplied:
					state.Outcome = domain.OperationCompleted
				case ports.UpgradeOutcomeAccepted:
					state.Outcome = domain.OperationAccepted
				default:
					state.Outcome = domain.OperationUnconfirmed
				}
				if state.Warning == "" && message.upgrade.Outcome != ports.UpgradeOutcomeAccepted && message.upgrade.Outcome != ports.UpgradeOutcomeApplied {
					state.Err = "request failed; check node state before retrying"
				}
			}
			if message.upgradeWaitErr != nil {
				state.Outcome = domain.OperationUnconfirmed
				state.Err = "request failed; check node state before retrying"
			}
			m.mutation.upgrade = state
			m.mutation.notice = renderDirectUpgradeNotice(state)
			m.mutationWork = directMutationWork{}
			return m, nil
		}
		m.mutation.prompt = nil
		m.mutation.results = append([]application.TargetResult(nil), message.result.Targets...)
		m.mutation.snapshot = cloneEtcdSnapshotResult(message.result.Snapshot)
		m.mutation.receiptVisible = m.mutation.snapshot != nil && m.canShowDirectEtcdReceipt()
		var refreshDisks tea.Cmd
		if plan := m.mutationWork.plan; plan != nil && plan.nodeKind == application.ActionWipeDevice && message.err == nil {
			completed := false
			for _, result := range message.result.Targets {
				if result.Outcome == domain.OperationCompleted {
					completed = true
					break
				}
			}
			targetNode := m.disks.node
			if plan.deviceWipe != nil {
				targetNode = plan.deviceWipe.Node
			}
			if completed && m.disks.node == targetNode {
				m.disks.status = directLoading
				m.disks.err = ""
				for _, frame := range m.views {
					if frame.Kind == viewDisks && frame.NodeTarget == targetNode && m.session != nil {
						request, _ := m.requests.beginWithContext(directDiagnosticSlot(frame.NodeID, "disks"), m.generation(), m.sessionCtx)
						refreshDisks = directDisksCommand(m.session, request, frame.NodeID, frame.NodeTarget)
						break
					}
				}
			}
		}
		if m.mutation.etcdKind != "" {
			failure := ""
			if message.err != nil {
				failure = mutationErrorMessage(message.err)
			}
			m.mutation.notice = renderEtcdMutationSummary(m.mutation.results, m.mutation.etcdKind, m.mutation.snapshot, failure)
			m.view.notice = ""
		} else if len(m.mutation.results) == 0 && message.err != nil {
			if m.mutation.snapshot != nil {
				m.mutation.notice = renderMutationResults(m.mutation.results, m.mutation.total)
				m.view.notice = ""
			} else {
				m.view.notice = mutationErrorMessage(message.err)
				m.mutation.notice = ""
			}
		} else {
			m.mutation.notice = renderMutationResults(m.mutation.results, m.mutation.total)
		}
		m.mutationWork = directMutationWork{}
		return m, refreshDisks
	}
	return m, nil
}

func (m *model) handleDirectMutationInput(message tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.mutation.upgradePending && message.String() == "esc" {
		m.requests.cancelSlot("upgrade-image")
		m.requests.cancelSlot("upgrade-prepare")
		m.upgradePrompt = nil
		m.mutation.busy = false
		m.mutation.upgradePending = false
		m.mutation.operation = m.nextMutationOperation()
		m.mutation.notice = ""
		m.view.notice = "Action canceled"
		return nil, true
	}
	if m.etcdPathPrompt != nil {
		switch message.String() {
		case "esc":
			m.etcdPathPrompt = nil
			m.mutation.notice = ""
			m.mutation.etcdKind = ""
			m.view.notice = "Action canceled"
			return nil, true
		case "enter":
			path := m.etcdPathPrompt.input.Value()
			if err := application.ValidateEtcdSnapshotPath(path); err != nil {
				m.etcdPathPrompt.err = "invalid snapshot path"
				return nil, true
			}
			prompt := m.etcdPathPrompt
			m.etcdPathPrompt = nil
			kind := prompt.kind
			if kind == "" {
				kind = application.EtcdActionSnapshot
			}
			request := application.EtcdActionRequest{Kind: kind, Node: prompt.node, MemberHostname: prompt.member, SnapshotPath: path}
			request.MemberID = prompt.memberID
			member := domain.EtcdMemberSnapshot{MemberID: prompt.memberID, Hostname: prompt.member}
			return m.prepareDirectEtcdMutation(request, member), true
		}
		var command tea.Cmd
		*m.etcdPathPrompt, command = m.etcdPathPrompt.update(message)
		return command, true
	}
	if m.upgradePrompt != nil {
		prompt := m.upgradePrompt
		switch message.String() {
		case "esc":
			m.requests.cancelSlot("upgrade-image")
			m.requests.cancelSlot("upgrade-prepare")
			m.upgradePrompt = nil
			m.mutation.busy = false
			m.mutation.upgradePending = false
			m.mutation.operation = m.nextMutationOperation()
			m.mutation.notice = ""
			m.view.notice = "Action canceled"
			return nil, true
		case "enter":
			image := strings.TrimSpace(prompt.input.Value())
			if image == "" {
				m.view.notice = "enter an installer image"
				return nil, true
			}
			m.upgradePrompt = nil
			m.mutation.busy = true
			m.mutation.upgradePending = true
			m.mutation.operation = prompt.operation
			m.mutation.notice = "Preparing upgrade…"
			request, _ := m.requests.beginWithContext("upgrade-prepare", prompt.generation, m.sessionCtx)
			return directPrepareNodeCommand(m.session, request.ctx, prompt.operation, prompt.generation, application.NodeActionRequest{Kind: application.ActionUpgrade, Targets: []string{prompt.target}, Image: image}), true
		}
		var command tea.Cmd
		*prompt, command = prompt.update(message)
		return command, true
	}
	if m.mutation.prompt != nil {
		prompt := m.mutation.prompt
		if prompt.nodeKind == application.ActionReset || prompt.nodeKind == application.ActionWipeDevice {
			command, handled := m.updateDirectDestructivePrompt(message)
			if handled {
				return command, true
			}
		}
		if message.String() == "y" || message.String() == "Y" {
			if prompt.assessment.Blocked != "" {
				m.view.notice = "Action refused: " + prompt.assessment.Blocked
				return nil, true
			}
			if prompt.node != nil && prompt.service == nil && prompt.etcd == nil {
				m.nodes.marked = nil
			}
			m.mutation.prompt = nil
			m.mutation.busy = true
			m.mutation.operation = prompt.operation
			m.mutation.total = len(prompt.assessment.Targets)
			m.mutation.results = nil
			m.mutation.snapshot = nil
			m.mutation.receiptVisible = false
			m.mutation.etcdKind = prompt.etcdKind
			m.mutation.receiptOffset = 0
			m.mutation.notice = "Mutation in progress…"
			capacity := m.mutation.total
			if capacity < 1 {
				capacity = 1
			}
			m.mutationWork = directMutationWork{operation: prompt.operation, generation: prompt.generation, plan: prompt, session: prompt.session, ctx: m.sessionCtx, events: make(chan directMutationWorkItem, capacity+1)}
			return directMutationStartCommand(m.mutationWork), true
		}
		if message.String() == "n" || message.String() == "N" || message.String() == "esc" {
			m.mutation.prompt = nil
			m.mutation.notice = ""
			m.mutation.etcdKind = ""
			m.view.notice = "Action canceled"
		}
		return nil, true
	}
	return nil, false
}
