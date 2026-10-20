package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

const kubernetesUpgradePlanLifetime = 5 * time.Minute

type kubernetesUpgradeRequest struct {
	target string
}

type kubernetesUpgradeFacts struct {
	plan      domain.KubernetesUpgradePlan
	expiresAt time.Time
}

// Summary returns only the safe plan fields; mutable slices are copied.
func (p *PreparedKubernetesUpgrade) Summary() domain.KubernetesUpgradePlan {
	if p == nil || p.state == nil || p.state.kubernetesUpgradePlan == nil {
		return domain.KubernetesUpgradePlan{}
	}
	return cloneKubernetesUpgradePlan(*p.state.kubernetesUpgradePlan)
}

func (s *Session) PrepareKubernetesUpgrade(ctx context.Context, target string) (*PreparedKubernetesUpgrade, error) {
	if s == nil {
		return nil, ErrSessionClosed
	}
	if s.sdkSession == nil || s.sdkSession.KubernetesUpgrade() == nil {
		return nil, ErrCapabilityUnavailable
	}
	readCtx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return nil, err
	}
	defer cancel()
	inventory, err := s.sdkSession.KubernetesUpgrade().Inspect(readCtx, target)
	if err != nil {
		return nil, safeKubernetesUpgradeError(err)
	}
	s.applyKubernetesUpgradeManifestEvidence(&inventory, target)
	plan, err := domain.AssessKubernetesUpgrade(inventory, target)
	if err != nil {
		return nil, safeKubernetesUpgradeError(err)
	}
	plan.Fingerprint, err = kubernetesUpgradeFingerprint(plan)
	if err != nil {
		return nil, ErrActionBlocked
	}
	s.mu.Lock()
	if s.closed || s.lifetimeCtx.Err() != nil {
		s.mu.Unlock()
		return nil, ErrSessionClosed
	}
	if s.clusterUID != "" && s.clusterUID != plan.ClusterUID {
		s.mu.Unlock()
		return nil, ErrAssessmentChanged
	}
	s.clusterUID = plan.ClusterUID
	s.mu.Unlock()
	request := &plan.TargetVersion
	phrase := fmt.Sprintf("upgrade %s to %s", plan.ClusterUID, plan.TargetVersion)
	state := &preparedState{
		session: s,
		assessment: Assessment{Targets: []string{plan.ClusterUID}, RequiredToken: phrase,
			Warning: "Kubernetes components and kubelet services restart sequentially; node scheduling stays unchanged."},
		kubernetesUpgradePlan: &plan, kubernetesUpgradeRequest: request,
	}
	state.kubernetesUpgradeFacts = &kubernetesUpgradeFacts{plan: cloneKubernetesUpgradePlan(plan), expiresAt: time.Now().Add(kubernetesUpgradePlanLifetime)}
	prepared := &PreparedKubernetesUpgrade{state: state}
	return prepared, nil
}

func (s *Session) applyKubernetesUpgradeManifestEvidence(inventory *domain.KubernetesUpgradeInventory, target string) {
	if inventory == nil {
		return
	}
	s.mu.Lock()
	if s.kubernetesUpgradeManifests != nil && s.kubernetesUpgradeManifests[kubernetesUpgradeStageKey(inventory.ClusterUID, target)] {
		// This session observed a successful CLI exit and verified every
		// control-plane component at the target after managed-manifest sync.
		inventory.ManifestsConverged = true
	}
	s.mu.Unlock()
}

func kubernetesUpgradeFingerprint(plan domain.KubernetesUpgradePlan) (string, error) {
	plan.Fingerprint = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func kubernetesUpgradeStageKey(clusterUID, target string) string { return clusterUID + "\x00" + target }

func cloneKubernetesUpgradePlan(plan domain.KubernetesUpgradePlan) domain.KubernetesUpgradePlan {
	plan.Nodes = append([]domain.KubernetesUpgradeNodePlan(nil), plan.Nodes...)
	plan.ProxyVersions = append([]domain.KubernetesProxyNodeVersion(nil), plan.ProxyVersions...)
	plan.Manifests = append([]string(nil), plan.Manifests...)
	return plan
}

func sameKubernetesUpgradePlan(left, right domain.KubernetesUpgradePlan) bool {
	left.Fingerprint, right.Fingerprint = "", ""
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftBytes) == string(rightBytes)
}

