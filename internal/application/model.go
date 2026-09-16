package application

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type Route string

const RouteNodes Route = "nodes"

type LoadStatus string

const (
	Idle    LoadStatus = "idle"
	Loading LoadStatus = "loading"
	Ready   LoadStatus = "ready"
	Partial LoadStatus = "partial"
	Failed  LoadStatus = "failed"
)

type Model struct {
	Route                  Route
	ContextName            string
	NodeFocus              string
	OpenContextPicker      bool
	WritesEnabled          bool
	Contexts               []domain.ClusterContext
	Generation             uint64
	Nodes                  NodeState
	Services               ServiceState
	Events                 EventState
	Etcd                   EtcdState
	EtcdSnapshot           EtcdSnapshotState
	Processes              ProcessesState
	Disks                  DisksState
	Network                NetworkState
	Dmesg                  DmesgState
	HealthCheck            ClusterHealthState
	Netstat                SocketState
	Mounts                 MountState
	Memory                 MemoryState
	ResourceBrowser        ResourceBrowserState
	Kubernetes             KubernetesState
	nodeReader             ports.NodeReader
	nodeController         ports.NodeController
	serviceReader          ports.ServiceReader
	serviceController      ports.ServiceController
	logReader              ports.ServiceLogReader
	eventReader            ports.EventReader
	etcdReader             ports.EtcdReader
	etcdOperations         ports.EtcdOperations
	processReader          ports.ProcessReader
	diskReader             ports.DiskReader
	networkReader          ports.NetworkReader
	dmesgReader            ports.DmesgReader
	clusterHealthReader    ports.ClusterHealthReader
	netstatReader          ports.NetstatReader
	mountReader            ports.MountReader
	memoryReader           ports.MemoryReader
	resourceKindReader     ports.ResourceKindReader
	resourceInstanceReader ports.ResourceInstanceReader
	kubernetesReader       ports.KubernetesNodeReader
	logStream              ports.ServiceLogStream
	logGeneration          uint64
	dmesgStream            ports.DmesgStream
	dmesgGeneration        uint64
	clusterHealthStream    ports.ClusterHealthStream
	healthGeneration       uint64
	Notice                 string
	Logs                   LogState
	PendingAction          *PendingAction
	PendingServiceAction   *PendingServiceAction
	PendingEtcdAction      *PendingEtcdAction
	ActionResults          []ActionResult
	// ActionTotal is the number of targets the currently-in-flight bulk
	// action was confirmed against. ActionResults grows one entry at a time
	// as ActionSucceeded/ActionFailed messages stream back independently, so
	// len(ActionResults) alone cannot be used as a "total" denominator while
	// results are still outstanding.
	ActionTotal int
	Upgrade     UpgradeState

	upgradeResults <-chan upgradeStreamResult
	upgradeCancel  context.CancelFunc
}

type NodeState struct {
	Status LoadStatus
	Value  domain.NodeSet
	Err    string
}

type ServiceState struct {
	Status LoadStatus
	Value  domain.ServiceSet
	Err    string
}

type EventState struct {
	Status LoadStatus
	Value  domain.EventSet
	Err    string
}

type EtcdState struct {
	Status LoadStatus
	Value  domain.EtcdSet
	Err    string
}

// EtcdSnapshotState is the standalone snapshot flow's result/notice. It is
// separate from EtcdState so a local backup never disturbs the membership
// view.
type EtcdSnapshotState struct {
	Status     LoadStatus
	Result     domain.EtcdSnapshotResult
	Err        string
	MemberNode string
}

// EtcdActionKind identifies a write action on the :etcd view.
type EtcdActionKind string

const (
	EtcdActionSnapshot     EtcdActionKind = "snapshot"
	EtcdActionDefragment   EtcdActionKind = "defragment"
	EtcdActionDisarmAlarms EtcdActionKind = "disarm-alarms"
	EtcdActionRemoveMember EtcdActionKind = "remove-member"
	EtcdActionLeaveCluster EtcdActionKind = "leave-cluster"
)

// EtcdActionStage tracks where a pending :etcd action is in its lifecycle.
// Destructive membership actions move Idle -> Snapshot -> Operation so the
// membership RPC cannot run before a snapshot of the target has succeeded.
type EtcdActionStage string

const (
	EtcdStageIdle      EtcdActionStage = "idle"
	EtcdStageSnapshot  EtcdActionStage = "snapshot"
	EtcdStageOperation EtcdActionStage = "operation"
)

