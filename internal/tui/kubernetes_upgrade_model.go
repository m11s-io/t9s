package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type kubernetesUpgradePhase uint8

const (
	kubernetesUpgradeEditing kubernetesUpgradePhase = iota
	kubernetesUpgradePreparing
	kubernetesUpgradeReviewing
	kubernetesUpgradeConfirming
	kubernetesUpgradeRunning
	kubernetesUpgradeComplete
)

type kubernetesUpgradeModel struct {
	target        textinput.Model
	snapshot      textinput.Model
	field         int
	phase         kubernetesUpgradePhase
	plan          *application.PreparedKubernetesUpgrade
	stream        ports.KubernetesUpgradeStream
	events        []domain.KubernetesUpgradeEvent
	result        *domain.KubernetesUpgradeResult
	err           string
	operation     uint64
	generation    uint64
	cancelRequest context.CancelFunc
}

func newKubernetesUpgradeModel() kubernetesUpgradeModel {
	target := textinput.New()
	target.Prompt = "TARGET :"
	target.Placeholder = "stable Kubernetes version, e.g. 1.37.1"
	target.CharLimit = 32
	target.SetWidth(64)
	_ = target.Focus()
	snapshot := textinput.New()
	snapshot.Prompt = "ETCD SNAPSHOT :"
	snapshot.Placeholder = "new local snapshot path"
	snapshot.CharLimit = 4096
	snapshot.SetWidth(64)
	snapshot.SetValue(defaultKubernetesUpgradeSnapshotPath())
	return kubernetesUpgradeModel{target: target, snapshot: snapshot, phase: kubernetesUpgradeEditing}
}

func defaultKubernetesUpgradeSnapshotPath() string {
	return filepath.Join(os.TempDir(), "t9s-k8s-upgrade-"+time.Now().Format("2006-01-02_15-04-05.000")+".snapshot")
}

func (m *kubernetesUpgradeModel) close(session *application.Session) {
	if m.cancelRequest != nil {
		m.cancelRequest()
		m.cancelRequest = nil
	}
	if m.stream != nil {
		m.stream.Cancel()
		m.stream = nil
	}
	m.plan = nil
	m.phase = kubernetesUpgradeEditing
	if m.target.Value() != "" {
		m.target.Reset()
	}
	if m.snapshot.Value() != "" {
		m.snapshot.SetValue(defaultKubernetesUpgradeSnapshotPath())
	}
	_ = session
}

