package application

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
)

// maxClusterHealthLines bounds the retained health-check transcript so a
// misbehaving server cannot grow the view without limit.
const maxClusterHealthLines = 500

func NewModel(contextOverride string) (Model, Effect) {
	model := Model{
		Route:       RouteNodes,
		ContextName: contextOverride,
		Generation:  1,
		Nodes:       NodeState{Status: Loading},
	}

	return Update(model, Start{})
}

func Update(model Model, message Message) (Model, Effect) {
	switch message := message.(type) {
	case Start:
		return model, loadContexts(model.ContextName, model.Generation)

	case ContextsLoaded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Contexts = append([]domain.ClusterContext(nil), message.Contexts...)
		model.ContextName = message.ContextName
		model.Nodes = NodeState{Status: Loading}
		model.Notice = ""
		return model, openSession(message.ContextName, message.Generation)

	case SelectContext:
		if message.Name == model.ContextName {
			return model, nil
		}
		cancelActiveUpgrade(&model)
		model.ContextName = message.Name
		model.Generation++
		model.Nodes = NodeState{Status: Loading}
		model.Services = ServiceState{}
		model.Events = EventState{}
		model.Etcd = EtcdState{}
		model.Processes = ProcessesState{}
		model.Kubernetes = KubernetesState{}
		model.Disks = DisksState{}
		model.Network = NetworkState{}
		model.Dmesg = DmesgState{}
		model.dmesgStream = nil
		model.dmesgGeneration++
		model.HealthCheck = ClusterHealthState{}
		model.clusterHealthStream = nil
		model.healthGeneration++
		model.Netstat = SocketState{}
		model.Mounts = MountState{}
		model.Memory = MemoryState{}
		model.ResourceBrowser = ResourceBrowserState{}
		model.Logs = LogState{}
		model.logStream = nil
		model.logGeneration++
		model.Notice = ""
		model.PendingAction = nil
		model.PendingServiceAction = nil
		model.PendingEtcdAction = nil
		model.EtcdSnapshot = EtcdSnapshotState{}
		model.ActionResults = nil
		model.ActionTotal = 0
		return model, openSession(message.Name, model.Generation)

	case SessionOpened:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.nodeReader = message.Nodes
		model.nodeController = message.NodeController
		model.serviceController = message.ServiceController
		model.serviceReader = message.Services
		model.logReader = message.Logs
		model.eventReader = message.Events
		model.etcdReader = message.Etcd
		model.etcdOperations = message.EtcdOperations
		model.processReader = message.Processes
		model.diskReader = message.Disks
		model.networkReader = message.Network
		model.dmesgReader = message.Dmesg
		model.clusterHealthReader = message.ClusterHealth
		model.netstatReader = message.Netstat
		model.mountReader = message.Mounts
		model.memoryReader = message.Memory
		model.resourceKindReader = message.ResourceKinds
		model.resourceInstanceReader = message.Resources
		model.kubernetesReader = message.KubernetesNodes
		model.Kubernetes.Available = model.kubernetesReader != nil
		return model, loadNodes(message.Nodes, message.Generation)

	case NodesLoaded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Nodes = NodeState{Status: nodeLoadStatus(message.Nodes), Value: message.Nodes}
		model.Notice = ""
		if model.serviceReader != nil {
			return model, loadServices(model.serviceReader, message.Generation)
		}
		return model, nil

	case ServicesLoaded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Services = ServiceState{Status: serviceLoadStatus(message.Services), Value: message.Services}
		if len(message.Services.Problems) > 0 {
			model.Notice = fmt.Sprintf("services unavailable on %d node(s)", len(message.Services.Problems))
		}
		return model, loadEvents(model.eventReader, message.Generation)

	case ServicesFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Services.Status = Failed
		model.Services.Err = "services unavailable"
		model.Notice = model.Services.Err
		return model, nil

	case RefreshServices:
		model.Services.Status = Loading
		model.Services.Err = ""
		return model, loadServices(model.serviceReader, model.Generation)

	case RefreshNodes:
		model.Nodes.Status = Loading
		model.Nodes.Err = ""
		return model, loadNodes(model.nodeReader, model.Generation)

	case EventsLoaded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Events = EventState{Status: Ready, Value: message.Events}
		controlPlaneNodes := controlPlaneHostnames(model.Nodes.Value.Nodes)
		if len(controlPlaneNodes) == 0 {
			return model, loadKubernetesNodesIfAvailable(model)
		}
		return model, loadEtcd(model.etcdReader, controlPlaneNodes, message.Generation)

	case EventsFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Events.Status = Failed
		model.Events.Err = "events unavailable"
		return model, nil

	case RefreshEvents:
		model.Events.Status = Loading
		model.Events.Err = ""
		return model, loadEvents(model.eventReader, model.Generation)

	case EtcdLoaded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Etcd = EtcdState{Status: Ready, Value: message.Etcd}
		return model, loadKubernetesNodesIfAvailable(model)

	case EtcdFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Etcd.Status = Failed
		model.Etcd.Err = "etcd unavailable"
		return model, loadKubernetesNodesIfAvailable(model)

	case RefreshEtcd:
		model.Etcd.Status = Loading
		model.Etcd.Err = ""
		controlPlaneNodes := controlPlaneHostnames(model.Nodes.Value.Nodes)
		if len(controlPlaneNodes) == 0 {
			return model, nil
		}
		return model, loadEtcd(model.etcdReader, controlPlaneNodes, model.Generation)

	case RequestEtcdSnapshotPrompt:
		// Snapshot writes a local file and creates an on-node snapshot; treat
		// it as a write path, consistent with "no mutation path by default".
		if !model.WritesEnabled || model.Upgrade.Active {
			return model, nil
		}
		model.EtcdSnapshot = EtcdSnapshotState{Status: Loading, MemberNode: message.Node}
		model.ActionResults = nil
		model.ActionTotal = 0
		return model, defaultEtcdSnapshotPathEffect(model.ContextName, message.MemberHostname, message.Node, model.Generation)

	case EtcdSnapshotPromptOpened:
		return model, nil

	case CancelEtcdSnapshotPrompt:
		model.EtcdSnapshot = EtcdSnapshotState{}
		return model, nil

	case ConfirmEtcdSnapshotPrompt:
		if err := ValidateEtcdSnapshotPath(message.Path); err != nil {
			model.EtcdSnapshot = EtcdSnapshotState{Status: Failed, Err: err.Error(), MemberNode: message.Node}
			return model, nil
		}
		return model, runEtcdSnapshot(model.etcdOperations, message.Node, message.Path, model.Generation)

	case EtcdSnapshotSucceeded:
		if message.Generation != model.Generation {
			return model, nil
		}
		// Destructive membership pipeline: the snapshot is the mandatory first
		// step. Only after it succeeds does the membership RPC run.
		// Identity check: only the snapshot this destructive action asked for
		// may satisfy the stage. An unrelated standalone snapshot that happens
		// to complete first falls through to the standalone branch instead of
		// unlocking a membership change with the wrong backup.
		if model.PendingEtcdAction != nil && model.PendingEtcdAction.Stage == EtcdStageSnapshot &&
			message.Result.Node == model.PendingEtcdAction.SnapshotNode &&
			message.Result.Path == model.PendingEtcdAction.SnapshotPath {
			pending := *model.PendingEtcdAction
			pending.Stage = EtcdStageOperation
			model.PendingEtcdAction.Stage = EtcdStageOperation
			return model, runEtcdMembership(model.etcdOperations, pending, model.Generation)
		}
		model.EtcdSnapshot = EtcdSnapshotState{Status: Ready, Result: message.Result, MemberNode: message.Result.Node}
		model.ActionResults = append(model.ActionResults, ActionResult{Target: message.Result.Node})
		model.ActionTotal = 1
		return model, nil

	case EtcdSnapshotFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		errText := "snapshot failed"
		if message.Err != nil {
			errText = message.Err.Error()
		}
		// Mandatory abort: a failed snapshot must cancel the destructive
		// membership action outright — the membership RPC is never scheduled.
		if model.PendingEtcdAction != nil && model.PendingEtcdAction.Stage == EtcdStageSnapshot {
			target := model.PendingEtcdAction.MemberHostname
			model.PendingEtcdAction = nil
			// Record the failure on the snapshot state too, so a stray standalone
			// snapshot that set Ready cannot mask the aborted removal notice.
			model.EtcdSnapshot = EtcdSnapshotState{Status: Failed, Err: errText, MemberNode: target}
			model.ActionResults = append(model.ActionResults, ActionResult{Target: target, Err: errText})
			model.ActionTotal = 1
			return model, nil
		}
		model.EtcdSnapshot.Status = Failed
		model.EtcdSnapshot.Err = errText
		model.ActionResults = append(model.ActionResults, ActionResult{Target: model.EtcdSnapshot.MemberNode, Err: errText})
		model.ActionTotal = 1
		return model, nil

	case RequestEtcdAction:
		if !model.WritesEnabled || model.Upgrade.Active || message.MemberHostname == "" {
			return model, nil
		}
		pending := PendingEtcdAction{
			Kind:           message.Kind,
			Stage:          EtcdStageIdle,
			MemberID:       message.MemberID,
			MemberHostname: message.MemberHostname,
			Node:           message.Node,
		}
		if isDestructiveEtcdMembership(message.Kind) {
			pending.Warning = etcdMembershipWarning(model.Etcd, message.Kind, message.MemberID, message.MemberHostname)
			pending.Blocked = etcdMembershipBlockReason(model.Etcd, message.Kind, message.MemberID, message.MemberHostname)
			pending.SnapshotNode = etcdSnapshotNodeFor(model.Etcd, message.MemberID, message.MemberHostname)
			pending.SnapshotPath = defaultEtcdSnapshotPath(model.ContextName, message.MemberHostname, time.Now().UTC())
			if message.Kind == EtcdActionLeaveCluster {
				// Leave is executed by the member itself.
				pending.Node = message.MemberHostname
			} else if pending.SnapshotNode != "" && pending.SnapshotNode != pending.MemberHostname {
				// A dead member cannot answer the removal RPC, so address it to
				// the live member the snapshot came from.
				pending.Node = pending.SnapshotNode
			} else if hostname := firstOtherControlPlaneHostname(model.Nodes.Value.Nodes, pending.MemberHostname); hostname != "" {
				// Snapshot source fell back to the target; still address the RPC
				// to a live control-plane node when one is known.
				pending.Node = hostname
			}
			if pending.SnapshotNode == pending.MemberHostname {
				pending.Warning = appendWarning(pending.Warning, "snapshot source is the target member; removal will fail if it is unreachable")
			}
		}
		model.PendingEtcdAction = &pending
		// A pending membership action takes over the footer, so a stale
		// standalone snapshot notice cannot mask its outcome.
		model.EtcdSnapshot = EtcdSnapshotState{}
		model.ActionResults = nil
		model.ActionTotal = 0
		return model, nil

	case ConfirmEtcdAction:
		// Confirm through the reducer only: a blocked, absent, or already-stage-
		// advanced pending action produces no effect, so nothing fires before a
		// successful confirm.
		if model.PendingEtcdAction == nil || model.PendingEtcdAction.Blocked != "" {
			return model, nil
		}
		if model.PendingEtcdAction.Stage == EtcdStageSnapshot || model.PendingEtcdAction.Stage == EtcdStageOperation {
			return model, nil
		}
		if isDestructiveEtcdMembership(model.PendingEtcdAction.Kind) {
			// Re-evaluate against the current snapshot: a removal that became
			// quorum-unsafe (or whose member vanished) after the prompt opened is
			// refused and the prompt stays pending for an explicit cancel.
			if reason := etcdMembershipBlockReason(model.Etcd, model.PendingEtcdAction.Kind, model.PendingEtcdAction.MemberID, model.PendingEtcdAction.MemberHostname); reason != "" {
				model.PendingEtcdAction.Blocked = reason
				return model, nil
			}
			// Refresh the advisory too: the cluster may have degraded (or
			// recovered) since the prompt opened.
			model.PendingEtcdAction.Warning = etcdMembershipWarning(model.Etcd, model.PendingEtcdAction.Kind, model.PendingEtcdAction.MemberID, model.PendingEtcdAction.MemberHostname)
			model.PendingEtcdAction.Stage = EtcdStageSnapshot
			model.ActionTotal = 1
			return model, runEtcdSnapshot(model.etcdOperations, model.PendingEtcdAction.SnapshotNode, model.PendingEtcdAction.SnapshotPath, model.Generation)
		}
		pending := *model.PendingEtcdAction
		model.PendingEtcdAction = nil
		model.ActionTotal = 1
		return model, runEtcdMaintenance(model.etcdOperations, pending, model.Generation)

	case EtcdActionSucceeded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.PendingEtcdAction = nil
		model.ActionResults = append(model.ActionResults, ActionResult{Target: message.MemberHostname})
		// Remove stale membership/alarm state: a successful disarm changes the
		// etct view's ALARMS column, so refresh rather than trust the cached
		// snapshot.
		model.Etcd.Status = Loading
		model.Etcd.Err = ""
		controlPlaneNodes := controlPlaneHostnames(model.Nodes.Value.Nodes)
		if len(controlPlaneNodes) == 0 {
			return model, nil
		}
		return model, loadEtcd(model.etcdReader, controlPlaneNodes, model.Generation)

	case EtcdActionFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		errText := "action failed"
		if message.Err != nil {
			errText = message.Err.Error()
		}
		model.PendingEtcdAction = nil
		model.ActionResults = append(model.ActionResults, ActionResult{Target: message.MemberHostname, Err: errText})
		return model, nil

	case KubernetesNodesLoaded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Kubernetes.Status = Ready
		model.Kubernetes.Nodes = message.Nodes
		model.Nodes.Value.Nodes = mergeKubernetesCorrelation(model.Nodes.Value.Nodes, message.Nodes)
		return model, recoveryEffectIfReady(model)

	case KubernetesNodesFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Kubernetes.Status = Failed
		model.Kubernetes.Err = "kubernetes nodes unavailable"
		return model, nil

	case RefreshKubernetesNodes:
		if model.kubernetesReader == nil {
			return model, nil
		}
		model.Kubernetes.Status = Loading
		model.Kubernetes.Err = ""
		return model, loadKubernetesNodes(model.kubernetesReader, model.Generation)

	case OpenProcesses:
		model.Processes = ProcessesState{Status: Loading, Node: message.Node}
		return model, loadProcesses(model.processReader, message.Node, model.Generation)

	case ProcessesLoaded:
		if message.Generation != model.Generation || message.Node != model.Processes.Node {
			return model, nil
		}
		model.Processes.Status = Ready
		model.Processes.Value = message.Processes
		return model, nil

	case ProcessesFailed:
		if message.Generation != model.Generation || message.Node != model.Processes.Node {
			return model, nil
		}
		model.Processes.Status = Failed
		model.Processes.Err = "processes unavailable"
		return model, nil

	case RefreshProcesses:
		model.Processes.Status = Loading
		model.Processes.Err = ""
		return model, loadProcesses(model.processReader, model.Processes.Node, model.Generation)

	case OpenDisks:
		model.Disks = DisksState{Status: Loading, Node: message.Node}
		return model, loadDisks(model.diskReader, message.Node, model.Generation)

	case DisksLoaded:
		if message.Generation != model.Generation || message.Node != model.Disks.Node {
			return model, nil
		}
		model.Disks.Status = Ready
		model.Disks.Value = message.Disks
		return model, nil

	case DisksFailed:
		if message.Generation != model.Generation || message.Node != model.Disks.Node {
			return model, nil
		}
		model.Disks.Status = Failed
		model.Disks.Err = "disks unavailable"
		return model, nil

	case RefreshDisks:
		model.Disks.Status = Loading
		model.Disks.Err = ""
		return model, loadDisks(model.diskReader, model.Disks.Node, model.Generation)

	case OpenNetwork:
		model.Network = NetworkState{Status: Loading, Node: message.Node}
		return model, loadNetwork(model.networkReader, message.Node, model.Generation)

	case NetworkLoaded:
		if message.Generation != model.Generation || message.Node != model.Network.Node {
			return model, nil
		}
		model.Network.Status = Ready
		model.Network.Value = message.Network
		return model, nil

	case NetworkFailed:
		if message.Generation != model.Generation || message.Node != model.Network.Node {
			return model, nil
		}
		model.Network.Status = Failed
		model.Network.Err = "network unavailable"
		return model, nil

	case RefreshNetwork:
		model.Network.Status = Loading
		model.Network.Err = ""
		return model, loadNetwork(model.networkReader, model.Network.Node, model.Generation)

	case OpenNetstat:
		model.Netstat = SocketState{Status: Loading, Node: message.Node}
		return model, loadNetstat(model.netstatReader, message.Node, model.Generation)

	case NetstatLoaded:
		// Generation alone is not enough: re-opening another node does not bump
		// it, so a late result for the previous node must be dropped by node.
		if message.Generation != model.Generation || message.Node != model.Netstat.Node {
			return model, nil
		}
		model.Netstat.Status = Ready
		model.Netstat.Value = message.Sockets
		return model, nil

	case NetstatFailed:
		if message.Generation != model.Generation || message.Node != model.Netstat.Node {
			return model, nil
		}
		model.Netstat.Status = Failed
		model.Netstat.Err = "netstat unavailable"
		return model, nil

	case RefreshNetstat:
		model.Netstat.Status = Loading
		model.Netstat.Err = ""
		return model, loadNetstat(model.netstatReader, model.Netstat.Node, model.Generation)

	case OpenMounts:
		model.Mounts = MountState{Status: Loading, Node: message.Node}
		return model, loadMounts(model.mountReader, message.Node, model.Generation)

	case MountsLoaded:
		// Generation alone is not enough: re-opening another node does not bump
		// it, so a late result for the previous node must be dropped by node.
		if message.Generation != model.Generation || message.Node != model.Mounts.Node {
			return model, nil
		}
		model.Mounts.Status = Ready
		model.Mounts.Value = message.Mounts
		return model, nil

	case MountsFailed:
		if message.Generation != model.Generation || message.Node != model.Mounts.Node {
			return model, nil
		}
		model.Mounts.Status = Failed
		model.Mounts.Err = "mounts unavailable"
		return model, nil

	case RefreshMounts:
		model.Mounts.Status = Loading
		model.Mounts.Err = ""
		return model, loadMounts(model.mountReader, model.Mounts.Node, model.Generation)

	case OpenMemory:
		model.Memory = MemoryState{Status: Loading, Node: message.Node}
		return model, loadMemory(model.memoryReader, message.Node, model.Generation)

	case MemoryLoaded:
		if message.Generation != model.Generation || message.Node != model.Memory.Node {
			return model, nil
		}
		model.Memory.Status = Ready
		model.Memory.Value = message.Memory
		return model, nil

	case MemoryFailed:
		if message.Generation != model.Generation || message.Node != model.Memory.Node {
			return model, nil
		}
		model.Memory.Status = Failed
		model.Memory.Err = "memory unavailable"
		return model, nil

	case RefreshMemory:
		model.Memory.Status = Loading
		model.Memory.Err = ""
		return model, loadMemory(model.memoryReader, model.Memory.Node, model.Generation)

	case OpenDmesg:
		oldStream := model.dmesgStream
		model.dmesgGeneration++
		model.dmesgStream = nil
		model.Dmesg = DmesgState{Status: Loading, Request: message.Request, Following: true}
		return model, openDmesg(model.dmesgReader, message.Request, model.Generation, model.dmesgGeneration, oldStream)

	case ReconnectDmesg:
		if model.Dmesg.Request.Node == "" {
			return model, nil
		}
		oldStream := model.dmesgStream
		model.dmesgGeneration++
		model.dmesgStream = nil
		model.Dmesg.Status = Loading
		model.Dmesg.Err = ""
		model.Dmesg.EOF = false
		return model, openDmesg(model.dmesgReader, model.Dmesg.Request, model.Generation, model.dmesgGeneration, oldStream)

	case CloseDmesg:
		stream := model.dmesgStream
		model.dmesgGeneration++
		model.dmesgStream = nil
		model.Dmesg = DmesgState{}
		return model, closeDmesg(stream)

	case ClearDmesg:
		model.Dmesg.Lines = nil
		return model, nil

	case dmesgOpened:
		if message.Generation != model.Generation || message.StreamGeneration != model.dmesgGeneration {
			return model, closeDmesg(message.Stream)
		}
		if message.Err != nil || message.Stream == nil {
			model.Dmesg.Status = Failed
			model.Dmesg.Err = "dmesg stream unavailable"
			return model, nil
		}
		model.dmesgStream = message.Stream
		model.Dmesg.Status = Ready
		return model, readDmesgBatch(message.Stream, message.Generation, message.StreamGeneration)

	case DmesgBatchLoaded:
		if message.Generation != model.Generation || model.dmesgGeneration != 0 && message.StreamGeneration != model.dmesgGeneration {
			return model, nil
		}
		if len(message.Batch.Lines) > 0 {
			model.Dmesg.Lines = append(model.Dmesg.Lines, message.Batch.Lines...)
			if excess := len(model.Dmesg.Lines) - 2000; excess > 0 {
				model.Dmesg.Lines = append([]string(nil), model.Dmesg.Lines[excess:]...)
			}
		}
		if message.Err != nil || message.Batch.Err != "" {
			model.Dmesg.Status = Failed
			model.Dmesg.Err = "dmesg stream unavailable"
			return model, nil
		}
		if message.Batch.EOF {
			model.Dmesg.EOF = true
			return model, nil
		}
		return model, readDmesgBatch(model.dmesgStream, message.Generation, message.StreamGeneration)

	case OpenClusterHealth:
		oldStream := model.clusterHealthStream
		model.healthGeneration++
		model.clusterHealthStream = nil
		model.HealthCheck = ClusterHealthState{Status: Loading, Request: message.Request}
		return model, openClusterHealth(model.clusterHealthReader, message.Request, model.Generation, model.healthGeneration, oldStream)

	case CloseClusterHealth:
		stream := model.clusterHealthStream
		model.healthGeneration++
		model.clusterHealthStream = nil
		model.HealthCheck = ClusterHealthState{}
		return model, closeClusterHealth(stream)

	case ClearClusterHealth:
		model.HealthCheck.Lines = nil
		return model, nil

	case clusterHealthOpened:
		if message.Generation != model.Generation || message.StreamGeneration != model.healthGeneration {
			return model, closeClusterHealth(message.Stream)
		}
		if message.Err != nil || message.Stream == nil {
			model.HealthCheck.Status = Failed
			model.HealthCheck.Err = "cluster health check unavailable"
			return model, nil
		}
		model.clusterHealthStream = message.Stream
		model.HealthCheck.Status = Ready
		return model, readClusterHealthBatch(message.Stream, message.Generation, message.StreamGeneration)

	case ClusterHealthProgressLoaded:
		if message.Generation != model.Generation || model.healthGeneration != 0 && message.StreamGeneration != model.healthGeneration {
			return model, nil
		}
		if message.Progress.Message != "" {
			model.HealthCheck.Lines = append(model.HealthCheck.Lines, message.Progress.Message)
			if excess := len(model.HealthCheck.Lines) - maxClusterHealthLines; excess > 0 {
				model.HealthCheck.Lines = append([]string(nil), model.HealthCheck.Lines[excess:]...)
			}
		}
		if message.Err != nil || message.Progress.Err != "" {
			model.HealthCheck.Status = Failed
			model.HealthCheck.Err = "cluster health check unavailable"
			return model, nil
		}
		if message.Progress.EOF {
			model.HealthCheck.EOF = true
			model.HealthCheck.VerdictReady = true
			model.HealthCheck.Status = Ready
			return model, nil
		}
		return model, readClusterHealthBatch(model.clusterHealthStream, message.Generation, message.StreamGeneration)

	case RequestAction:
		// Defense in depth: the TUI already refuses to send RequestAction
		// while writes are disabled, but the reducer must not trust that —
		// it is the last line of defense against a cluster-mutating action.
		if !model.WritesEnabled || model.Upgrade.Active || len(message.Targets) == 0 || message.Kind == ActionUpgrade && len(message.Targets) != 1 {
			return model, nil
		}
		blocked := ""
		if targetsIncludeControlPlane(model.Nodes.Value.Nodes, message.Targets) {
			blocked = etcdQuorumBlockReason(model.Etcd, message.Targets)
		}
		model.PendingAction = &PendingAction{
			Kind:    message.Kind,
			Targets: append([]string(nil), message.Targets...),
			Warning: func() string {
				if message.Kind == ActionUpgrade {
					return UpgradeActionWarning(model.Nodes.Value.Nodes, model.Etcd, message.Targets, message.Image)
				}
				return computeActionWarning(model.Nodes.Value.Nodes, model.Etcd, message.Targets)
			}(),
			Blocked: blocked,
			Image:   message.Image,
		}
		model.ActionResults = nil
		model.ActionTotal = 0
		return model, nil

	case RequestServiceAction:
		if !model.WritesEnabled || model.Upgrade.Active || message.Node == "" || message.Service == "" {
			return model, nil
		}
		warning := ""
		blocked := ""
		if message.Service == "etcd" && message.Kind != ServiceActionStart {
			warning = computeEtcdQuorumWarning(model.Etcd, []string{message.Node})
			blocked = etcdQuorumBlockReason(model.Etcd, []string{message.Node})
		}
		model.PendingServiceAction = &PendingServiceAction{
			Kind:    message.Kind,
			Node:    message.Node,
			Service: message.Service,
			Warning: warning,
			Blocked: blocked,
		}
		model.ActionResults = nil
		model.ActionTotal = 0
		return model, nil

	case RequestUpgradePrompt:
		// Defense in depth: mirrors the RequestAction/RequestServiceAction
		// gate above — the reducer is the last line of defense against a
		// cluster-mutating action, not the TUI's own key-handler gating.
		if !model.WritesEnabled || model.Upgrade.Active {
			return model, nil
		}
		return model, requestUpgradeImage(model.nodeController, message.Target, model.Generation)

	case UpgradePromptOpened:
		return model, nil

	case CancelPendingAction:
		model.PendingAction = nil
		model.PendingServiceAction = nil
		model.PendingEtcdAction = nil
		return model, nil

	case ConfirmPendingAction:
		// Hard gate, re-evaluated against the current snapshot: an action that
		// is (or became) quorum-unsafe is refused outright and the pending
		// prompt is left in place so the operator must cancel it explicitly.
		if model.PendingAction != nil {
			if reason := pendingActionBlockReason(model, *model.PendingAction); reason != "" {
				model.PendingAction.Blocked = reason
				return model, nil
			}
		}
		if model.PendingServiceAction != nil {
			if reason := pendingServiceActionBlockReason(model, *model.PendingServiceAction); reason != "" {
				model.PendingServiceAction.Blocked = reason
				return model, nil
			}
		}
		if model.PendingAction != nil {
			model.ActionTotal = len(model.PendingAction.Targets)
		} else if model.PendingServiceAction != nil {
			model.ActionTotal = 1
		}
		model.PendingAction = nil
		model.PendingServiceAction = nil
		return model, nil

	case UpgradeStarted:
		if message.Generation != model.Generation || model.Upgrade.Active {
			if message.cancel != nil {
				message.cancel()
			}
			return model, nil
		}
		if message.results == nil {
			return model, nil
		}
		model.Upgrade = UpgradeState{Active: true, Target: message.Target}
		model.upgradeResults = message.results
		model.upgradeCancel = message.cancel
		return model, readUpgradeUpdate(model.upgradeResults, message.Generation, message.Target)

	case UpgradeProgressed:
		if message.Generation != model.Generation || !model.Upgrade.Active || message.Target != model.Upgrade.Target {
			return model, nil
		}
		model.Upgrade.Event = message.Event
		return model, readUpgradeUpdate(model.upgradeResults, message.Generation, message.Target)

	case UpgradeAppliedWithRecoveryWarning:
		if message.Generation != model.Generation || !model.Upgrade.Active || message.Target != model.Upgrade.Target {
			return model, nil
		}
		finishUpgrade(&model)
		model.Upgrade.Warning = message.Warning
		model.ActionResults = append(model.ActionResults, ActionResult{Target: message.Target, Warning: message.Warning})
		return model, nil

	case UpgradeSucceeded:
		if message.Generation != model.Generation || !model.Upgrade.Active || message.Target != model.Upgrade.Target {
			return model, nil
		}
		finishUpgrade(&model)
		model.ActionResults = append(model.ActionResults, ActionResult{Target: message.Target})
		if model.nodeReader == nil {
			return model, nil
		}
		model.Nodes.Status = Loading
		model.Nodes.Err = ""
		return model, loadNodes(model.nodeReader, model.Generation)

	case RecoveryUncordonSucceeded:
		if message.Generation != model.Generation || model.Upgrade.Target != message.Target || model.Upgrade.Warning == "" {
			return model, nil
		}
		model.Upgrade.Warning = ""
		return model, nil

	case RecoveryUncordonFailed:
		// A single failed attempt is not escalated to the operator: it is
		// usually transient, and the existing warning already promises a
		// retry. The next heartbeat tick tries again automatically.
		return model, nil

	case UpgradeFailed:
		if message.Generation != model.Generation || !model.Upgrade.Active || message.Target != model.Upgrade.Target {
			return model, nil
		}
		errText := safeUpgradeError(message.Err)
		finishUpgrade(&model)
		model.Upgrade.Err = errText
		model.ActionResults = append(model.ActionResults, ActionResult{Target: message.Target, Err: errText})
		return model, nil

	case ActionSucceeded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.ActionResults = append(model.ActionResults, ActionResult{Target: message.Target})
		return model, nil

	case ActionFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		errText := "action failed"
		if message.Err != nil {
			errText = message.Err.Error()
		}
		model.ActionResults = append(model.ActionResults, ActionResult{Target: message.Target, Err: errText})
		return model, nil

	case OpenResourceBrowser:
		model.ResourceBrowser = ResourceBrowserState{KindsStatus: Loading}
		if message.Kind != "" {
			model.ResourceBrowser.InstancesStatus = Loading
			model.ResourceBrowser.SelectedKind = message.Kind
			model.ResourceBrowser.SelectedNode = message.Node
		}
		return model, loadResourceKinds(model.resourceKindReader, model.Generation)

	case ResourceKindsLoaded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.ResourceBrowser.KindsStatus = Ready
		model.ResourceBrowser.Kinds = message.Kinds
		return model, nil

	case ResourceKindsFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.ResourceBrowser.KindsStatus = Failed
		model.ResourceBrowser.KindsErr = "resource kinds unavailable"
		return model, nil

	case SelectResourceKind:
		model.ResourceBrowser.InstancesStatus = Loading
		model.ResourceBrowser.InstancesErr = ""
		model.ResourceBrowser.SelectedKind = message.Kind
		model.ResourceBrowser.SelectedNode = message.Node
		return model, loadResourceInstances(model.resourceInstanceReader, message.Node, message.Kind, model.Generation)

	case ResourceInstancesLoaded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.ResourceBrowser.InstancesStatus = Ready
		model.ResourceBrowser.Instances = message.Instances
		return model, nil

	case ResourceInstancesFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.ResourceBrowser.InstancesStatus = Failed
		model.ResourceBrowser.InstancesErr = "resource instances unavailable"
		return model, nil

	case OpenResourceInstance:
		model.ResourceBrowser.DetailStatus = Loading
		model.ResourceBrowser.DetailErr = ""
		return model, loadResourceInstance(model.resourceInstanceReader, model.ResourceBrowser.SelectedNode, model.ResourceBrowser.SelectedKind, message.ID, model.Generation)

	case ResourceInstanceLoaded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.ResourceBrowser.DetailStatus = Ready
		model.ResourceBrowser.Detail = message.Instance
		return model, nil

	case ResourceInstanceFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.ResourceBrowser.DetailStatus = Failed
		model.ResourceBrowser.DetailErr = "resource instance unavailable"
		return model, nil

	case OpenServiceLogs:
		oldStream := model.logStream
		model.logGeneration++
		model.logStream = nil
		model.Logs = LogState{Status: Loading, Request: message.Request, Following: true}
		return model, openServiceLogs(model.logReader, message.Request, model.Generation, model.logGeneration, oldStream)

	case ReconnectServiceLogs:
		if model.Logs.Request.Node == "" || model.Logs.Request.Service == "" {
			return model, nil
		}
		oldStream := model.logStream
		model.logGeneration++
		model.logStream = nil
		model.Logs.Status = Loading
		model.Logs.Err = ""
		model.Logs.EOF = false
		return model, openServiceLogs(model.logReader, model.Logs.Request, model.Generation, model.logGeneration, oldStream)

	case CloseServiceLogs:
		stream := model.logStream
		model.logGeneration++
		model.logStream = nil
		model.Logs = LogState{}
		return model, closeServiceLogs(stream)

	case ClearServiceLogs:
		model.Logs.Lines = nil
		return model, nil

	case serviceLogOpened:
		if message.Generation != model.Generation || message.StreamGeneration != model.logGeneration {
			return model, closeServiceLogs(message.Stream)
		}
		if message.Err != nil || message.Stream == nil {
			model.Logs.Status = Failed
			model.Logs.Err = "log stream unavailable"
			return model, nil
		}
		model.logStream = message.Stream
		model.Logs.Status = Ready
		return model, readServiceLogBatch(message.Stream, message.Generation, message.StreamGeneration)

	case ServiceLogBatchLoaded:
		if message.Generation != model.Generation || model.logGeneration != 0 && message.StreamGeneration != model.logGeneration {
			return model, nil
		}
		if len(message.Batch.Lines) > 0 {
			model.Logs.Lines = append(model.Logs.Lines, message.Batch.Lines...)
			if excess := len(model.Logs.Lines) - 2000; excess > 0 {
				model.Logs.Lines = append([]string(nil), model.Logs.Lines[excess:]...)
			}
		}
		if message.Err != nil || message.Batch.Err != "" {
			model.Logs.Status = Failed
			model.Logs.Err = "log stream unavailable"
			return model, nil
		}
		if message.Batch.EOF {
			model.Logs.EOF = true
			return model, nil
		}
		return model, readServiceLogBatch(model.logStream, message.Generation, message.StreamGeneration)

	case LoadFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		errText := "load failed"
		if message.Err != nil {
			errText = message.Err.Error()
		}
		model.Nodes = NodeState{Status: Failed, Err: errText}
		model.Notice = errText
		return model, nil

	default:
		return model, nil
	}
}