// PendingEtcdAction is a confirmed-but-not-yet-run :etcd write action. Blocked
// is non-empty when the action must not be confirmed at all, mirroring
// PendingAction.Blocked. SnapshotNode/SnapshotPath are only set for the
// destructive membership kinds, which require a snapshot of the target member
// before the membership change.
type PendingEtcdAction struct {
	Kind           EtcdActionKind
	Stage          EtcdActionStage
	MemberID       uint64
	MemberHostname string
	Node           string
	SnapshotNode   string
	SnapshotPath   string
	Warning        string
	Blocked        string
}

type ProcessesState struct {
	Status LoadStatus
	Value  domain.ProcessSet
	Err    string
	Node   string
}

type DisksState struct {
	Status LoadStatus
	Value  domain.DiskSet
	Err    string
	Node   string
}

type NetworkState struct {
	Status LoadStatus
	Value  domain.NetworkSet
	Err    string
	Node   string
}

type SocketState struct {
	Status LoadStatus
	Value  domain.SocketSet
	Err    string
	Node   string
}

type MountState struct {
	Status LoadStatus
	Value  domain.MountSet
	Err    string
	Node   string
}

type MemoryState struct {
	Status LoadStatus
	Value  domain.MemorySnapshot
	Err    string
	Node   string
}

// DmesgState is the streaming kernel-log view's result. It is separate from
// LogState because the request and error wording differ from service logs.
type DmesgState struct {
	Status    LoadStatus
	Request   domain.DmesgRequest
	Lines     []string
	Err       string
	EOF       bool
	Following bool
}

// ClusterHealthState is the streaming server-side cluster health check view.
// VerdictReady is true only after a clean EOF, so an interrupted or failed
// check never reads as healthy.
type ClusterHealthState struct {
	Status       LoadStatus
	Request      domain.ClusterHealthRequest
	Lines        []string
	Err          string
	EOF          bool
	VerdictReady bool
}

type ResourceBrowserState struct {
	KindsStatus LoadStatus
	Kinds       domain.ResourceKindSet
	KindsErr    string

	InstancesStatus LoadStatus
	Instances       domain.ResourceInstanceSet
	InstancesErr    string
	SelectedKind    string
	SelectedNode    string

	DetailStatus LoadStatus
	Detail       domain.ResourceInstanceSnapshot
	DetailErr    string
}

type KubernetesState struct {
	Available bool
	Status    LoadStatus
	Nodes     map[string]domain.KubernetesNodeSnapshot
	Err       string
}

type KubernetesNodesLoaded struct {
	Generation uint64
	Nodes      map[string]domain.KubernetesNodeSnapshot
}

func (KubernetesNodesLoaded) applicationMessage() {}

type KubernetesNodesFailed struct {
	Generation uint64
	Err        error
}

func (KubernetesNodesFailed) applicationMessage() {}

type RefreshKubernetesNodes struct{}

func (RefreshKubernetesNodes) applicationMessage() {}

type OpenResourceBrowser struct {
	Kind string
	Node string
}

func (OpenResourceBrowser) applicationMessage() {}

type ResourceKindsLoaded struct {
	Generation uint64
	Kinds      domain.ResourceKindSet
}

func (ResourceKindsLoaded) applicationMessage() {}

type ResourceKindsFailed struct {
	Generation uint64
	Err        error
}

func (ResourceKindsFailed) applicationMessage() {}

type SelectResourceKind struct {
	Kind string
	Node string
}

func (SelectResourceKind) applicationMessage() {}

type ResourceInstancesLoaded struct {
	Generation uint64
	Instances  domain.ResourceInstanceSet
}

func (ResourceInstancesLoaded) applicationMessage() {}

type ResourceInstancesFailed struct {
	Generation uint64
	Err        error
}

func (ResourceInstancesFailed) applicationMessage() {}

type OpenResourceInstance struct {
	ID string
}

func (OpenResourceInstance) applicationMessage() {}

type ResourceInstanceLoaded struct {
	Generation uint64
	Instance   domain.ResourceInstanceSnapshot
}

func (ResourceInstanceLoaded) applicationMessage() {}

type ResourceInstanceFailed struct {
	Generation uint64
	Err        error
}

func (ResourceInstanceFailed) applicationMessage() {}

type LogState struct {
	Status    LoadStatus
	Request   domain.LogRequest
	Lines     []string
	Err       string
	EOF       bool
	Following bool
}

type Message interface{ applicationMessage() }