func safeKubernetesUpgradeError(err error) error {
	if err == nil {
		return ErrActionBlocked
	}

	// The Talos backend returns fixed, sanitized preflight messages. Preserve
	// those explanations for the operator while retaining the blocked-action
	// sentinel for callers that rely on errors.Is.
	return fmt.Errorf("%w: %s", ErrActionBlocked, err)
}

type kubernetesUpgradeStream struct {
	events chan domain.KubernetesUpgradeEvent
	done   chan struct{}
	cancel context.CancelFunc
	mu     sync.Mutex
	result domain.KubernetesUpgradeResult
	err    error
}

func (s *kubernetesUpgradeStream) Events() <-chan domain.KubernetesUpgradeEvent { return s.events }
func (s *kubernetesUpgradeStream) Cancel()                                      { s.cancel() }
func (s *kubernetesUpgradeStream) Wait(ctx context.Context) (domain.KubernetesUpgradeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return cloneKubernetesUpgradeResult(s.result), s.err
	case <-ctx.Done():
		return domain.KubernetesUpgradeResult{}, ctx.Err()
	}
}

func cloneKubernetesUpgradeResult(result domain.KubernetesUpgradeResult) domain.KubernetesUpgradeResult {
	result.Nodes = append([]domain.KubernetesUpgradeNodeResult(nil), result.Nodes...)
	if result.Snapshot != nil {
		receipt := *result.Snapshot
		result.Snapshot = &receipt
	}
	return result
}

func (s *Session) ExecuteKubernetesUpgrade(ctx context.Context, prepared *PreparedKubernetesUpgrade, confirmation Confirmation, snapshotPath string) (ports.KubernetesUpgradeStream, error) {
	if prepared == nil || prepared.state == nil || prepared.state.kubernetesUpgradeFacts == nil || prepared.state.kubernetesUpgradeRequest == nil {
		return nil, ErrInvalidPlan
	}
	state := prepared.state
	if time.Now().After(state.kubernetesUpgradeFacts.expiresAt) {
		return nil, ErrInvalidPlan
	}
	if err := ValidateEtcdSnapshotPath(snapshotPath); err != nil {
		return nil, err
	}
	opCtx, release, err := s.admit(ctx, state, confirmation, 0)
	if err != nil {
		return nil, err
	}
	if time.Now().After(state.kubernetesUpgradeFacts.expiresAt) {
		release()
		return nil, ErrInvalidPlan
	}
	if s.sdkSession == nil || s.sdkSession.KubernetesUpgrade() == nil || s.sdkSession.EtcdOperations() == nil {
		release()
		return nil, ErrCapabilityUnavailable
	}
	workCtx, cancel := context.WithCancel(opCtx)
	stream := &kubernetesUpgradeStream{events: make(chan domain.KubernetesUpgradeEvent, 64), done: make(chan struct{}), cancel: cancel}
	go func() {
		result, runErr := s.runKubernetesUpgrade(workCtx, state.kubernetesUpgradeFacts.plan, snapshotPath, stream.events)
		stream.mu.Lock()
		stream.result, stream.err = result, runErr
		stream.mu.Unlock()
		close(stream.events)
		release()
		close(stream.done)
	}()
	return stream, nil
}