func (m kubernetesUpgradeModel) view(width, height int, writesEnabled bool) string {
	plan := ""
	if m.plan != nil {
		summary := m.plan.Summary()
		var lines []string
		lines = append(lines, fmt.Sprintf("Cluster UID: %s", summary.ClusterUID))
		lines = append(lines, fmt.Sprintf("Talos CLI: %s", summary.CLI.Version))
		lines = append(lines, fmt.Sprintf("Kubernetes: %s -> %s", summary.SourceVersion, summary.TargetVersion))
		lines = append(lines,
			"  API server: registry.k8s.io/kube-apiserver:v"+summary.TargetVersion,
			"  Controller manager: registry.k8s.io/kube-controller-manager:v"+summary.TargetVersion,
			"  Scheduler: registry.k8s.io/kube-scheduler:v"+summary.TargetVersion)
		if summary.ProxyEnabled {
			lines = append(lines, "  kube-proxy: "+summary.TargetProxyImage)
			lines = append(lines, "Order: control plane, kube-proxy, managed manifests, then kubelets")
		} else {
			lines = append(lines, "  kube-proxy: remains disabled; Talos image config -> "+summary.TargetProxyImage)
			lines = append(lines, "Order: control plane, managed manifests, then kubelets")
		}
		lines = append(lines, "Kubelets: one node at a time; update image, restart service, verify readiness")
		lines = append(lines, "Node scheduling stays unchanged; draining is a separate operation")
		lines = append(lines, "Manifest preview: anticipated Talos managed-resource reconciliation; exact diff is execution-time")
		for _, node := range summary.Nodes {
			state := "update kubelet"
			if node.Skip {
				state = "already at target"
			}
			lines = append(lines, fmt.Sprintf("  %-12s %-24s %s (%s)", node.Role, node.Name, node.SourceVersion, state))
		}
		plan = strings.Join(lines, "\n")
	}
	body := []string{"KUBERNETES UPGRADE THROUGH TALOS"}
	switch m.phase {
	case kubernetesUpgradeEditing, kubernetesUpgradePreparing:
		body = append(body, "Enter a stable target and a new local etcd snapshot path.", "Tab changes fields; Enter on the snapshot field prepares a read-only plan.", m.target.View(), m.snapshot.View())
	case kubernetesUpgradeReviewing, kubernetesUpgradeConfirming:
		body = append(body, plan)
		if !writesEnabled {
			body = append(body, "Read-only session: execution requires --enable-writes.")
		}
		if m.phase == kubernetesUpgradeReviewing {
			body = append(body, "Enter to confirm; Esc discards this plan.")
		} else {
			body = append(body, "Press y to start this upgrade · n/Esc cancels.")
		}
	case kubernetesUpgradeRunning:
		body = append(body, plan, "Upgrade is running. Esc requests cancellation.")
		for _, event := range m.events {
			label := string(event.Stage)
			if event.NodeUID != "" {
				label += " · " + event.NodeUID
			}
			body = append(body, "  "+label+" · "+event.Status)
		}
	case kubernetesUpgradeComplete:
		body = append(body, plan)
		if m.result != nil {
			body = append(body, "Outcome: "+string(m.result.Outcome))
			for _, node := range m.result.Nodes {
				if node.Message != "" {
					body = append(body, "Reason: "+sanitizeUntrustedText(node.Message))
				}
			}
			if m.result.Snapshot != nil {
				body = append(body, "Snapshot: "+m.result.Snapshot.Path+" · "+m.result.Snapshot.SHA256)
			}
			for _, node := range m.result.Nodes {
				line := fmt.Sprintf("  %s · %s", node.NodeUID, node.Outcome)
				if node.LeftCordoned {
					line += " · existing cordon preserved"
				}
				body = append(body, line)
			}
		}
		body = append(body, "Press Esc to return to nodes.")
	}
	if m.err != "" {
		body = append(body, "", m.err)
	}
	if width < 1 {
		width = 1
	}
	if height > 0 && len(body) > height {
		body = body[:height]
	}
	for index := range body {
		body[index] = fitK9sCell(body[index], width)
	}
	return strings.Join(body, "\n")
}

func (m *model) updateKubernetesUpgradeInput(message tea.KeyPressMsg) (tea.Cmd, bool) {
	u := &m.kubernetesUpgrade
	key := message.String()
	switch u.phase {
	case kubernetesUpgradeEditing:
		if key == "esc" {
			u.close(m.session)
			m.views = m.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})
			return nil, true
		}
		if key == "tab" {
			u.field = (u.field + 1) % 2
			if u.field == 0 {
				_ = u.target.Focus()
				u.snapshot.Blur()
			} else {
				u.target.Blur()
				_ = u.snapshot.Focus()
			}
			return nil, true
		}
		if key == "shift+tab" {
			u.field = (u.field + 1) % 2
			if u.field == 0 {
				_ = u.target.Focus()
				u.snapshot.Blur()
			} else {
				u.target.Blur()
				_ = u.snapshot.Focus()
			}
			return nil, true
		}
		if key == "enter" && u.field == 0 {
			u.field = 1
			u.target.Blur()
			_ = u.snapshot.Focus()
			return nil, true
		}
		if key == "enter" && u.field == 1 {
			if m.session == nil {
				u.err = "Cluster session is unavailable"
				return nil, true
			}
			if strings.TrimSpace(u.target.Value()) == "" || strings.TrimSpace(u.snapshot.Value()) == "" {
				u.err = "Enter both a target version and snapshot path"
				return nil, true
			}
			u.operation, u.generation = m.nextMutationOperation(), m.generation()
			request, cancel := m.requests.beginWithContext("kubernetes-upgrade", u.generation, m.sessionCtx)
			u.cancelRequest, u.phase, u.err = cancel, kubernetesUpgradePreparing, ""
			return directKubernetesUpgradePrepareCommand(m.session, request, u.operation, u.generation, u.target.Value()), true
		}
		var command tea.Cmd
		if u.field == 0 {
			u.target, command = u.target.Update(message)
		} else {
			u.snapshot, command = u.snapshot.Update(message)
		}
		return command, true
	case kubernetesUpgradeReviewing:
		if key == "esc" {
			u.close(m.session)
			return nil, true
		}
		if key == "enter" {
			u.phase = kubernetesUpgradeConfirming
			return nil, true
		}
		return nil, true
	case kubernetesUpgradeConfirming:
		if key == "esc" || key == "n" {
			u.close(m.session)
			return nil, true
		}
		if key == "y" {
			if !m.options.WritesEnabled {
				u.err = "Execution requires --enable-writes"
				return nil, true
			}
			if u.plan == nil {
				u.err = "Prepared upgrade plan is unavailable"
				return nil, true
			}
			request, cancel := m.requests.beginWithContext("kubernetes-upgrade", u.generation, m.sessionCtx)
			u.cancelRequest = cancel
			u.phase, u.err = kubernetesUpgradeRunning, ""
			return directKubernetesUpgradeStartCommand(m.session, request, u.operation, u.generation, u.plan, application.Confirmation{Approved: true, Token: u.plan.Assessment().RequiredToken}, u.snapshot.Value()), true
		}
		return nil, true
	case kubernetesUpgradeRunning:
		if key == "esc" && u.stream != nil {
			u.stream.Cancel()
			u.err = "Cancellation requested; waiting for the process and node operation to stop."
			return nil, true
		}
		return nil, true
	case kubernetesUpgradeComplete:
		if key == "esc" || key == "q" {
			u.close(m.session)
			m.views = m.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})
			return nil, true
		}
		return nil, true
	}
	return nil, false
}

