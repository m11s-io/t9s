package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
)

const (
	configurationFileLimit  = 4 << 20
	configurationPlanTTL    = 5 * time.Minute
	configurationRPCTimeout = 30 * time.Second
	configurationApplyTTL   = 10 * time.Minute
)

type ConfigurationApplyRequest struct {
	Data    []byte
	Targets []string
	Mode    domain.ConfigurationMode
}

type ConfigurationPreviewStatus string

const (
	ConfigurationPreviewSucceeded ConfigurationPreviewStatus = "succeeded"
	ConfigurationPreviewFailed    ConfigurationPreviewStatus = "failed"
)

type ConfigurationTargetPreview struct {
	Target        string
	Mode          domain.ConfigurationMode
	WarningCount  int
	WarningsKnown bool
	Status        ConfigurationPreviewStatus
	Error         string
}

type ConfigurationAssessment struct {
	Targets        []string
	Mode           domain.ConfigurationMode
	DigestPrefix   string
	RequiredPhrase string
	ExpiresAt      time.Time
	Blocked        string
	Previews       []ConfigurationTargetPreview
}

type configurationNodeIdentity struct {
	ID     string
	Name   string
	Target string
	Role   domain.NodeRole
}

type configurationTargetFact struct {
	Identity configurationNodeIdentity
	Preview  domain.ConfigurationPreview
}

type configurationFacts struct {
	Targets       []configurationTargetFact
	PayloadDigest [32]byte
	Expires       time.Time
}

// PrepareConfiguration previews a local configuration on every canonical
// target and returns a one-use plan only when all dry runs succeed.
func (s *Session) PrepareConfiguration(ctx context.Context, request ConfigurationApplyRequest) (*PreparedConfiguration, error) {
	if s == nil {
		return nil, ErrSessionClosed
	}
	s.mu.Lock()
	closed, writesEnabled := s.closed || s.lifetimeCtx == nil || s.lifetimeCtx.Err() != nil, s.writesEnabled
	s.mu.Unlock()
	if closed {
		return nil, ErrSessionClosed
	}
	if !writesEnabled {
		return nil, ErrWritesDisabled
	}
	if err := validateConfigurationApplyRequest(request); err != nil {
		return nil, err
	}
	if s.sdkSession == nil || s.sdkSession.Configuration() == nil {
		return nil, ErrCapabilityUnavailable
	}

	operationCtx, cancel, err := s.operationContext(ctx, configurationApplyTTL)
	if err != nil {
		return nil, err
	}
	defer cancel()
	readCtx, readCancel := context.WithTimeout(operationCtx, unaryReadTimeout)
	nodes, err := readNodes(readCtx, s.sdkSession)
	readCancel()
	if err != nil {
		return nil, err
	}
	if err := operationCtx.Err(); err != nil {
		return nil, err
	}
	canonical, resolved, err := resolveNodeActionTargets(nodes.Nodes, request.Targets)
	if err != nil {
		return nil, err
	}
	request.Targets = canonical

	facts := configurationFacts{Targets: make([]configurationTargetFact, 0, len(resolved))}
	previews := make([]ConfigurationTargetPreview, 0, len(resolved))
	blocked := ""
	for _, node := range resolved {
		identity := configurationNodeIdentity{ID: node.ID, Name: node.Name, Target: node.Target(), Role: node.Role}
		previewCtx, previewCancel := context.WithTimeout(operationCtx, configurationRPCTimeout)
		preview, previewErr := s.sdkSession.Configuration().Preview(previewCtx, node.Target(), request.Data, request.Mode)
		previewCancel()
		if previewErr != nil || preview.Mode != expectedConfigurationResponseMode(request.Mode) {
			safeError := "preview failed"
			if previewErr != nil {
				safeError = safeConfigurationFailure(previewErr).Error()
			} else {
				previewErr = domain.ConfigurationErrorResponseInvalid
				safeError = previewErr.Error()
			}
			blocked = appendActionBlock(blocked, "one or more targets failed configuration preview")
			previews = append(previews, ConfigurationTargetPreview{Target: node.Target(), Status: ConfigurationPreviewFailed, Error: safeError})
			continue
		}
		facts.Targets = append(facts.Targets, configurationTargetFact{Identity: identity, Preview: preview})
		previews = append(previews, ConfigurationTargetPreview{Target: node.Target(), Mode: preview.Mode, WarningCount: preview.WarningCount, WarningsKnown: preview.WarningsKnown, Status: ConfigurationPreviewSucceeded})
	}
	if err := operationCtx.Err(); err != nil {
		clear(request.Data)
		return nil, err
	}
	facts.Expires = time.Now().Add(configurationPlanTTL)
	digest := sha256.Sum256(request.Data)
	facts.PayloadDigest = digest
	assessment := Assessment{Targets: append([]string(nil), canonical...), Blocked: blocked}
	assessment.RequiredToken = configurationConfirmationPhrase(request.Mode, len(canonical))
	configAssessment := ConfigurationAssessment{
		Targets: append([]string(nil), canonical...), Mode: request.Mode,
		DigestPrefix: hex.EncodeToString(digest[:])[:12], RequiredPhrase: assessment.RequiredToken,
		ExpiresAt: facts.Expires, Blocked: blocked, Previews: previews,
	}
	state := &preparedState{session: s, assessment: assessment, configurationFacts: &facts}
	if blocked == "" {
		request.Data = append([]byte(nil), request.Data...)
		state.configurationRequest = &request
	}
	prepared := &PreparedConfiguration{state: state}
	prepared.state.configurationAssessment = &configAssessment
	return prepared, nil
}