func (s *Session) runKubernetesUpgrade(ctx context.Context, plan domain.KubernetesUpgradePlan, snapshotPath string, events chan<- domain.KubernetesUpgradeEvent) (domain.KubernetesUpgradeResult, error) {
	result := domain.KubernetesUpgradeResult{Outcome: domain.OperationNotAttempted}
	backend := s.sdkSession.KubernetesUpgrade()
	emit := func(event domain.KubernetesUpgradeEvent) {
		select {
		case events <- event:
		default:
		}
	}
	inspect := func(readCtx context.Context) (domain.KubernetesUpgradeInventory, error) {
		inventory, err := backend.Inspect(readCtx, plan.TargetVersion)
		if err == nil {
			s.applyKubernetesUpgradeManifestEvidence(&inventory, plan.TargetVersion)
		}
		return inventory, err
	}
	current, err := inspect(ctx)
	if err != nil {
		return result, safeKubernetesUpgradeError(err)
	}
	currentPlan, err := domain.AssessKubernetesUpgrade(current, plan.TargetVersion)
	if err != nil || !sameKubernetesUpgradePlan(plan, currentPlan) {
		return result, ErrAssessmentChanged
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	snapshot, _, err := s.executeEtcdSnapshot(ctx, plan.Gateway, snapshotPath)
	if snapshot != nil {
		result.Snapshot = snapshot
	}
	if err != nil {
		return result, errors.New("verified etcd snapshot failed; upgrade was not started")
	}
	emit(domain.KubernetesUpgradeEvent{Stage: domain.KubernetesUpgradeSnapshot, Status: "verified"})
	current, err = inspect(ctx)
	if err != nil {
		return result, safeKubernetesUpgradeError(err)
	}
	currentPlan, err = domain.AssessKubernetesUpgrade(current, plan.TargetVersion)
	if err != nil || !sameKubernetesUpgradePlan(plan, currentPlan) {
		return result, ErrAssessmentChanged
	}
	if !plan.SkipControlPlane {
		if err := backend.RunControlPlane(ctx, plan, emit); err != nil {
			result.Outcome = domain.OperationUnconfirmed
			return result, errors.New("Talos CLI upgrade failed or was interrupted; cluster state may be partial")
		}
	}
	emit(domain.KubernetesUpgradeEvent{Stage: domain.KubernetesUpgradeVerify, Status: "checking-control-plane"})
	afterCLI, err := inspect(ctx)
	if err != nil || !controlPlaneReachedTarget(afterCLI, plan) {
		result.Outcome = domain.OperationUnconfirmed
		return result, errors.New("control-plane or manifest convergence could not be verified")
	}
	if !plan.SkipControlPlane {
		s.mu.Lock()
		if s.kubernetesUpgradeManifests == nil {
			s.kubernetesUpgradeManifests = make(map[string]bool)
		}
		s.kubernetesUpgradeManifests[kubernetesUpgradeStageKey(plan.ClusterUID, plan.TargetVersion)] = true
		s.mu.Unlock()
	}
	for index, original := range plan.Nodes {
		if err := ctx.Err(); err != nil {
			result.Outcome = domain.OperationUnconfirmed
			appendNotAttempted(&result, plan.Nodes, len(result.Nodes))
			return result, err
		}
		liveInventory, inspectErr := waitForUpgradeInventory(ctx, inspect, plan, afterCLI, index, emit)
		if inspectErr != nil {
			result.Outcome = domain.OperationUnconfirmed
			appendNotAttempted(&result, plan.Nodes, len(result.Nodes))
			result.Nodes[index].Message = fmt.Sprintf("Readiness check before %s stopped: %s", original.Name, inspectErr)
			return result, inspectErr
		}
		baseline := findUpgradeNode(afterCLI.Nodes, original.UID)
		fresh := findUpgradeNode(liveInventory.Nodes, original.UID)
		if baseline == nil || fresh == nil || fresh.Name != original.Name || fresh.TalosID != original.TalosID || fresh.TalosAddress != original.TalosAddress || fresh.Role != original.Role || fresh.TalosVersion != original.TalosVersion || !fresh.Ready {
			result.Outcome = domain.OperationUnconfirmed
			appendNotAttempted(&result, plan.Nodes, len(result.Nodes))
			return result, ErrAssessmentChanged
		}
		if fresh.Unschedulable != original.OriginallyCordoned {
			result.Outcome = domain.OperationUnconfirmed
			appendNotAttempted(&result, plan.Nodes, len(result.Nodes))
			return result, ErrAssessmentChanged
		}
		if fresh.KubeletVersion == plan.TargetVersion {
			if fresh.KubeletImage != fresh.TargetKubeletImage {
				result.Outcome = domain.OperationUnconfirmed
				appendNotAttempted(&result, plan.Nodes, len(result.Nodes))
				return result, ErrAssessmentChanged
			}
			result.Nodes = append(result.Nodes, domain.KubernetesUpgradeNodeResult{NodeUID: original.UID, Outcome: domain.OperationCompleted})
			continue
		}
		if fresh.KubeletVersion != baseline.KubeletVersion || fresh.KubeletImage != baseline.KubeletImage || fresh.ConfigFingerprint != baseline.ConfigFingerprint {
			result.Outcome = domain.OperationUnconfirmed
			appendNotAttempted(&result, plan.Nodes, len(result.Nodes))
			return result, ErrAssessmentChanged
		}
		nodePlan := original
		nodePlan.SourceVersion = fresh.KubeletVersion
		nodePlan.SourceKubeletImage = fresh.KubeletImage
		nodePlan.TargetKubeletImage = fresh.TargetKubeletImage
		nodePlan.ConfigFingerprint = fresh.ConfigFingerprint
		nodePlan.OriginallyCordoned = fresh.Unschedulable
		nodeResult, updateErr := backend.UpgradeKubelet(ctx, nodePlan, emit)
		if updateErr == nil && (nodeResult.Outcome != domain.OperationCompleted || nodeResult.ConfigFingerprint == "") {
			nodeResult.Outcome = domain.OperationUnconfirmed
			updateErr = errors.New("completed kubelet configuration could not be verified")
		}
		if updateErr != nil {
			nodeResult.Message = updateErr.Error()
		}
		result.Nodes = append(result.Nodes, nodeResult)
		if updateErr != nil {
			result.Outcome = domain.OperationUnconfirmed
			appendNotAttempted(&result, plan.Nodes, len(result.Nodes))
			return result, errors.New("kubelet rollout stopped; review the node outcomes before retry")
		}
		baseline.ConfigFingerprint = nodeResult.ConfigFingerprint
		baseline.KubeletVersion = plan.TargetVersion
		baseline.KubeletImage = fresh.TargetKubeletImage
	}
	final, err := waitForUpgradeInventory(ctx, inspect, plan, afterCLI, len(plan.Nodes), emit)
	if err != nil || !allKubeletsReachedTarget(final, plan) {
		result.Outcome = domain.OperationUnconfirmed
		return result, errors.New("final Kubernetes version and readiness check failed")
	}
	result.Outcome = domain.OperationCompleted
	return result, nil
}

// Read-only stabilization never retries a node mutation. Every successful
// observation must retain cluster/node identity, scheduling state, and pending
// node configuration before readiness can permit the next mutation.
func waitForUpgradeInventory(ctx context.Context, inspect func(context.Context) (domain.KubernetesUpgradeInventory, error), plan domain.KubernetesUpgradePlan, baseline domain.KubernetesUpgradeInventory, next int, emit func(domain.KubernetesUpgradeEvent)) (domain.KubernetesUpgradeInventory, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return domain.KubernetesUpgradeInventory{}, err
		}
		inventory, err := inspect(waitCtx)
		if err == nil {
			if inventory.ClusterUID != plan.ClusterUID || inventory.TalosClusterID != plan.TalosClusterID || len(inventory.Nodes) != len(plan.Nodes) {
				return inventory, fmt.Errorf("%w: cluster identity or node membership changed", ErrAssessmentChanged)
			}
			for index, planned := range plan.Nodes {
				node := findUpgradeNode(inventory.Nodes, planned.UID)
				if node == nil || node.Name != planned.Name || node.TalosID != planned.TalosID || node.TalosAddress != planned.TalosAddress || node.Role != planned.Role || node.TalosVersion != planned.TalosVersion || node.Unschedulable != planned.OriginallyCordoned {
					return inventory, fmt.Errorf("%w: identity or scheduling state changed for %s", ErrAssessmentChanged, planned.Name)
				}
				if index < next && (node.KubeletVersion != plan.TargetVersion || node.KubeletImage != node.TargetKubeletImage) {
					return inventory, fmt.Errorf("%w: completed kubelet changed for %s", ErrAssessmentChanged, planned.Name)
				}
				before := findUpgradeNode(baseline.Nodes, planned.UID)
				if before == nil || node.KubeletVersion != before.KubeletVersion || node.KubeletImage != before.KubeletImage || node.ConfigFingerprint != before.ConfigFingerprint {
					return inventory, fmt.Errorf("%w: kubelet configuration changed for %s", ErrAssessmentChanged, planned.Name)
				}
			}
			ready := inventory
			ready.Nodes = append([]domain.KubernetesUpgradeNode(nil), inventory.Nodes...)
			ready.Healthy, ready.ProxyRolloutComplete = true, true
			for index := range ready.Nodes {
				ready.Nodes[index].Ready = true
			}
			if !controlPlaneReachedTarget(ready, plan) {
				return inventory, fmt.Errorf("%w: control-plane version or configuration changed", ErrAssessmentChanged)
			}
			if controlPlaneReachedTarget(inventory, plan) {
				return inventory, nil
			}
			err = domain.ErrKubernetesUpgradeNotReady
		}
		if !errors.Is(err, domain.ErrKubernetesUpgradeNotReady) {
			return inventory, err
		}
		lastErr = err
		emit(domain.KubernetesUpgradeEvent{Stage: domain.KubernetesUpgradeVerify, Status: lastErr.Error()})
		select {
		case <-waitCtx.Done():
			return inventory, fmt.Errorf("cluster readiness did not settle: %w", lastErr)
		case <-time.After(3 * time.Second):
		}
	}
}

