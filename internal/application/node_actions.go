package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type nodeIdentityFacts struct {
	ID      string
	Name    string
	Target  string
	Role    domain.NodeRole
	Version string
}

type nodeActionFacts struct {
	Nodes       []nodeIdentityFacts
	Warning     string
	Blocked     string
	Disks       *destructiveDiskFacts
	ResetQuorum etcdQuorumAssessment
	HasQuorum   bool
}

// PrepareNodeAction resolves real backend nodes and captures their identity
// and current quorum risk before the caller asks for confirmation.
func (s *Session) PrepareNodeAction(ctx context.Context, request NodeActionRequest) (*PreparedNodeAction, error) {
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
	if err := validateNodeActionRequest(request); err != nil {
		return nil, err
	}
	if s.sdkSession == nil || s.sdkSession.NodeActions() == nil {
		return nil, ErrCapabilityUnavailable
	}

	readCtx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return nil, err
	}
	defer cancel()
	nodes, err := readNodes(readCtx, s.sdkSession)
	if err != nil {
		if readCtx.Err() != nil {
			return nil, errors.Join(readCtx.Err(), err)
		}
		return nil, err
	}
	if err := readCtx.Err(); err != nil {
		return nil, err
	}

	canonical, resolved, err := resolveNodeActionTargets(nodes.Nodes, request.Targets)
	if err != nil {
		return nil, err
	}
	request.Targets = canonical
	if request.Kind == ActionWipeDevice {
		wipeNode, ok := resolveNodeTarget(nodes.Nodes, request.DeviceWipe.Node)
		if !ok || wipeNode.Target() != canonical[0] {
			return nil, errors.New("device-wipe node must resolve to the requested target")
		}
		wipe := *request.DeviceWipe
		wipe.Node = canonical[0]
		request.DeviceWipe = &wipe
	}
	facts, err := s.readNodeActionFacts(readCtx, nodes.Nodes, resolved, request)
	if err != nil {
		return nil, err
	}
	assessment := Assessment{Targets: append([]string(nil), canonical...), Warning: facts.Warning, Blocked: facts.Blocked}
	if request.Kind == ActionReset || request.Kind == ActionWipeDevice {
		diskFacts, err := s.readDestructiveDiskFacts(readCtx, resolved, request)
		if err != nil {
			return nil, err
		}
		facts.Disks = &diskFacts
		assessment.Blocked = appendActionBlock(assessment.Blocked, diskFacts.Blocked)
		if request.Kind == ActionReset {
			assessment.ResetPreview = cloneResetPreviews(diskFacts.Previews)
		}
	}
	if err := readCtx.Err(); err != nil {
		return nil, err
	}
	if request.Kind == ActionReset {
		assessment.RequiredToken = ResetConfirmationToken(canonical)
	}
	if request.Kind == ActionWipeDevice {
		assessment.RequiredToken = request.DeviceWipe.Device
	}
	plan := newPreparedNodeAction(s, request, assessment)
	plan.state.nodeFacts = &facts
	return &plan, nil
}

// ExecuteNodeAction revalidates node identity and quorum risk under the
// shared mutation lease, then dispatches each canonical target once.
func (s *Session) ExecuteNodeAction(ctx context.Context, plan *PreparedNodeAction, confirmation Confirmation, onTarget func(TargetResult)) (MutationResult, error) {
	if plan == nil || plan.state == nil || plan.state.nodeRequest == nil || plan.state.nodeFacts == nil {
		return MutationResult{}, ErrInvalidPlan
	}
	request := cloneNodeActionRequest(*plan.state.nodeRequest)
	if request.Kind == ActionUpgrade {
		return MutationResult{}, errors.New("upgrade execution is handled by the streaming upgrade operation")
	}
	timeout := controlOperationTimeout
	if request.Kind == ActionReset || request.Kind == ActionWipeDevice {
		timeout = destructiveNodeTimeout
	}
	operationCtx, release, err := s.admit(ctx, plan.state, confirmation, timeout)
	if err != nil {
		return MutationResult{}, err
	}
	defer release()
	if s.sdkSession == nil || s.sdkSession.NodeActions() == nil {
		return MutationResult{}, ErrCapabilityUnavailable
	}
	readCtx, cancel := context.WithTimeout(operationCtx, unaryReadTimeout)
	defer cancel()
	nodes, readErr := readNodes(readCtx, s.sdkSession)
	if readErr != nil {
		if readCtx.Err() != nil {
			return MutationResult{}, errors.Join(readCtx.Err(), readErr)
		}
		return MutationResult{}, errors.Join(ErrAssessmentChanged, readErr)
	}
	resolved := make([]domain.NodeSnapshot, 0, len(request.Targets))
	for _, target := range request.Targets {
		node, ok := resolveNodeTarget(nodes.Nodes, target)
		if !ok {
			return MutationResult{}, ErrAssessmentChanged
		}
		if !knownNodeActionRole(node.Role) {
			return MutationResult{}, ErrAssessmentChanged
		}
		resolved = append(resolved, node)
	}
	currentFacts, readErr := s.readNodeActionFacts(readCtx, nodes.Nodes, resolved, request)
	if readErr != nil {
		if err := readCtx.Err(); err != nil {
			return MutationResult{}, errors.Join(err, readErr)
		}
		return MutationResult{}, errors.Join(ErrAssessmentChanged, readErr)
	}
	if err := readCtx.Err(); err != nil {
		return MutationResult{}, err
	}
	if request.Kind == ActionReset || request.Kind == ActionWipeDevice {
		currentDisks, diskErr := s.readDestructiveDiskFacts(readCtx, resolved, request)
		if diskErr != nil {
			return MutationResult{}, diskErr
		}
		currentFacts.Disks = &currentDisks
		currentFacts.Blocked = appendActionBlock(currentFacts.Blocked, currentDisks.Blocked)
	}
	if currentFacts.Blocked != "" {
		return MutationResult{}, ErrActionBlocked
	}
	if !sameNodeActionFacts(*plan.state.nodeFacts, currentFacts) {
		return MutationResult{}, ErrAssessmentChanged
	}
	if err := operationCtx.Err(); err != nil {
		return MutationResult{}, err
	}

	type targetExecution struct {
		index  int
		result TargetResult
	}
	completed := make(chan targetExecution, len(request.Targets))
	for index, target := range request.Targets {
		go func(index int, target string) {
			result := dispatchPreparedNodeAction(operationCtx, s.sdkSession.NodeActions(), request, currentFacts.Disks, target)
			completed <- targetExecution{index: index, result: result}
		}(index, target)
	}
	results := make([]TargetResult, len(request.Targets))
	for range len(request.Targets) {
		item := <-completed
		results[item.index] = item.result
		if onTarget != nil {
			onTarget(item.result)
		}
	}
	mutation := MutationResult{Targets: results}
	var targetErrors []error
	for _, result := range results {
		if result.Err != nil {
			targetErrors = append(targetErrors, result.Err)
		}
	}
	return mutation, errors.Join(targetErrors...)
}