func nodeLoadStatus(nodes domain.NodeSet) LoadStatus {
	for _, node := range nodes.Nodes {
		if node.Problem != "" {
			return Partial
		}
	}

	return Ready
}

func serviceLoadStatus(services domain.ServiceSet) LoadStatus {
	if len(services.Problems) > 0 {
		return Partial
	}
	return Ready
}

func loadKubernetesNodesIfAvailable(model Model) Effect {
	if model.kubernetesReader == nil {
		return nil
	}
	return loadKubernetesNodes(model.kubernetesReader, model.Generation)
}

// recoveryEffectIfReady closes the loop left by an applied-with-warning
// upgrade: once a later Kubernetes node refresh shows the pending target is
// Ready again, it fires an opportunistic uncordon instead of leaving the
// operator to run kubectl by hand. Uncordon is idempotent, so retrying it on
// every refresh until the warning clears is safe.
func recoveryEffectIfReady(model Model) Effect {
	if model.Upgrade.Warning == "" || model.Upgrade.Active {
		return nil
	}
	target := model.Upgrade.Target
	for _, node := range model.Nodes.Value.Nodes {
		if node.Target() != target {
			continue
		}
		if node.Kubernetes != domain.KubernetesReady {
			return nil
		}
		return recoveryUncordonEffect(model.nodeController, target, model.Generation)
	}
	return nil
}