type Start struct{}

func (Start) applicationMessage() {}

type ContextsLoaded struct {
	Generation  uint64
	Contexts    []domain.ClusterContext
	ContextName string
}

func (ContextsLoaded) applicationMessage() {}

type SelectContext struct {
	Name string
}

func (SelectContext) applicationMessage() {}

type ActionKind string

const (
	ActionReboot   ActionKind = "reboot"
	ActionShutdown ActionKind = "shutdown"
	ActionRollback ActionKind = "rollback"
	ActionUpgrade  ActionKind = "upgrade"
)

type PendingAction struct {
	Kind    ActionKind
	Targets []string
	Warning string
	// Blocked is non-empty when the action must not be confirmed at all
	// (for example it would drop etcd below quorum). It is a hard gate,
	// not an advisory warning.
	Blocked string
	Image   string
}

type ActionResult struct {
	Target  string
	Err     string
	Warning string
}

type UpgradeState struct {
	Active  bool
	Target  string
	Event   ports.UpgradeEvent
	Err     string
	Warning string
}

// upgradeStreamResult is private so stream mechanics stay inside application effects.
type upgradeStreamResult struct {
	Event   *ports.UpgradeEvent
	Err     error
	Outcome ports.UpgradeOutcome
	Warning string
	Done    bool
}

type RequestAction struct {
	Kind    ActionKind
	Targets []string
	Image   string
}

func (RequestAction) applicationMessage() {}

type ConfirmPendingAction struct{}

func (ConfirmPendingAction) applicationMessage() {}

type CancelPendingAction struct{}

func (CancelPendingAction) applicationMessage() {}

type ServiceActionKind string

const (
	ServiceActionStart   ServiceActionKind = "start"
	ServiceActionStop    ServiceActionKind = "stop"
	ServiceActionRestart ServiceActionKind = "restart"
)

type PendingServiceAction struct {
	Kind    ServiceActionKind
	Node    string
	Service string
	Warning string
	Blocked string
}

type RequestServiceAction struct {
	Kind    ServiceActionKind
	Node    string
	Service string
}

func (RequestServiceAction) applicationMessage() {}

type UpgradeStarted struct {
	Generation uint64
	Target     string
	results    <-chan upgradeStreamResult
	cancel     context.CancelFunc
}

func (UpgradeStarted) applicationMessage() {}

type UpgradeProgressed struct {
	Generation uint64
	Target     string
	Event      ports.UpgradeEvent
}

func (UpgradeProgressed) applicationMessage() {}

type UpgradeSucceeded struct {
	Generation uint64
	Target     string
}

func (UpgradeSucceeded) applicationMessage() {}

type UpgradeAppliedWithRecoveryWarning struct {
	Generation uint64
	Target     string
	Warning    string
}

func (UpgradeAppliedWithRecoveryWarning) applicationMessage() {}

// RecoveryUncordonSucceeded and RecoveryUncordonFailed report the outcome of
// an opportunistic uncordon attempt made after a Kubernetes node refresh
// observes a pending-recovery target has become Ready. See
// recoveryEffectIfReady.
type RecoveryUncordonSucceeded struct {
	Generation uint64
	Target     string
}

func (RecoveryUncordonSucceeded) applicationMessage() {}

type RecoveryUncordonFailed struct {
	Generation uint64
	Target     string
}

func (RecoveryUncordonFailed) applicationMessage() {}

type UpgradeFailed struct {
	Generation uint64
	Target     string
	Err        error
}

func (UpgradeFailed) applicationMessage() {}

type ActionSucceeded struct {
	Generation uint64
	Target     string
}

func (ActionSucceeded) applicationMessage() {}

type ActionFailed struct {
	Generation uint64
	Target     string
	Err        error
}

func (ActionFailed) applicationMessage() {}

type SessionOpened struct {
	Generation        uint64
	Nodes             ports.NodeReader
	NodeController    ports.NodeController
	ServiceController ports.ServiceController
	Services          ports.ServiceReader
	Logs              ports.ServiceLogReader
	Events            ports.EventReader
	Etcd              ports.EtcdReader
	EtcdOperations    ports.EtcdOperations
	Processes         ports.ProcessReader
	Disks             ports.DiskReader
	Network           ports.NetworkReader
	Dmesg             ports.DmesgReader
	Netstat           ports.NetstatReader
	Mounts            ports.MountReader
	Memory            ports.MemoryReader
	ClusterHealth     ports.ClusterHealthReader
	ResourceKinds     ports.ResourceKindReader
	Resources         ports.ResourceInstanceReader
	KubernetesNodes   ports.KubernetesNodeReader
}