func validateNodeActionRequest(request NodeActionRequest) error {
	switch request.Kind {
	case ActionReboot, ActionShutdown, ActionRollback:
		if request.Reset != nil || request.DeviceWipe != nil {
			return errors.New("reset and device-wipe options require their matching action kind")
		}
		if strings.TrimSpace(request.Image) != "" {
			return errors.New("image is only valid for upgrade actions")
		}
	case ActionUpgrade:
		if request.Reset != nil || request.DeviceWipe != nil {
			return errors.New("reset and device-wipe options require their matching action kind")
		}
		if len(request.Targets) != 1 {
			return errors.New("upgrade requires exactly one target")
		}
		if strings.TrimSpace(request.Image) == "" {
			return errors.New("upgrade requires an explicit image")
		}
	case ActionReset:
		if request.Reset == nil || request.DeviceWipe != nil {
			return errors.New("reset requires reset options only")
		}
		if strings.TrimSpace(request.Image) != "" {
			return errors.New("image is only valid for upgrade actions")
		}
		if request.Reset.Mode < ports.WipeModeAll || request.Reset.Mode > ports.WipeModeUserDisks {
			return fmt.Errorf("unsupported reset mode %d", request.Reset.Mode)
		}
		if request.Reset.Mode != ports.WipeModeSystemDisk && len(request.Targets) != 1 {
			return errors.New("all-disk and user-disk reset modes are single-node only")
		}
		if request.Reset.Mode == ports.WipeModeSystemDisk && len(request.Reset.UserDisks) > 0 {
			return errors.New("system-disk reset cannot include user disks")
		}
		if request.Reset.Mode != ports.WipeModeSystemDisk && len(request.Reset.SystemPartitions) > 0 {
			return errors.New("system partitions require system-disk reset mode")
		}
	case ActionWipeDevice:
		if request.DeviceWipe == nil || request.Reset != nil {
			return errors.New("device wipe requires device-wipe options only")
		}
		if strings.TrimSpace(request.Image) != "" {
			return errors.New("image is only valid for upgrade actions")
		}
		if len(request.Targets) != 1 {
			return errors.New("device wipe requires exactly one target")
		}
		if strings.TrimSpace(request.DeviceWipe.Node) == "" || strings.TrimSpace(request.DeviceWipe.Device) == "" {
			return errors.New("device wipe requires a node and device")
		}
		if request.DeviceWipe.Method != ports.DeviceWipeFast && request.DeviceWipe.Method != ports.DeviceWipeZeroes {
			return fmt.Errorf("unsupported device-wipe method %d", request.DeviceWipe.Method)
		}
	default:
		return fmt.Errorf("unsupported node action kind %q", request.Kind)
	}
	if len(request.Targets) == 0 {
		return errors.New("node action requires at least one target")
	}
	for _, target := range request.Targets {
		if strings.TrimSpace(target) == "" {
			return errors.New("node action target cannot be empty")
		}
	}
	return nil
}