func mergeKubernetesCorrelation(nodes []domain.NodeSnapshot, kubernetesNodes map[string]domain.KubernetesNodeSnapshot) []domain.NodeSnapshot {
	merged := make([]domain.NodeSnapshot, len(nodes))
	for index, node := range nodes {
		merged[index] = node
		snapshot, ok := lookupKubernetesCorrelation(node, kubernetesNodes)
		if !ok {
			continue
		}
		merged[index].KubernetesNode = &snapshot
		merged[index].Kubernetes = kubernetesReadiness(snapshot)
	}
	return merged
}

func lookupKubernetesCorrelation(node domain.NodeSnapshot, kubernetesNodes map[string]domain.KubernetesNodeSnapshot) (domain.KubernetesNodeSnapshot, bool) {
	if node.Name != "" {
		if snapshot, ok := kubernetesNodes[node.Name]; ok {
			return snapshot, true
		}
	}
	for _, address := range node.Addresses {
		if snapshot, ok := kubernetesNodes[address]; ok {
			return snapshot, true
		}
	}
	return domain.KubernetesNodeSnapshot{}, false
}

func kubernetesReadiness(snapshot domain.KubernetesNodeSnapshot) domain.KubernetesState {
	for _, condition := range snapshot.Conditions {
		if condition.Type != "Ready" {
			continue
		}
		if condition.Status == "True" {
			return domain.KubernetesReady
		}
		return domain.KubernetesNotReady
	}
	return domain.KubernetesUnknown
}

