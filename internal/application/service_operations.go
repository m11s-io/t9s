package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

// serviceActionFacts keeps only the node identity and confirmation-relevant
// etcd risk facts observed when a service action is prepared.
type serviceActionFacts struct {
	nodeID   string
	nodeName string
	target   string
	role     domain.NodeRole
	warning  string
	blocked  string
}

// PrepareServiceAction resolves the requested identity and captures the
// service action's current risk before presenting a confirmation prompt.
func (s *Session) PrepareServiceAction(ctx context.Context, request ServiceActionRequest) (*PreparedServiceAction, error) {
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
	if strings.TrimSpace(request.Node) == "" || strings.TrimSpace(request.Service) == "" {
		return nil, errors.New("service action requires a node identity and service name")
	}
	if request.Kind != ServiceActionStart && request.Kind != ServiceActionStop && request.Kind != ServiceActionRestart {
		return nil, fmt.Errorf("unsupported service action kind %q", request.Kind)
	}
	if s.sdkSession == nil || s.sdkSession.ServiceActions() == nil {
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
	if !ok {
		return nil, fmt.Errorf("service action node %q does not resolve to one canonical node", request.Node)
	}
	request.Node = node.Target()
	facts, err := s.readServiceActionFacts(readCtx, nodes.Nodes, node, request)
	if err != nil {
		return nil, err
	}
	if err := readCtx.Err(); err != nil {
		return nil, err
	}
	assessment := Assessment{Targets: []string{request.Node}, Warning: facts.warning, Blocked: facts.blocked}
	plan := newPreparedServiceActionWithFacts(s, request, assessment, facts)
	return &plan, nil
}

// ExecuteServiceAction consumes a prepared plan, rechecks its canonical
// identity and risk under the shared mutation lease, then dispatches one RPC.
func (s *Session) ExecuteServiceAction(ctx context.Context, plan *PreparedServiceAction, confirmation Confirmation) (MutationResult, error) {
	if plan == nil {
		return MutationResult{}, ErrInvalidPlan
	}
	operationCtx, release, err := s.admit(ctx, plan.state, confirmation, controlOperationTimeout)
	if err != nil {
		return MutationResult{}, err
	}
	defer release()
	if plan.state.serviceRequest == nil || plan.state.serviceFacts == nil {
		return MutationResult{}, ErrInvalidPlan
	}
	request := *plan.state.serviceRequest
	if s.sdkSession == nil || s.sdkSession.ServiceActions() == nil {
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
	node, ok := resolveNodeTarget(nodes.Nodes, request.Node)
	if !ok {
		return MutationResult{}, ErrAssessmentChanged
	}
	currentFacts, readErr := s.readServiceActionFacts(readCtx, nodes.Nodes, node, request)
	if readErr != nil {
		if err := readCtx.Err(); err != nil {
			return MutationResult{}, errors.Join(err, readErr)
		}
		return MutationResult{}, errors.Join(ErrAssessmentChanged, readErr)
	}
	if err := readCtx.Err(); err != nil {
		return MutationResult{}, err
	}
	preparedFacts := plan.state.serviceFacts
	if currentFacts.blocked != "" {
		return MutationResult{}, ErrActionBlocked
	}
	if !sameServiceActionFacts(*preparedFacts, currentFacts) {
		return MutationResult{}, ErrAssessmentChanged
	}
	if err := operationCtx.Err(); err != nil {
		return MutationResult{}, err
	}

	target := request.Service + "@" + request.Node
	var rpcErr error
	switch request.Kind {
	case ServiceActionStart:
		rpcErr = s.sdkSession.ServiceActions().Start(operationCtx, request.Node, request.Service)
	case ServiceActionStop:
		rpcErr = s.sdkSession.ServiceActions().Stop(operationCtx, request.Node, request.Service)
	case ServiceActionRestart:
		rpcErr = s.sdkSession.ServiceActions().Restart(operationCtx, request.Node, request.Service)
	default:
		return MutationResult{}, fmt.Errorf("unsupported service action kind %q", request.Kind)
	}
	if rpcErr == nil {
		return MutationResult{Targets: []TargetResult{{Target: target, Outcome: domain.OperationAccepted, Warning: currentFacts.warning}}}, nil
	}
	if operationCtx.Err() != nil {
		rpcErr = mutationCallError(operationCtx, rpcErr)
	}
	result := MutationResult{Targets: []TargetResult{{Target: target, Outcome: domain.OperationUnconfirmed, Err: rpcErr, Warning: currentFacts.warning}}}
	return result, rpcErr
}

func readNodes(ctx context.Context, session ports.Session) (domain.NodeSet, error) {
	if session == nil || session.Nodes() == nil {
		return domain.NodeSet{}, ErrCapabilityUnavailable
	}
	return session.Nodes().List(ctx)
}

func (s *Session) readServiceActionFacts(ctx context.Context, nodes []domain.NodeSnapshot, node domain.NodeSnapshot, request ServiceActionRequest) (serviceActionFacts, error) {
	facts := serviceActionFacts{nodeID: node.ID, nodeName: node.Name, target: node.Target(), role: node.Role}
	if err := ctx.Err(); err != nil {
		return facts, err
	}
	if request.Service != "etcd" || request.Kind == ServiceActionStart {
		return facts, nil
	}
	if s.sdkSession == nil || s.sdkSession.Etcd() == nil {
		etcdFacts := EtcdPolicyFacts{}
		quorum := assessEtcdQuorumFacts(etcdFacts, []string{node.Target()})
		facts.warning = "control-plane node(s); " + quorum.reason
		facts.blocked = etcdQuorumBlockReasonFromFacts(etcdFacts, node.Target())
		return facts, nil
	}

	var etcdFacts EtcdPolicyFacts
	targets := controlPlaneHostnames(nodes)
	if len(targets) > 0 {
		members, etcdErr := s.sdkSession.Etcd().List(ctx, targets)
		if etcdErr == nil {
			etcdFacts = EtcdPolicyFacts{Available: true, Members: members.Members}
		} else if ctx.Err() != nil {
			return facts, errors.Join(ctx.Err(), etcdErr)
		} else if errors.Is(etcdErr, context.Canceled) || errors.Is(etcdErr, context.DeadlineExceeded) {
			return facts, etcdErr
		}
	}
	if err := ctx.Err(); err != nil {
		return facts, err
	}

	quorum := assessEtcdQuorumFacts(etcdFacts, []string{node.Target()})
	if !quorum.known {
		facts.warning = "control-plane node(s); " + quorum.reason
	} else if quorum.belowQuorum() {
		facts.warning = fmt.Sprintf("control-plane node(s); would drop etcd to %d/%d — below quorum (need %d)", quorum.remaining, quorum.voters, quorum.floor)
	} else {
		facts.warning = "control-plane node(s)"
	}
	if quorum.certainlyBelowQuorum() {
		facts.blocked = fmt.Sprintf("refusing: would drop etcd to %d/%d (need %d)", quorum.confirmedRemaining, quorum.voters, quorum.floor)
	}
	return facts, nil
}

func etcdQuorumBlockReasonFromFacts(etcd EtcdPolicyFacts, target string) string {
	quorum := assessEtcdQuorumFacts(etcd, []string{target})
	if !quorum.certainlyBelowQuorum() {
		return ""
	}
	return fmt.Sprintf("refusing: would drop etcd to %d/%d (need %d)", quorum.confirmedRemaining, quorum.voters, quorum.floor)
}

func sameServiceActionFacts(prepared, current serviceActionFacts) bool {
	return prepared.nodeID == current.nodeID && prepared.nodeName == current.nodeName && prepared.target == current.target && prepared.role == current.role && prepared.warning == current.warning && prepared.blocked == current.blocked
}