func resolveNodeActionTargets(nodes []domain.NodeSnapshot, requested []string) ([]string, []domain.NodeSnapshot, error) {
	targets := make([]string, 0, len(requested))
	resolved := make([]domain.NodeSnapshot, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, identity := range requested {
		node, ok := resolveNodeTarget(nodes, identity)
		if !ok {
			return nil, nil, fmt.Errorf("node target %q does not resolve to one canonical node", identity)
		}
		if !knownNodeActionRole(node.Role) {
			return nil, nil, fmt.Errorf("node target %q has an unknown role; refusing node action", identity)
		}
		target := node.Target()
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
		resolved = append(resolved, node)
	}
	if len(targets) == 0 {
		return nil, nil, errors.New("node action requires at least one target")
	}
	return targets, resolved, nil
}

func knownNodeActionRole(role domain.NodeRole) bool {
	return role == domain.NodeRoleControl || role == domain.NodeRoleWorker
}

func (s *Session) readNodeActionFacts(ctx context.Context, allNodes, targets []domain.NodeSnapshot, request NodeActionRequest) (nodeActionFacts, error) {
	facts := nodeActionFacts{Nodes: make([]nodeIdentityFacts, 0, len(targets))}
	targetNames := make([]string, 0, len(targets))
	hasControlPlane := false
	for _, node := range targets {
		version := ""
		if request.Kind == ActionUpgrade {
			version = node.Version
		}
		facts.Nodes = append(facts.Nodes, nodeIdentityFacts{ID: node.ID, Name: node.Name, Target: node.Target(), Role: node.Role, Version: version})
		targetNames = append(targetNames, node.Target())
		hasControlPlane = hasControlPlane || node.Role == domain.NodeRoleControl
	}
	if !hasControlPlane {
		if request.Kind == ActionUpgrade {
			facts.Warning = upgradeActionWarningFacts(allNodes, EtcdPolicyFacts{}, targetNames, request.Image)
		}
		if request.Kind == ActionReset && request.Reset.Graceful {
			facts.Warning = appendWarning(facts.Warning, "node(s) leave etcd before reset")
		}
		return facts, nil
	}
	if request.Kind == ActionWipeDevice {
		return facts, nil
	}
	if err := ctx.Err(); err != nil {
		return facts, err
	}
	etcdFacts := EtcdPolicyFacts{}
	if s.sdkSession != nil && s.sdkSession.Etcd() != nil {
		controlPlanes := controlPlaneHostnames(allNodes)
		if len(controlPlanes) > 0 {
			members, err := s.sdkSession.Etcd().List(ctx, controlPlanes)
			if err == nil {
				etcdFacts = EtcdPolicyFacts{Available: true, Members: members.Members}
			} else if ctx.Err() != nil {
				return facts, errors.Join(ctx.Err(), err)
			} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return facts, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return facts, err
	}
	if request.Kind == ActionUpgrade {
		facts.Warning = upgradeActionWarningFacts(allNodes, etcdFacts, targetNames, request.Image)
		facts.Blocked = etcdQuorumBlockReasonFacts(etcdFacts, targetNames)
	} else if request.Kind == ActionReset {
		facts.Warning = etcdQuorumWarningFacts(etcdFacts, targetNames)
		if request.Reset.Graceful {
			facts.Warning = appendWarning(facts.Warning, "node(s) leave etcd before reset")
		}
		facts.Blocked = resetQuorumBlockReasonFacts(allNodes, etcdFacts, targetNames)
		facts.ResetQuorum = assessEtcdQuorumFacts(etcdFacts, targetNames)
		facts.HasQuorum = true
	} else {
		facts.Warning = etcdQuorumWarningFacts(etcdFacts, targetNames)
		facts.Blocked = etcdQuorumBlockReasonFacts(etcdFacts, targetNames)
	}
	return facts, nil
}

func sameNodeActionFacts(prepared, current nodeActionFacts) bool {
	if !slices.Equal(prepared.Nodes, current.Nodes) || prepared.Warning != current.Warning || prepared.Blocked != current.Blocked {
		return false
	}
	if prepared.HasQuorum != current.HasQuorum || (prepared.HasQuorum && prepared.ResetQuorum != current.ResetQuorum) {
		return false
	}
	return sameDestructiveDiskFacts(prepared.Disks, current.Disks)
}

func dispatchNodeAction(ctx context.Context, controller ports.NodeController, kind ActionKind, target string) TargetResult {
	result := TargetResult{Target: target}
	var err error
	switch kind {
	case ActionReboot:
		if err := ctx.Err(); err != nil {
			result.Err = err
			return result
		}
		err = controller.Reboot(ctx, target, ports.RebootDefault)
	case ActionShutdown:
		if err := ctx.Err(); err != nil {
			result.Err = err
			return result
		}
		err = controller.Shutdown(ctx, target, false)
	case ActionRollback:
		if err := ctx.Err(); err != nil {
			result.Err = err
			return result
		}
		err = controller.Rollback(ctx, target)
	default:
		err = fmt.Errorf("unsupported node action kind %q", kind)
	}
	if err == nil {
		result.Outcome = domain.OperationAccepted
		return result
	}
	if ctx.Err() != nil {
		err = mutationCallError(ctx, err)
	}
	result.Outcome = domain.OperationUnconfirmed
	result.Err = err
	return result
}