func controlPlaneHostnames(nodes []domain.NodeSnapshot) []string {
	hostnames := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Role != domain.NodeRoleControl {
			continue
		}
		target := node.Name
		if target == "" && len(node.Addresses) > 0 {
			target = node.Addresses[0]
		}
		if target == "" {
			continue
		}
		hostnames = append(hostnames, target)
	}
	return hostnames
}

// ClusterHealthRequestFromNodes builds the ClusterInfo request from the current
// node snapshots. Only parseable IP addresses are included: the Talos server
// rejects hostnames, so Names are used only when they themselves parse as an
// IP. Empty lists are tolerated — the server falls back to its own discovery.
func ClusterHealthRequestFromNodes(nodes []domain.NodeSnapshot, timeout time.Duration) domain.ClusterHealthRequest {
	var controlPlane, workers []string
	for _, node := range nodes {
		switch node.Role {
		case domain.NodeRoleControl:
			controlPlane = appendNodeIP(controlPlane, node)
		case domain.NodeRoleWorker:
			workers = appendNodeIP(workers, node)
		}
	}
	return domain.ClusterHealthRequest{ControlPlaneNodes: controlPlane, WorkerNodes: workers, WaitTimeout: timeout}
}

func appendNodeIP(target []string, node domain.NodeSnapshot) []string {
	for _, candidate := range node.Addresses {
		if _, err := netip.ParseAddr(candidate); err == nil {
			return append(target, candidate)
		}
	}
	if node.Name != "" {
		if _, err := netip.ParseAddr(node.Name); err == nil {
			return append(target, node.Name)
		}
	}
	return target
}

// ControlPlaneHostnamesForTest exposes controlPlaneHostnames for tests in
// package application_test, which cannot see unexported identifiers.
func ControlPlaneHostnamesForTest(nodes []domain.NodeSnapshot) []string {
	return controlPlaneHostnames(nodes)
}

func safeUpgradeError(_ error) string {
	return "upgrade failed"
}

func cancelActiveUpgrade(model *Model) {
	if model.upgradeCancel != nil {
		model.upgradeCancel()
	}
	model.upgradeCancel = nil
	model.upgradeResults = nil
	model.Upgrade = UpgradeState{}
}

func finishUpgrade(model *Model) {
	if model.upgradeCancel != nil {
		model.upgradeCancel()
	}
	model.upgradeCancel = nil
	model.upgradeResults = nil
	model.Upgrade.Active = false
}