func controlPlaneReachedTarget(inventory domain.KubernetesUpgradeInventory, plan domain.KubernetesUpgradePlan) bool {
	if inventory.ClusterUID != plan.ClusterUID || inventory.TalosClusterID != plan.TalosClusterID || len(inventory.Nodes) != len(plan.Nodes) || !inventory.ProxyConfigKnown || inventory.ProxyEnabled != plan.ProxyEnabled {
		return false
	}
	for _, node := range inventory.Nodes {
		if node.Role == domain.NodeRoleControl && (!node.ProxyConfigKnown || node.ProxyEnabled != plan.ProxyEnabled || node.ProxyConfigImage != plan.TargetProxyImage || node.APIServerVersion != plan.TargetVersion || node.ControllerManagerVersion != plan.TargetVersion || node.SchedulerVersion != plan.TargetVersion) {
			return false
		}
		if !node.Ready {
			return false
		}
	}
	if plan.ProxyEnabled {
		if !inventory.ProxyRolloutComplete {
			return false
		}
		if len(inventory.ProxyVersions) != len(plan.Nodes) {
			return false
		}
		plannedNodes := make(map[string]string, len(plan.Nodes))
		for _, node := range plan.Nodes {
			plannedNodes[node.UID] = node.Name
		}
		seenProxyNodes := make(map[string]struct{}, len(inventory.ProxyVersions))
		for _, proxy := range inventory.ProxyVersions {
			if proxy.Version != plan.TargetVersion || plannedNodes[proxy.NodeUID] != proxy.Name {
				return false
			}
			if _, exists := seenProxyNodes[proxy.NodeUID]; exists {
				return false
			}
			seenProxyNodes[proxy.NodeUID] = struct{}{}
		}
		if len(seenProxyNodes) != len(plan.Nodes) {
			return false
		}
	} else if len(inventory.ProxyVersions) != 0 {
		return false
	}
	for _, planned := range plan.Nodes {
		if findUpgradeNode(inventory.Nodes, planned.UID) == nil {
			return false
		}
	}
	return inventory.Healthy
}