func (SessionOpened) applicationMessage() {}

type NodesLoaded struct {
	Generation uint64
	Nodes      domain.NodeSet
}

type ServicesLoaded struct {
	Generation uint64
	Services   domain.ServiceSet
}

type ServicesFailed struct {
	Generation uint64
	Err        error
}

type RefreshServices struct{}

type RefreshNodes struct{}

type RefreshEvents struct{}

func (RefreshEvents) applicationMessage() {}

type EventsLoaded struct {
	Generation uint64
	Events     domain.EventSet
}

func (EventsLoaded) applicationMessage() {}

type EventsFailed struct {
	Generation uint64
	Err        error
}

func (EventsFailed) applicationMessage() {}

type RefreshEtcd struct{}

func (RefreshEtcd) applicationMessage() {}

type EtcdLoaded struct {
	Generation uint64
	Etcd       domain.EtcdSet
}

func (EtcdLoaded) applicationMessage() {}

type EtcdFailed struct {
	Generation uint64
	Err        error
}

func (EtcdFailed) applicationMessage() {}

type OpenProcesses struct {
	Node string
}

func (OpenProcesses) applicationMessage() {}

type RefreshProcesses struct{}

func (RefreshProcesses) applicationMessage() {}

type ProcessesLoaded struct {
	Generation uint64
	Processes  domain.ProcessSet
}

func (ProcessesLoaded) applicationMessage() {}

type ProcessesFailed struct {
	Generation uint64
	Err        error
}

func (ProcessesFailed) applicationMessage() {}

type RequestEtcdSnapshotPrompt struct {
	Node           string
	MemberHostname string
}

func (RequestEtcdSnapshotPrompt) applicationMessage() {}

type EtcdSnapshotPromptOpened struct {
	Generation     uint64
	Node           string
	MemberHostname string
	DefaultPath    string
}

func (EtcdSnapshotPromptOpened) applicationMessage() {}

type ConfirmEtcdSnapshotPrompt struct {
	Node           string
	MemberHostname string
	Path           string
}

func (ConfirmEtcdSnapshotPrompt) applicationMessage() {}

type EtcdSnapshotSucceeded struct {
	Generation uint64
	Result     domain.EtcdSnapshotResult
}

func (EtcdSnapshotSucceeded) applicationMessage() {}

// CancelEtcdSnapshotPrompt resets the snapshot flow when the operator escapes
// the path prompt, so a cancelled prompt cannot leave the state stuck in
// Loading.
type CancelEtcdSnapshotPrompt struct{}

func (CancelEtcdSnapshotPrompt) applicationMessage() {}

type EtcdSnapshotFailed struct {
	Generation uint64
	Err        error
}

func (EtcdSnapshotFailed) applicationMessage() {}

type RequestEtcdAction struct {
	Kind           EtcdActionKind
	MemberID       uint64
	MemberHostname string
	Node           string
}

func (RequestEtcdAction) applicationMessage() {}

type ConfirmEtcdAction struct{}

func (ConfirmEtcdAction) applicationMessage() {}

type EtcdActionSucceeded struct {
	Generation     uint64
	MemberHostname string
}

func (EtcdActionSucceeded) applicationMessage() {}

type EtcdActionFailed struct {
	Generation     uint64
	MemberHostname string
	Err            error
}

func (EtcdActionFailed) applicationMessage() {}

type RequestUpgradePrompt struct {
	Target string
}

func (RequestUpgradePrompt) applicationMessage() {}

type UpgradePromptOpened struct {
	Target     string
	Image      string
	Generation uint64
}

func (UpgradePromptOpened) applicationMessage() {}

type OpenDisks struct {
	Node string
}

func (OpenDisks) applicationMessage() {}

type RefreshDisks struct{}

func (RefreshDisks) applicationMessage() {}

type DisksLoaded struct {
	Generation uint64
	Disks      domain.DiskSet
}

func (DisksLoaded) applicationMessage() {}

type DisksFailed struct {
	Generation uint64
	Err        error
}

func (DisksFailed) applicationMessage() {}

type OpenNetwork struct {
	Node string
}

func (OpenNetwork) applicationMessage() {}

type RefreshNetwork struct{}

func (RefreshNetwork) applicationMessage() {}

