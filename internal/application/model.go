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
	Processes              ProcessesState
	Disks                  DisksState
	Network                NetworkState
	ResourceBrowser        ResourceBrowserState
	Kubernetes             KubernetesState
	nodeReader             ports.NodeReader
	nodeController         ports.NodeController
	serviceReader          ports.ServiceReader
	serviceController      ports.ServiceController
	logReader              ports.ServiceLogReader
	eventReader            ports.EventReader
	etcdReader             ports.EtcdReader
	processReader          ports.ProcessReader
	diskReader             ports.DiskReader
	networkReader          ports.NetworkReader
	resourceKindReader     ports.ResourceKindReader
	resourceInstanceReader ports.ResourceInstanceReader
	kubernetesReader       ports.KubernetesNodeReader
	logStream              ports.ServiceLogStream
	logGeneration          uint64
	Notice                 string
	Logs                   LogState
	PendingAction          *PendingAction
	PendingServiceAction   *PendingServiceAction
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
	Image   string
}

type ActionResult struct {
	Target string
	Err    string
}

type UpgradeState struct {
	Active bool
	Target string
	Event  ports.UpgradeEvent
	Err    string
}

// upgradeStreamResult is private so stream mechanics stay inside application effects.
type upgradeStreamResult struct {
	Event *ports.UpgradeEvent
	Err   error
	Done  bool
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
	Processes         ports.ProcessReader
	Disks             ports.DiskReader
	Network           ports.NetworkReader
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
