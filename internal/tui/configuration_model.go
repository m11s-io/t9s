package tui

import (
	"context"
	"fmt"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type configurationPhase uint8

const (
	configurationEditing configurationPhase = iota
	configurationPreparing
	configurationReviewing
	configurationConfirming
	configurationApplying
	configurationComplete
)

type configurationField uint8

const (
	configurationPathField configurationField = iota
	configurationTargetsField
	configurationModeField
	configurationFieldCount
)

var configurationModes = []domain.ConfigurationMode{
	domain.ConfigurationModeAuto,
	domain.ConfigurationModeNoReboot,
	domain.ConfigurationModeStaged,
}

type configurationModel struct {
	path          textinput.Model
	confirmation  textinput.Model
	field         configurationField
	phase         configurationPhase
	nodes         []domain.NodeSnapshot
	targetCursor  int
	selected      map[string]bool
	modeIndex     int
	plan          *application.PreparedConfiguration
	assessment    application.ConfigurationAssessment
	basename      string
	err           string
	results       []application.TargetResult
	work          directConfigurationWork
	operation     uint64
	cancelPreview context.CancelFunc
	cancelApply   context.CancelFunc
}

func newConfigurationModel(initialTargets []string, nodes []domain.NodeSnapshot) configurationModel {
	path := textinput.New()
	path.Prompt = "CONFIG FILE :"
	path.Placeholder = "local Talos machine configuration path"
	path.CharLimit = 4096
	path.SetWidth(72)
	_ = path.Focus()
	confirmation := textinput.New()
	confirmation.Prompt = "TYPE :"
	confirmation.CharLimit = 128
	selected := make(map[string]bool)
	for _, node := range nodes {
		for _, target := range initialTargets {
			if target == node.Target() {
				selected[node.ID] = true
				break
			}
		}
	}
	return configurationModel{path: path, confirmation: confirmation, nodes: append([]domain.NodeSnapshot(nil), nodes...), selected: selected, modeIndex: -1}
}

func (m *configurationModel) setField(field configurationField) {
	m.field = field
	if field == configurationPathField && m.phase == configurationEditing {
		_ = m.path.Focus()
	} else {
		m.path.Blur()
	}
}

func (m configurationModel) selectedTargets() []string {
	targets := make([]string, 0, len(m.selected))
	for _, node := range m.nodes {
		if m.selected[node.ID] && node.Target() != "" {
			targets = append(targets, node.Target())
		}
	}
	return targets
}

func (m configurationModel) mode() (domain.ConfigurationMode, bool) {
	if m.modeIndex < 0 || m.modeIndex >= len(configurationModes) {
		return "", false
	}
	return configurationModes[m.modeIndex], true
}

func (m *configurationModel) discard(session *application.Session) {
	if m.cancelPreview != nil {
		m.cancelPreview()
		m.cancelPreview = nil
	}
	if m.cancelApply != nil {
		m.cancelApply()
		m.cancelApply = nil
	}
	if m.plan != nil && session != nil {
		session.DiscardConfiguration(m.plan)
	}
	m.plan = nil
	m.assessment = application.ConfigurationAssessment{}
}

func (m *model) updateDirectConfigurationInput(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	configuration := &m.configuration
	key := message.String()
	switch configuration.phase {
	case configurationPreparing:
		if key == "esc" || key == "q" {
			if configuration.cancelPreview != nil {
				configuration.cancelPreview()
				configuration.cancelPreview = nil
			}
			configuration.operation++
			configuration.phase = configurationEditing
			configuration.err = "Preview canceled"
		}
		return m, nil
	case configurationReviewing:
		switch key {
		case "esc":
			configuration.discard(m.session)
			configuration.phase = configurationEditing
			configuration.err = ""
		case "enter":
			if configuration.assessment.Blocked == "" && configuration.plan != nil {
				configuration.phase = configurationConfirming
				configuration.confirmation.Reset()
				_ = configuration.confirmation.Focus()
			}
		}
		return m, nil
	case configurationConfirming:
		switch key {
		case "esc":
			configuration.phase = configurationReviewing
			configuration.confirmation.Blur()
		case "enter":
			if configuration.confirmation.Value() != configuration.assessment.RequiredPhrase {
				configuration.err = "Confirmation does not match"
				return m, nil
			}
			return m, m.startDirectConfigurationApply()
		default:
			var command tea.Cmd
			configuration.confirmation, command = configuration.confirmation.Update(message)
			configuration.err = ""
			return m, command
		}
		return m, nil
	case configurationApplying:
		if (key == "esc" || key == "q") && configuration.cancelApply != nil {
			configuration.cancelApply()
			configuration.cancelApply = nil
			configuration.err = "Cancellation requested; remote results may be partial"
		}
		return m, nil
	case configurationComplete:
		if key == "esc" || key == "q" {
			m.closeConfigurationView()
		}
		return m, nil
	}

	if key == "esc" || key == "q" {
		m.closeConfigurationView()
		return m, nil
	}
	if key == "tab" {
		configuration.setField((configuration.field + 1) % configurationFieldCount)
		return m, nil
	}
	switch configuration.field {
	case configurationPathField:
		if key == "enter" {
			configuration.setField(configurationTargetsField)
			return m, nil
		}
		var command tea.Cmd
		configuration.path, command = configuration.path.Update(message)
		configuration.err = ""
		return m, command
	case configurationTargetsField:
		switch key {
		case "up", "k":
			configuration.targetCursor = max(0, configuration.targetCursor-1)
		case "down", "j":
			configuration.targetCursor = min(len(configuration.nodes)-1, configuration.targetCursor+1)
		case "space":
			if configuration.targetCursor >= 0 && configuration.targetCursor < len(configuration.nodes) {
				node := configuration.nodes[configuration.targetCursor]
				if configuration.selected[node.ID] {
					delete(configuration.selected, node.ID)
				} else {
					configuration.selected[node.ID] = true
				}
			}
		case "enter":
			configuration.setField(configurationModeField)
		}
		configuration.err = ""
		return m, nil
	case configurationModeField:
		switch key {
		case "left", "h":
			if configuration.modeIndex < 0 {
				configuration.modeIndex = len(configurationModes) - 1
			} else {
				configuration.modeIndex = (configuration.modeIndex + len(configurationModes) - 1) % len(configurationModes)
			}
		case "right", "l", "space":
			configuration.modeIndex = (configuration.modeIndex + 1) % len(configurationModes)
		case "enter":
			if _, ok := configuration.mode(); !ok {
				configuration.err = "Choose an apply mode with ← or →"
				return m, nil
			}
			return m, m.startDirectConfigurationPreview()
		}
	}
	return m, nil
}

func (m *model) startDirectConfigurationPreview() tea.Cmd {
	configuration := &m.configuration
	targets := configuration.selectedTargets()
	mode, ok := configuration.mode()
	if len(targets) == 0 {
		configuration.err = "Select at least one node"
		return nil
	}
	if !ok {
		configuration.err = "Choose an apply mode with ← or →"
		return nil
	}
	if configuration.path.Value() == "" {
		configuration.err = "Enter a configuration file path"
		configuration.setField(configurationPathField)
		return nil
	}
	configuration.discard(m.session)
	configuration.phase = configurationPreparing
	configuration.err = "Previewing configuration on every target…"
	configuration.basename = ""
	configuration.results = nil
	configuration.operation = m.nextMutationOperation()
	ctx, cancel := context.WithCancel(m.sessionCtx)
	configuration.cancelPreview = cancel
	return directConfigurationPreviewCommand(m.session, ctx, configuration.operation, m.generation(), configuration.path.Value(), targets, mode)
}

func (m *model) startDirectConfigurationApply() tea.Cmd {
	configuration := &m.configuration
	if configuration.plan == nil || configuration.assessment.Blocked != "" {
		configuration.err = "A successful preview is required before apply"
		return nil
	}
	ctx, cancel := context.WithCancel(m.sessionCtx)
	configuration.cancelApply = cancel
	configuration.phase = configurationApplying
	configuration.err = "Applying configuration sequentially…"
	configuration.results = nil
	work := directConfigurationWork{
		operation:    configuration.operation,
		generation:   m.generation(),
		ctx:          ctx,
		session:      m.session,
		plan:         configuration.plan,
		confirmation: application.Confirmation{Approved: true, Token: configuration.assessment.RequiredPhrase},
		events:       make(chan directConfigurationWorkItem, len(configuration.assessment.Targets)+1),
	}
	configuration.work = work
	return directConfigurationStartCommand(work)
}

func (m *model) closeConfigurationView() {
	m.configuration.discard(m.session)
	m.configuration = configurationModel{}
	m.views = m.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})
}

