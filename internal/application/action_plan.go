package application

import "github.com/m11s-io/t9s/internal/ports"

// ActionKind identifies a node mutation.
type ActionKind string

const (
	ActionReboot     ActionKind = "reboot"
	ActionShutdown   ActionKind = "shutdown"
	ActionRollback   ActionKind = "rollback"
	ActionUpgrade    ActionKind = "upgrade"
	ActionReset      ActionKind = "reset"
	ActionWipeDevice ActionKind = "wipe-device"
)

// ServiceActionKind identifies a service mutation.
type ServiceActionKind string

const (
	ServiceActionStart   ServiceActionKind = "start"
	ServiceActionStop    ServiceActionKind = "stop"
	ServiceActionRestart ServiceActionKind = "restart"
)

// EtcdActionKind identifies an etcd mutation.
type EtcdActionKind string

const (
	EtcdActionSnapshot          EtcdActionKind = "snapshot"
	EtcdActionDefragment        EtcdActionKind = "defragment"
	EtcdActionDisarmAlarms      EtcdActionKind = "disarm-alarms"
	EtcdActionRemoveMember      EtcdActionKind = "remove-member"
	EtcdActionLeaveCluster      EtcdActionKind = "leave-cluster"
	EtcdActionForfeitLeadership EtcdActionKind = "forfeit-leadership"
)

// ResetPreview classifies a node's disks for the reset overlay. Known is
// false when the disks reader returned no inventory for the node.
type ResetPreview struct {
	Node       string
	SystemDisk string
	UserDisks  []string
	Known      bool
	Err        string
}

// Assessment describes the user-visible risk and confirmation scope for a
// prepared mutation. Values returned by Prepared*.Assessment are deep copies.
type Assessment struct {
	Targets       []string
	Warning       string
	Blocked       string
	RequiredToken string
	SnapshotPath  string
	ResetPreview  []ResetPreview
}

type NodeActionRequest struct {
	Kind       ActionKind
	Targets    []string
	Image      string
	Reset      *ports.ResetOptions
	DeviceWipe *ports.DeviceWipeOptions
}

type ServiceActionRequest struct {
	Kind    ServiceActionKind
	Node    string
	Service string
}

type EtcdActionRequest struct {
	Kind           EtcdActionKind
	Node           string
	MemberID       uint64
	MemberHostname string
	SnapshotPath   string
}

// Prepared action copies share an unexported one-shot state.
type PreparedNodeAction struct{ state *preparedState }
type PreparedServiceAction struct{ state *preparedState }
type PreparedEtcdAction struct{ state *preparedState }

type preparedState struct {
	session        *Session
	consumed       bool // guarded by session.mu
	assessment     Assessment
	serviceFacts   *serviceActionFacts
	nodeFacts      *nodeActionFacts
	etcdFacts      *etcdActionFacts
	nodeRequest    *NodeActionRequest
	serviceRequest *ServiceActionRequest
	etcdRequest    *EtcdActionRequest
}

func newPreparedNodeAction(session *Session, request NodeActionRequest, assessment Assessment) PreparedNodeAction {
	request = cloneNodeActionRequest(request)
	return PreparedNodeAction{state: &preparedState{session: session, nodeRequest: &request, assessment: cloneAssessment(assessment)}}
}

func newPreparedServiceAction(session *Session, request ServiceActionRequest, assessment Assessment) PreparedServiceAction {
	return PreparedServiceAction{state: &preparedState{session: session, serviceRequest: &request, assessment: cloneAssessment(assessment)}}
}

func newPreparedServiceActionWithFacts(session *Session, request ServiceActionRequest, assessment Assessment, facts serviceActionFacts) PreparedServiceAction {
	plan := newPreparedServiceAction(session, request, assessment)
	plan.state.serviceFacts = &facts
	return plan
}

func newPreparedEtcdAction(session *Session, request EtcdActionRequest, assessment Assessment) PreparedEtcdAction {
	return PreparedEtcdAction{state: &preparedState{session: session, etcdRequest: &request, assessment: cloneAssessment(assessment)}}
}

func (p PreparedNodeAction) Assessment() Assessment    { return assessmentOf(p.state) }
func (p PreparedServiceAction) Assessment() Assessment { return assessmentOf(p.state) }
func (p PreparedEtcdAction) Assessment() Assessment    { return assessmentOf(p.state) }

func assessmentOf(state *preparedState) Assessment {
	if state == nil {
		return Assessment{}
	}
	return cloneAssessment(state.assessment)
}

func cloneAssessment(value Assessment) Assessment {
	value.Targets = append([]string(nil), value.Targets...)
	if value.ResetPreview != nil {
		value.ResetPreview = append([]ResetPreview(nil), value.ResetPreview...)
		for i := range value.ResetPreview {
			value.ResetPreview[i].UserDisks = append([]string(nil), value.ResetPreview[i].UserDisks...)
		}
	}
	return value
}

func cloneNodeActionRequest(value NodeActionRequest) NodeActionRequest {
	value.Targets = append([]string(nil), value.Targets...)
	if value.Reset != nil {
		reset := *value.Reset
		reset.SystemPartitions = append([]string(nil), reset.SystemPartitions...)
		reset.UserDisks = append([]string(nil), reset.UserDisks...)
		value.Reset = &reset
	}
	if value.DeviceWipe != nil {
		wipe := *value.DeviceWipe
		value.DeviceWipe = &wipe
	}
	return value
}