func allKubeletsReachedTarget(inventory domain.KubernetesUpgradeInventory, plan domain.KubernetesUpgradePlan) bool {
	if inventory.ClusterUID != plan.ClusterUID || inventory.TalosClusterID != plan.TalosClusterID || len(inventory.Nodes) != len(plan.Nodes) || !controlPlaneReachedTarget(inventory, plan) {
		return false
	}
	for _, planned := range plan.Nodes {
		current := findUpgradeNode(inventory.Nodes, planned.UID)
		if current == nil || current.Name != planned.Name || current.KubeletVersion != plan.TargetVersion || current.KubeletImage != planned.TargetKubeletImage || !current.Ready {
			return false
		}
		if current.Unschedulable != planned.OriginallyCordoned {
			return false
		}
	}
	return true
}

func findUpgradeNode(nodes []domain.KubernetesUpgradeNode, uid string) *domain.KubernetesUpgradeNode {
	for i := range nodes {
		if nodes[i].UID == uid {
			return &nodes[i]
		}
	}
	return nil
}

func appendNotAttempted(result *domain.KubernetesUpgradeResult, plans []domain.KubernetesUpgradeNodePlan, from int) {
	for _, plan := range plans[from:] {
		result.Nodes = append(result.Nodes, domain.KubernetesUpgradeNodeResult{NodeUID: plan.UID, Outcome: domain.OperationNotAttempted})
	}
}