func (m *model) updateDirectKubernetesUpgradeResult(message tea.Msg) (model, tea.Cmd) {
	u := &m.kubernetesUpgrade
	switch message := message.(type) {
	case directKubernetesUpgradePrepared:
		if message.operation != u.operation || message.generation != m.generation() || !m.views.contains(viewKubernetesUpgrade) {
			return *m, nil
		}
		if u.cancelRequest != nil {
			u.cancelRequest()
			u.cancelRequest = nil
		}
		if message.err != "" || message.plan == nil {
			u.phase = kubernetesUpgradeEditing
			u.err = message.err
			if u.err == "" {
				u.err = "Unable to prepare a safe Kubernetes upgrade plan"
			}
			return *m, nil
		}
		u.plan = message.plan
		u.phase = kubernetesUpgradeReviewing
		u.err = ""
		return *m, nil
	case directKubernetesUpgradeOpened:
		if message.operation != u.operation || message.generation != m.generation() || !m.views.contains(viewKubernetesUpgrade) {
			if message.stream != nil {
				message.stream.Cancel()
			}
			return *m, nil
		}
		if message.err != "" || message.stream == nil {
			if u.cancelRequest != nil {
				u.cancelRequest()
				u.cancelRequest = nil
			}
			u.phase = kubernetesUpgradeReviewing
			u.err = "Unable to start upgrade; review the prepared plan and session"
			return *m, nil
		}
		// Keep this request context alive: it is also the retained operation's
		// parent and is canceled only when the operator leaves or cancels.
		u.stream = message.stream
		return *m, directKubernetesUpgradeAwaitCommand(u.stream, u.operation, u.generation)
	case directKubernetesUpgradeEvent:
		if message.operation != u.operation || message.generation != m.generation() || !m.views.contains(viewKubernetesUpgrade) || u.phase != kubernetesUpgradeRunning {
			return *m, nil
		}
		u.events = append(u.events, message.event)
		if len(u.events) > 50 {
			u.events = append([]domain.KubernetesUpgradeEvent(nil), u.events[len(u.events)-50:]...)
		}
		return *m, directKubernetesUpgradeAwaitCommand(u.stream, u.operation, u.generation)
	case directKubernetesUpgradeFinished:
		if message.operation != u.operation || message.generation != m.generation() || !m.views.contains(viewKubernetesUpgrade) {
			return *m, nil
		}
		if u.cancelRequest != nil {
			u.cancelRequest()
			u.cancelRequest = nil
		}
		u.stream = nil
		u.result = &message.result
		u.err = message.err
		u.phase = kubernetesUpgradeComplete
		return *m, nil
	default:
		return *m, nil
	}
}
