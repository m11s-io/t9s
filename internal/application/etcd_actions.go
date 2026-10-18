package application

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/m11s-io/t9s/internal/domain"
)

type etcdActionFacts struct {
	nodeID       string
	nodeName     string
	nodeTarget   string
	nodeRole     domain.NodeRole
	memberID     uint64
	memberHost   string
	hasMember    bool
	snapshotNode string
	rpcNode      string
	warning      string
	blocked      string
	quorum       etcdQuorumAssessment
}

// PrepareEtcdAction resolves the requested node/member identities and records
// the membership and quorum facts that execution must recheck.
func (s *Session) PrepareEtcdAction(ctx context.Context, request EtcdActionRequest) (*PreparedEtcdAction, error) {
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
	if err := validateEtcdActionRequest(request); err != nil {
		return nil, err
	}
	if s.sdkSession == nil || s.sdkSession.EtcdOperations() == nil {
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
	node, ok := resolveNodeTarget(nodes.Nodes, request.Node)
	if !ok || node.Role != domain.NodeRoleControl {
		return nil, fmt.Errorf("etcd action node %q does not resolve to one control-plane node", request.Node)
	}
	request.Node = node.Target()
	facts, err := s.readEtcdActionFacts(readCtx, nodes.Nodes, node, request)
	if err != nil {
		return nil, err
	}
	if err := readCtx.Err(); err != nil {
		return nil, err
	}
	if facts.hasMember {
		request.MemberID = facts.memberID
		request.MemberHostname = facts.memberHost
	}
	assessment := Assessment{Targets: []string{request.Node}, Warning: facts.warning, Blocked: facts.blocked, SnapshotPath: request.SnapshotPath}
	plan := newPreparedEtcdAction(s, request, assessment)
	plan.state.etcdFacts = &facts
	return &plan, nil
}

// ExecuteEtcdAction consumes a prepared plan, rechecks its canonical safety
// facts under the session mutation lease, and performs a snapshot before any
// membership change. The lease spans every stage, including the local write.
func (s *Session) ExecuteEtcdAction(ctx context.Context, plan *PreparedEtcdAction, confirmation Confirmation) (MutationResult, error) {
	if plan == nil || plan.state == nil || plan.state.etcdRequest == nil || plan.state.etcdFacts == nil {
		return MutationResult{}, ErrInvalidPlan
	}
	request := *plan.state.etcdRequest
	budget := controlOperationTimeout
	switch request.Kind {
	case EtcdActionSnapshot:
		budget = snapshotTimeout
	case EtcdActionRemoveMember, EtcdActionLeaveCluster:
		budget = snapshotTimeout + controlOperationTimeout + 2*unaryReadTimeout
	case EtcdActionDefragment:
		budget = defragmentTimeout
	}
	operationCtx, release, err := s.admit(ctx, plan.state, confirmation, budget)
	if err != nil {
		return MutationResult{}, err
	}
	defer release()
	if s.sdkSession == nil || s.sdkSession.EtcdOperations() == nil {
		return MutationResult{}, ErrCapabilityUnavailable
	}
	current, err := s.readCurrentEtcdActionFacts(operationCtx, request)
	if err != nil {
		return MutationResult{}, err
	}
	if current.blocked != "" {
		return MutationResult{}, ErrActionBlocked
	}
	if !sameEtcdActionFacts(*plan.state.etcdFacts, current) {
		return MutationResult{}, ErrAssessmentChanged
	}
	if err := operationCtx.Err(); err != nil {
		return MutationResult{}, err
	}
	if request.Kind == EtcdActionSnapshot {
		_, result, snapshotErr := s.executeEtcdSnapshot(operationCtx, request.Node, request.SnapshotPath)
		return result, snapshotErr
	}
	var snapshotResult *domain.EtcdSnapshotResult
	if isDestructiveEtcdMembership(request.Kind) {
		snapshot, result, snapshotErr := s.executeEtcdSnapshot(operationCtx, current.snapshotNode, request.SnapshotPath)
		if snapshotErr != nil {
			return result, snapshotErr
		}
		snapshotResult = snapshot
		result = MutationResult{Snapshot: snapshot}
		if err := operationCtx.Err(); err != nil {
			return result, err
		}
		postSnapshot, readErr := s.readCurrentEtcdActionFacts(operationCtx, request)
		if readErr != nil {
			return result, readErr
		}
		if postSnapshot.blocked != "" {
			return result, ErrActionBlocked
		}
		if !sameEtcdActionFacts(current, postSnapshot) {
			return result, ErrAssessmentChanged
		}
		current = postSnapshot
	}
	if err := operationCtx.Err(); err != nil {
		return MutationResult{Snapshot: snapshotResult}, err
	}
	target := current.rpcNode
	if target == "" {
		target = request.Node
	}
	stageTimeout := controlOperationTimeout
	if request.Kind == EtcdActionDefragment {
		stageTimeout = defragmentTimeout
	}
	stageCtx, stageCancel := context.WithTimeout(operationCtx, stageTimeout)
	defer stageCancel()
	if err := stageCtx.Err(); err != nil {
		return MutationResult{Snapshot: snapshotResult}, err
	}
	var rpcErr error
	switch request.Kind {
	case EtcdActionRemoveMember:
		rpcErr = s.sdkSession.EtcdOperations().RemoveMemberByID(stageCtx, target, request.MemberID)
	case EtcdActionLeaveCluster:
		rpcErr = s.sdkSession.EtcdOperations().LeaveCluster(stageCtx, target)
	case EtcdActionDefragment:
		rpcErr = s.sdkSession.EtcdOperations().Defragment(stageCtx, target)
	case EtcdActionDisarmAlarms:
		rpcErr = s.sdkSession.EtcdOperations().DisarmAlarms(stageCtx, target)
	case EtcdActionForfeitLeadership:
		rpcErr = s.sdkSession.EtcdOperations().ForfeitLeadership(stageCtx, target)
	default:
		return MutationResult{}, fmt.Errorf("unsupported etcd action kind %q", request.Kind)
	}
	outcome := domain.OperationAccepted
	if rpcErr != nil {
		if stageCtx.Err() != nil {
			rpcErr = mutationCallError(stageCtx, rpcErr)
		}
		outcome = domain.OperationUnconfirmed
	}
	result := MutationResult{Targets: []TargetResult{{Target: target, Outcome: outcome, Err: rpcErr, Warning: current.warning}}, Snapshot: snapshotResult}
	if rpcErr != nil {
		return result, rpcErr
	}
	return result, nil
}

func validateEtcdActionRequest(request EtcdActionRequest) error {
	if strings.TrimSpace(request.Node) == "" {
		return errors.New("etcd action requires a node identity")
	}
	switch request.Kind {
	case EtcdActionSnapshot, EtcdActionRemoveMember, EtcdActionLeaveCluster:
		if err := ValidateEtcdSnapshotPath(request.SnapshotPath); err != nil {
			return err
		}
	case EtcdActionDefragment, EtcdActionDisarmAlarms, EtcdActionForfeitLeadership:
		if request.SnapshotPath != "" {
			return errors.New("snapshot path only applies to snapshot and membership actions")
		}
	default:
		return fmt.Errorf("unsupported etcd action kind %q", request.Kind)
	}
	if request.Kind == EtcdActionRemoveMember || request.Kind == EtcdActionLeaveCluster || request.Kind == EtcdActionForfeitLeadership || (request.Kind == EtcdActionSnapshot && (request.MemberID != 0 || request.MemberHostname != "")) {
		if request.MemberID == 0 && strings.TrimSpace(request.MemberHostname) == "" {
			return errors.New("etcd member action requires a member ID or hostname")
		}
	} else if request.MemberID != 0 || request.MemberHostname != "" {
		return errors.New("member identity only applies to membership and leadership actions")
	}
	return nil
}

func (s *Session) readCurrentEtcdActionFacts(ctx context.Context, request EtcdActionRequest) (etcdActionFacts, error) {
	readCtx, cancel := context.WithTimeout(ctx, unaryReadTimeout)
	defer cancel()
	if err := readCtx.Err(); err != nil {
		return etcdActionFacts{}, err
	}
	nodes, err := readNodes(readCtx, s.sdkSession)
	if err != nil {
		if readCtx.Err() != nil {
			return etcdActionFacts{}, errors.Join(readCtx.Err(), err)
		}
		return etcdActionFacts{}, errors.Join(ErrAssessmentChanged, err)
	}
	if err := readCtx.Err(); err != nil {
		return etcdActionFacts{}, err
	}
	node, ok := resolveNodeTarget(nodes.Nodes, request.Node)
	if !ok || node.Role != domain.NodeRoleControl {
		return etcdActionFacts{}, ErrAssessmentChanged
	}
	return s.readEtcdActionFacts(readCtx, nodes.Nodes, node, request)
}

func (s *Session) readEtcdActionFacts(ctx context.Context, nodes []domain.NodeSnapshot, node domain.NodeSnapshot, request EtcdActionRequest) (etcdActionFacts, error) {
	facts := etcdActionFacts{nodeID: node.ID, nodeName: node.Name, nodeTarget: node.Target(), nodeRole: node.Role, rpcNode: node.Target()}
	requiresMember := request.Kind == EtcdActionRemoveMember || request.Kind == EtcdActionLeaveCluster || request.Kind == EtcdActionForfeitLeadership
	validatesMember := requiresMember || (request.Kind == EtcdActionSnapshot && (request.MemberID != 0 || request.MemberHostname != ""))
	if !validatesMember {
		return facts, nil
	}
	if err := ctx.Err(); err != nil {
		return facts, err
	}
	if s.sdkSession == nil || s.sdkSession.Etcd() == nil {
		return facts, ErrCapabilityUnavailable
	}
	hostnames := controlPlaneHostnames(nodes)
	if len(hostnames) == 0 {
		return facts, ErrAssessmentChanged
	}
	if err := ctx.Err(); err != nil {
		return facts, err
	}
	set, readErr := s.sdkSession.Etcd().List(ctx, hostnames)
	if readErr != nil {
		if ctx.Err() != nil {
			return facts, errors.Join(ctx.Err(), readErr)
		}
		return facts, errors.Join(ErrAssessmentChanged, readErr)
	}
	if err := ctx.Err(); err != nil {
		return facts, err
	}
	etcd := EtcdPolicyFacts{Available: true, Members: set.Members}
	member, ok := resolveEtcdMember(set.Members, request.MemberID, request.MemberHostname)
	if !ok || member.Hostname == "" {
		return facts, ErrAssessmentChanged
	}
	memberNode, ok := resolveNodeTarget(nodes, member.Hostname)
	if !ok || memberNode.Role != domain.NodeRoleControl || memberNode.Target() != node.Target() {
		return facts, ErrAssessmentChanged
	}
	memberHostname := memberNode.Target()
	facts.memberID = member.MemberID
	facts.memberHost = member.Hostname
	facts.hasMember = true
	if request.Kind == EtcdActionSnapshot {
		return facts, nil
	}
	facts.quorum = assessEtcdQuorumFacts(etcd, []string{member.Hostname})
	facts.blocked = etcdMembershipBlockReasonFacts(etcd, request.Kind, request.MemberID, request.MemberHostname)
	facts.warning = etcdMembershipWarningFacts(etcd, request.Kind, request.MemberID, request.MemberHostname)
	if request.Kind == EtcdActionForfeitLeadership {
		facts.blocked = ""
		facts.warning = etcdLeadershipWarningFacts(etcd, member.Hostname)
	}
	if isDestructiveEtcdMembership(request.Kind) {
		facts.snapshotNode = etcdSnapshotNodeForFacts(etcd, request.MemberID, member.Hostname)
		if request.Kind == EtcdActionLeaveCluster {
			facts.rpcNode = memberHostname
		} else if facts.snapshotNode == member.Hostname || facts.snapshotNode == memberHostname {
			facts.rpcNode = firstOtherControlPlaneHostname(nodes, member.Hostname)
		} else {
			facts.rpcNode = facts.snapshotNode
		}
		source, sourceOK := resolveNodeTarget(nodes, facts.snapshotNode)
		if !sourceOK || source.Role != domain.NodeRoleControl {
			return facts, ErrAssessmentChanged
		}
		facts.snapshotNode = source.Target()
		gateway, gatewayOK := resolveNodeTarget(nodes, facts.rpcNode)
		if !gatewayOK || gateway.Role != domain.NodeRoleControl {
			if facts.blocked != "" {
				facts.rpcNode = ""
				return facts, nil
			}
			return facts, ErrAssessmentChanged
		}
		facts.rpcNode = gateway.Target()
		if facts.rpcNode == "" {
			return facts, ErrAssessmentChanged
		}
	}
	return facts, nil
}

func sameEtcdActionFacts(left, right etcdActionFacts) bool {
	return left.nodeID == right.nodeID && left.nodeName == right.nodeName && left.nodeTarget == right.nodeTarget && left.nodeRole == right.nodeRole &&
		left.memberID == right.memberID && left.memberHost == right.memberHost && left.hasMember == right.hasMember &&
		left.snapshotNode == right.snapshotNode && left.rpcNode == right.rpcNode && left.warning == right.warning && left.blocked == right.blocked &&
		left.quorum == right.quorum
}

func (s *Session) executeEtcdSnapshot(ctx context.Context, node, path string) (*domain.EtcdSnapshotResult, MutationResult, error) {
	snapshotCtx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	if err := snapshotCtx.Err(); err != nil {
		return nil, MutationResult{}, err
	}
	result, rpcErr := s.sdkSession.EtcdOperations().Snapshot(snapshotCtx, node, path)
	var snapshot *domain.EtcdSnapshotResult
	if validEtcdSnapshotReceipt(result, node, path) {
		copy := result
		snapshot = &copy
	}
	if rpcErr == nil && snapshot == nil {
		rpcErr = errors.New("etcd snapshot adapter returned an invalid receipt")
	}
	if rpcErr != nil && snapshotCtx.Err() != nil {
		rpcErr = mutationCallError(snapshotCtx, rpcErr)
	}
	outcome := domain.OperationUnconfirmed
	if rpcErr == nil {
		outcome = domain.OperationCompleted
	}
	targetResult := TargetResult{Target: node, Outcome: outcome, Err: rpcErr}
	mutation := MutationResult{Targets: []TargetResult{targetResult}, Snapshot: snapshot}
	if rpcErr != nil {
		return snapshot, mutation, rpcErr
	}
	return snapshot, mutation, nil
}

func validEtcdSnapshotReceipt(result domain.EtcdSnapshotResult, node, path string) bool {
	if result.Node != node || result.Path != path || result.Size <= 0 || len(result.SHA256) != 64 {
		return false
	}
	_, err := hex.DecodeString(result.SHA256)
	return err == nil
}