func (m model) updateDirectConfigurationResult(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case directConfigurationPreviewLoaded:
		if message.operation != m.configuration.operation || message.generation != m.generation() || m.views.top().Kind != viewConfiguration {
			if message.plan != nil && m.session != nil {
				m.session.DiscardConfiguration(message.plan)
			}
			return m, nil
		}
		if m.configuration.cancelPreview != nil {
			m.configuration.cancelPreview()
			m.configuration.cancelPreview = nil
		}
		if message.err != "" {
			m.configuration.phase = configurationEditing
			m.configuration.err = message.err
			return m, nil
		}
		m.configuration.plan = message.plan
		m.configuration.basename = message.basename
		m.configuration.assessment = message.plan.Assessment()
		m.configuration.phase = configurationReviewing
		m.configuration.err = ""
		return m, directConfigurationExpiryCommand(message.operation, message.generation, m.configuration.assessment.ExpiresAt)
	case directConfigurationExpired:
		if message.operation != m.configuration.operation || message.generation != m.generation() || m.views.top().Kind != viewConfiguration || m.configuration.plan == nil {
			return m, nil
		}
		m.configuration.discard(m.session)
		m.configuration.phase = configurationEditing
		m.configuration.err = "Configuration preview expired; prepare a new preview"
		return m, nil
	case directConfigurationProgress:
		if message.operation != m.configuration.operation || message.generation != m.generation() || m.views.top().Kind != viewConfiguration {
			return m, nil
		}
		m.configuration.results = append(m.configuration.results, message.result)
		return m, directConfigurationAwaitCommand(directConfigurationWorkFromModel(m))
	case directConfigurationFinished:
		if message.operation != m.configuration.operation || message.generation != m.generation() || m.views.top().Kind != viewConfiguration {
			return m, nil
		}
		if m.configuration.cancelApply != nil {
			m.configuration.cancelApply()
			m.configuration.cancelApply = nil
		}
		m.configuration.phase = configurationComplete
		m.configuration.results = append([]application.TargetResult(nil), message.result.Targets...)
		m.configuration.err = message.err
		return m, nil
	default:
		return m, nil
	}
}

func directConfigurationWorkFromModel(m model) directConfigurationWork {
	return m.configuration.work
}

func (m configurationModel) summaryLine() string {
	return fmt.Sprintf("%d selected target(s)", len(m.selectedTargets()))
}