func validateConfigurationApplyRequest(request ConfigurationApplyRequest) error {
	if len(request.Data) == 0 {
		return errors.New("configuration file is empty")
	}
	if len(request.Data) > configurationFileLimit {
		return errors.New("configuration file exceeds 4 MiB")
	}
	if !supportedConfigurationMode(request.Mode) {
		return domain.ConfigurationErrorUnsupported
	}
	if len(request.Targets) == 0 {
		return errors.New("configuration apply requires at least one target")
	}
	return nil
}

func supportedConfigurationMode(mode domain.ConfigurationMode) bool {
	return mode == domain.ConfigurationModeAuto || mode == domain.ConfigurationModeNoReboot || mode == domain.ConfigurationModeStaged
}

func expectedConfigurationResponseMode(mode domain.ConfigurationMode) domain.ConfigurationMode {
	if mode == domain.ConfigurationModeAuto {
		return domain.ConfigurationModeNoReboot
	}
	return mode
}

func configurationConfirmationPhrase(mode domain.ConfigurationMode, count int) string {
	noun := "nodes"
	if count == 1 {
		noun = "node"
	}
	return fmt.Sprintf("apply %s to %d %s", mode, count, noun)
}

func (p PreparedConfiguration) Previews() []ConfigurationTargetPreview {
	return slices.Clone(configurationAssessmentOf(p.state).Previews)
}

func configurationAssessmentOf(state *preparedState) ConfigurationAssessment {
	if state == nil || state.configurationAssessment == nil {
		return ConfigurationAssessment{}
	}
	assessment := *state.configurationAssessment
	assessment.Targets = slices.Clone(assessment.Targets)
	assessment.Previews = slices.Clone(assessment.Previews)
	return assessment
}

// DiscardConfiguration releases an unconsumed prepared payload when the
// operator edits or leaves the preview. Execution owns cleanup after admission.
func (s *Session) DiscardConfiguration(plan *PreparedConfiguration) {
	if s == nil || plan == nil || plan.state == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan.state.session != s || plan.state.consumed || plan.state.configurationRequest == nil {
		return
	}
	clear(plan.state.configurationRequest.Data)
	plan.state.configurationRequest = nil
}

