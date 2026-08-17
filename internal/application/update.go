package application

import (
	"fmt"

	"github.com/m11s-io/t9s/internal/domain"
)

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
		model.ResourceBrowser = ResourceBrowserState{}
		model.Logs = LogState{}
		model.logStream = nil
		model.logGeneration++
		model.Notice = ""
		model.PendingAction = nil
		model.ActionResults = nil
		model.ActionTotal = 0
		return model, openSession(message.Name, model.Generation)

	case SessionOpened:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.nodeReader = message.Nodes
		model.nodeController = message.NodeController
		model.serviceReader = message.Services
		model.logReader = message.Logs
		model.eventReader = message.Events
		model.etcdReader = message.Etcd
		model.processReader = message.Processes
		model.diskReader = message.Disks
		model.networkReader = message.Network
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

	case KubernetesNodesLoaded:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Kubernetes.Status = Ready
		model.Kubernetes.Nodes = message.Nodes
		model.Nodes.Value.Nodes = mergeKubernetesCorrelation(model.Nodes.Value.Nodes, message.Nodes)
		return model, nil

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
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Processes.Status = Ready
		model.Processes.Value = message.Processes
		return model, nil

	case ProcessesFailed:
		if message.Generation != model.Generation {
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
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Disks.Status = Ready
		model.Disks.Value = message.Disks
		return model, nil

	case DisksFailed:
		if message.Generation != model.Generation {
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
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Network.Status = Ready
		model.Network.Value = message.Network
		return model, nil

	case NetworkFailed:
		if message.Generation != model.Generation {
			return model, nil
		}
		model.Network.Status = Failed
		model.Network.Err = "network unavailable"
		return model, nil

	case RefreshNetwork:
		model.Network.Status = Loading
		model.Network.Err = ""
		return model, loadNetwork(model.networkReader, model.Network.Node, model.Generation)

	case RequestAction:
		// Defense in depth: the TUI already refuses to send RequestAction
		// while writes are disabled, but the reducer must not trust that —
		// it is the last line of defense against a cluster-mutating action.
		if !model.WritesEnabled || len(message.Targets) == 0 {
			return model, nil
		}
		model.PendingAction = &PendingAction{
			Kind:    message.Kind,
			Targets: append([]string(nil), message.Targets...),
			Warning: computeActionWarning(model.Nodes.Value.Nodes, model.Etcd, message.Targets),
			Image:   message.Image,
		}
		model.ActionResults = nil
		model.ActionTotal = 0
		return model, nil

	case CancelPendingAction:
		model.PendingAction = nil
		return model, nil

	case ConfirmPendingAction:
		if model.PendingAction != nil {
			model.ActionTotal = len(model.PendingAction.Targets)
		}
		model.PendingAction = nil
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

// ControlPlaneHostnamesForTest exposes controlPlaneHostnames for tests in
// package application_test, which cannot see unexported identifiers.
func ControlPlaneHostnamesForTest(nodes []domain.NodeSnapshot) []string {
	return controlPlaneHostnames(nodes)
}