type NetworkLoaded struct {
	Generation uint64
	Network    domain.NetworkSet
}

func (NetworkLoaded) applicationMessage() {}

type NetworkFailed struct {
	Generation uint64
	Err        error
}

func (NetworkFailed) applicationMessage() {}

type OpenNetstat struct {
	Node string
}

func (OpenNetstat) applicationMessage() {}

type RefreshNetstat struct{}

func (RefreshNetstat) applicationMessage() {}

type NetstatLoaded struct {
	Generation uint64
	Node       string
	Sockets    domain.SocketSet
}

func (NetstatLoaded) applicationMessage() {}

type NetstatFailed struct {
	Generation uint64
	Node       string
	Err        error
}

func (NetstatFailed) applicationMessage() {}

type OpenMounts struct {
	Node string
}

func (OpenMounts) applicationMessage() {}

type RefreshMounts struct{}

func (RefreshMounts) applicationMessage() {}

type MountsLoaded struct {
	Generation uint64
	Node       string
	Mounts     domain.MountSet
}

func (MountsLoaded) applicationMessage() {}

type MountsFailed struct {
	Generation uint64
	Node       string
	Err        error
}

func (MountsFailed) applicationMessage() {}

type OpenMemory struct {
	Node string
}

func (OpenMemory) applicationMessage() {}

type RefreshMemory struct{}

func (RefreshMemory) applicationMessage() {}

type MemoryLoaded struct {
	Generation uint64
	Node       string
	Memory     domain.MemorySnapshot
}

func (MemoryLoaded) applicationMessage() {}

type MemoryFailed struct {
	Generation uint64
	Node       string
	Err        error
}

func (MemoryFailed) applicationMessage() {}

type OpenDmesg struct {
	Request domain.DmesgRequest
}

func (OpenDmesg) applicationMessage() {}

type ReconnectDmesg struct{}

func (ReconnectDmesg) applicationMessage() {}

type CloseDmesg struct{}

func (CloseDmesg) applicationMessage() {}

type ClearDmesg struct{}

func (ClearDmesg) applicationMessage() {}

type dmesgOpened struct {
	Generation       uint64
	StreamGeneration uint64
	Stream           ports.DmesgStream
	Err              error
}

func (dmesgOpened) applicationMessage() {}

type OpenClusterHealth struct {
	Request domain.ClusterHealthRequest
}

func (OpenClusterHealth) applicationMessage() {}

type CloseClusterHealth struct{}

func (CloseClusterHealth) applicationMessage() {}

type ClearClusterHealth struct{}

func (ClearClusterHealth) applicationMessage() {}

type clusterHealthOpened struct {
	Generation       uint64
	StreamGeneration uint64
	Stream           ports.ClusterHealthStream
	Err              error
}

func (clusterHealthOpened) applicationMessage() {}

type ClusterHealthProgressLoaded struct {
	Generation       uint64
	StreamGeneration uint64
	Progress         domain.ClusterHealthProgress
	Err              error
}

func (ClusterHealthProgressLoaded) applicationMessage() {}

type DmesgBatchLoaded struct {
	Generation       uint64
	StreamGeneration uint64
	Batch            domain.DmesgBatch
	Err              error
}

func (DmesgBatchLoaded) applicationMessage() {}

type OpenServiceLogs struct {
	Request domain.LogRequest
}

type ReconnectServiceLogs struct{}
type CloseServiceLogs struct{}
type ClearServiceLogs struct{}

type serviceLogOpened struct {
	Generation       uint64
	StreamGeneration uint64
	Stream           ports.ServiceLogStream
	Err              error
}

type ServiceLogBatchLoaded struct {
	Generation       uint64
	StreamGeneration uint64
	Batch            domain.LogBatch
	Err              error
}

func (NodesLoaded) applicationMessage()           {}
func (ServicesLoaded) applicationMessage()        {}
func (ServicesFailed) applicationMessage()        {}
func (RefreshServices) applicationMessage()       {}
func (RefreshNodes) applicationMessage()          {}
func (OpenServiceLogs) applicationMessage()       {}
func (ReconnectServiceLogs) applicationMessage()  {}
func (CloseServiceLogs) applicationMessage()      {}
func (ClearServiceLogs) applicationMessage()      {}
func (serviceLogOpened) applicationMessage()      {}
func (ServiceLogBatchLoaded) applicationMessage() {}

type LoadFailed struct {
	Generation uint64
	Err        error
}

func (LoadFailed) applicationMessage() {}