// ExecuteConfiguration rechecks identity and dry-run fingerprints under the
// shared session mutation lease, then applies targets sequentially.
func (s *Session) ExecuteConfiguration(ctx context.Context, plan *PreparedConfiguration, confirmation Confirmation, onTarget func(TargetResult)) (MutationResult, error) {
	if plan == nil || plan.state == nil || plan.state.configurationRequest == nil || plan.state.configurationFacts == nil {
		return MutationResult{}, ErrInvalidPlan
	}
	state := plan.state
	operationCtx, release, err := s.admit(ctx, state, confirmation, configurationApplyTTL)
	if err != nil {
		return MutationResult{}, err
	}
	defer release()
	defer clear(state.configurationRequest.Data)
	if time.Now().After(state.configurationFacts.Expires) {
		state.configurationAssessment.Blocked = "configuration preview expired; prepare a new preview"
		return MutationResult{}, ErrAssessmentChanged
	}
	if s.sdkSession == nil || s.sdkSession.Configuration() == nil {
		return MutationResult{}, ErrCapabilityUnavailable
	}
	request := state.configurationRequest
	if sha256.Sum256(request.Data) != state.configurationFacts.PayloadDigest {
		return MutationResult{}, ErrAssessmentChanged
	}
	readCtx, readCancel := context.WithTimeout(operationCtx, unaryReadTimeout)
	nodes, readErr := readNodes(readCtx, s.sdkSession)
	readCancel()
	if readErr != nil {
		return MutationResult{}, errors.Join(ErrAssessmentChanged, readErr)
	}
	canonical, resolved, err := resolveNodeActionTargets(nodes.Nodes, request.Targets)
	if err != nil || !slices.Equal(canonical, request.Targets) || !sameConfigurationIdentities(resolved, state.configurationFacts.Targets) {
		return MutationResult{}, ErrAssessmentChanged
	}
	for i, node := range resolved {
		if err := operationCtx.Err(); err != nil {
			return MutationResult{}, err
		}
		previewCtx, cancel := context.WithTimeout(operationCtx, configurationRPCTimeout)
		preview, previewErr := s.sdkSession.Configuration().Preview(previewCtx, node.Target(), request.Data, request.Mode)
		cancel()
		fact := state.configurationFacts.Targets[i]
		if previewErr != nil || preview.Mode != expectedConfigurationResponseMode(request.Mode) || preview.Fingerprint != fact.Preview.Fingerprint {
			return MutationResult{}, ErrAssessmentChanged
		}
	}

	result := MutationResult{Targets: make([]TargetResult, 0, len(request.Targets))}
	for i, target := range request.Targets {
		if err := operationCtx.Err(); err != nil {
			for _, notAttempted := range request.Targets[i:] {
				targetResult := TargetResult{Target: notAttempted, Outcome: domain.OperationNotAttempted, Err: domain.ConfigurationErrorCanceled}
				result.Targets = append(result.Targets, targetResult)
				if onTarget != nil {
					onTarget(targetResult)
				}
			}
			return result, err
		}
		rpcCtx, cancel := context.WithTimeout(operationCtx, configurationRPCTimeout)
		applied, applyErr := s.sdkSession.Configuration().Apply(rpcCtx, target, request.Data, request.Mode)
		cancel()
		if applyErr != nil || applied.Mode != expectedConfigurationResponseMode(request.Mode) {
			if applyErr == nil {
				applyErr = domain.ConfigurationErrorResponseInvalid
			}
			applyErr = safeConfigurationFailure(applyErr)
			failed := TargetResult{Target: target, Outcome: domain.OperationUnconfirmed, Err: applyErr}
			result.Targets = append(result.Targets, failed)
			if onTarget != nil {
				onTarget(failed)
			}
			for _, notAttempted := range request.Targets[i+1:] {
				pending := TargetResult{Target: notAttempted, Outcome: domain.OperationNotAttempted, Warning: "not attempted after an earlier apply failed"}
				result.Targets = append(result.Targets, pending)
				if onTarget != nil {
					onTarget(pending)
				}
			}
			return result, applyErr
		}
		outcome := domain.OperationAccepted
		if request.Mode == domain.ConfigurationModeStaged {
			outcome = domain.OperationStaged
		}
		accepted := TargetResult{Target: target, Outcome: outcome}
		result.Targets = append(result.Targets, accepted)
		if onTarget != nil {
			onTarget(accepted)
		}
	}
	return result, nil
}

func safeConfigurationFailure(err error) error {
	if err == nil {
		return domain.ConfigurationErrorUnknown
	}
	var configurationError domain.ConfigurationError
	if errors.As(err, &configurationError) {
		return configurationError
	}
	if errors.Is(err, context.Canceled) {
		return domain.ConfigurationErrorCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.ConfigurationErrorDeadline
	}
	return domain.ConfigurationErrorUnknown
}

func sameConfigurationIdentities(nodes []domain.NodeSnapshot, facts []configurationTargetFact) bool {
	if len(nodes) != len(facts) {
		return false
	}
	for i, node := range nodes {
		identity := facts[i].Identity
		if node.ID != identity.ID || node.Name != identity.Name || node.Target() != identity.Target || node.Role != identity.Role {
			return false
		}
	}
	return true
}
