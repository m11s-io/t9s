package application

import (
	"context"
	"maps"
	"time"
)

// Update applies one input or operation result. Request lifetime belongs to the
// session owner; screen data and current request identities stay in Model.
func Update(model Model, message Message) (Model, Effect) {
	if model.MutationActive() && changesMutationPrompt(message) {
		return model, nil
	}
	_, operationID := identifyOperation(message, 0)
	if operationID != 0 && operationID != model.operationID {
		if started, ok := message.(UpgradeStarted); ok && started.cancel != nil {
			started.cancel()
		}
		return model, nil
	}
	_, key, id := identifyRead(message, 0)
	if id != 0 && model.requestIDs[key] != id {
		return model, nil
	}
	if close, ok := message.(CloseRead); ok {
		model = invalidateRead(model, close.Key)
		model.requests.cancelThrough(close.Key, model.requestIDs[close.Key])
		return model, nil
	}
	switch message.(type) {
	case OpenResourceBrowser:
		model = closeRead(model, "instances")
		model = closeRead(model, "detail")
	case CancelEtcdSnapshotPrompt:
		model = closeRead(model, "snapshot-prompt")
	case SelectResourceKind:
		model = closeRead(model, "detail")
	case RequestAction, RequestServiceAction, RequestEtcdAction, CancelPendingAction:
		model = closeRead(model, "node-prompt")
		model = closeRead(model, "snapshot-prompt")
	}
	if request, ok := message.(RequestEtcdAction); ok {
		request.requestedAt = time.Now().UTC()
		message = request
	}
	next, effect := updateModel(model, message)
	if next.operationID != model.operationID && !next.Upgrade.Active {
		// A previous terminal upgrade notice must not mask a newer action.
		next.Upgrade = UpgradeState{}
	}
	if startsOperation(message) && effect != nil {
		next.operationTargets = acceptedOperationTargets(model, message)
		// Async prompt lookups no longer belong to this accepted operation.
		next = closeRead(next, "node-prompt")
		next = closeRead(next, "snapshot-prompt")
		next.PendingAction = nil
		next.PendingServiceAction = nil
		if _, membership := message.(ConfirmEtcdAction); !membership {
			next.PendingEtcdAction = nil
		}
	}
	next = finishOperationTarget(next, message)
	if next.Generation != model.Generation {
		next.operationTargets = nil
		if model.requests != nil {
			model.requests.close()
		}
		next.sessionRuntime = sessionRuntime{requests: newRequestOwner(context.Background())}
		next.requestIDs = nil
	}
	if opened, ok := message.(SessionOpened); ok && opened.Generation == next.Generation {
		return startSessionLoads(next)
	}
	if key := streamOpenIntent(message, next); key != "" && effect != nil {
		if key == "event-follow" {
			next = closeRead(next, "events")
		}
		return trackStream(next, key, effect)
	}
	for _, key := range streamCloseIntent(message, next) {
		next = closeRead(next, key)
	}
	if (operationID != 0 || startsOperation(message)) && effect != nil {
		if next.requests == nil {
			next.requests = newRequestOwner(context.Background())
		}
		effect = operationEffect(next, effect)
	}
	key = readIntent(message)
	if key != "" && effect != nil {
		return track(next, key, effect)
	}
	return next, effect
}

// CloseRead releases an inactive diagnostic without discarding its last data.
// Keys are the diagnostic names used by the TUI, not arbitrary commands.
type CloseRead struct{ Key string }

func (CloseRead) applicationMessage() {}

func invalidateRead(model Model, key string) Model {
	if model.requests == nil {
		model.requests = newRequestOwner(context.Background())
	}
	model.requestIDs = maps.Clone(model.requestIDs)
	if model.requestIDs == nil {
		model.requestIDs = make(map[string]uint64)
	}
	model.requestIDs[key]++
	model.requests.cancelThrough(key, model.requestIDs[key]-1)
	return model
}

func track(model Model, key string, effect Effect) (Model, Effect) {
	if effect == nil {
		return model, nil
	}
	model = invalidateRead(model, key)
	if key == "nodes" && model.nodeServices != nil {
		model = invalidateRead(model, "services")
		model.Services.Status = Loading
	}
	id, generation, owner := model.requestIDs[key], model.Generation, model.requests
	return model, func(ctx context.Context, deps Dependencies) Message {
		ctx, finish, err := owner.begin(ctx, key, id, 15*time.Second)
		if err != nil {
			failure := readFailure(key, generation, err, model)
			if key == "nodes" && model.nodeServices != nil {
				failure = NodeServicesLoaded{Generation: generation, ServicesRequestID: model.requestIDs["services"], Err: err}
			}
			message, _, _ := identifyRead(failure, id)
			return message
		}
		defer finish()
		result := effect(ctx, deps)
		if observation, ok := result.(NodeServicesLoaded); ok {
			observation.ServicesRequestID = model.requestIDs["services"]
			result = observation
		}
		message, _, _ := identifyRead(result, id)
		return message
	}
}

func readIntent(message Message) string {
	switch message.(type) {
	case RequestUpgradePrompt:
		return "node-prompt"
	case RequestResetPrompt:
		return "node-prompt"
	case RequestEtcdSnapshotPrompt:
		return "snapshot-prompt"
	case RefreshNodes, UpgradeSucceeded:
		return "nodes"
	case RefreshServices:
		return "services"
	case RefreshEvents:
		return "events"
	case RefreshEtcd, EtcdActionSucceeded:
		return "etcd"
	case RefreshKubernetesNodes:
		return "kubernetes"
	case OpenProcesses, RefreshProcesses:
		return "processes"
	case OpenDisks, RefreshDisks, ActionSucceeded:
		return "disks"
	case OpenNetwork, RefreshNetwork:
		return "network"
	case OpenNetstat, RefreshNetstat:
		return "netstat"
	case OpenMounts, RefreshMounts:
		return "mounts"
	case OpenMemory, RefreshMemory:
		return "memory"
	case OpenResourceBrowser:
		return "kinds"
	case SelectResourceKind:
		return "instances"
	case OpenResourceInstance:
		return "detail"
	case OpenTime, RefreshTime:
		return "time"
	case CheckTime:
		return "time-check"
	case OpenCPU, RefreshCPU:
		return "cpu"
	}
	return ""
}

// Explicit typed results keep SDK and frontend types out of dispatch.
func identifyRead(message Message, stamp uint64) (Message, string, uint64) {
	switch m := message.(type) {
	case UpgradePromptOpened:
		id := stampResultID(&m.RequestID, stamp)
		return m, "node-prompt", id
	case ResetPromptOpened:
		id := stampResultID(&m.RequestID, stamp)
		return m, "node-prompt", id
	case EtcdSnapshotPromptOpened:
		id := stampResultID(&m.RequestID, stamp)
		return m, "snapshot-prompt", id

	case NodeServicesLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "nodes", id
	case NodesLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "nodes", id
	case LoadFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "nodes", id
	case ServicesLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "services", id
	case ServicesFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "services", id
	case EventsLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "events", id
	case EventsFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "events", id
	case EtcdLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "etcd", id
	case EtcdFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "etcd", id
	case KubernetesNodesLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "kubernetes", id
	case KubernetesNodesFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "kubernetes", id
	case ProcessesLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "processes", id
	case ProcessesFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "processes", id
	case DisksLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "disks", id
	case DisksFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "disks", id
	case NetworkLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "network", id
	case NetworkFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "network", id
	case NetstatLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "netstat", id
	case NetstatFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "netstat", id
	case MountsLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "mounts", id
	case MountsFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "mounts", id
	case MemoryLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "memory", id
	case MemoryFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "memory", id
	case ResourceKindsLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "kinds", id
	case ResourceKindsFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "kinds", id
	case ResourceInstancesLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "instances", id
	case ResourceInstancesFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "instances", id
	case ResourceInstanceLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "detail", id
	case ResourceInstanceFailed:
		id := stampResultID(&m.RequestID, stamp)
		return m, "detail", id
	case TimeLoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "time", id
	case TimeChecked:
		id := stampResultID(&m.RequestID, stamp)
		return m, "time-check", id
	case CPULoaded:
		id := stampResultID(&m.RequestID, stamp)
		return m, "cpu", id
	}
	return message, "", 0
}

func readFailure(key string, generation uint64, err error, model Model) Message {
	switch key {
	case "nodes":
		return LoadFailed{Generation: generation, Err: err}
	case "services":
		return ServicesFailed{Generation: generation, Err: err}
	case "events":
		return EventsFailed{Generation: generation, Err: err}
	case "etcd":
		return EtcdFailed{Generation: generation, Err: err}
	case "kubernetes":
		return KubernetesNodesFailed{Generation: generation, Err: err}
	case "processes":
		return ProcessesFailed{Generation: generation, Node: model.Processes.Node, Err: err}
	case "disks":
		return DisksFailed{Generation: generation, Node: model.Disks.Node, Err: err}
	case "network":
		return NetworkFailed{Generation: generation, Node: model.Network.Node, Err: err}
	case "netstat":
		return NetstatFailed{Generation: generation, Node: model.Netstat.Node, Err: err}
	case "mounts":
		return MountsFailed{Generation: generation, Node: model.Mounts.Node, Err: err}
	case "memory":
		return MemoryFailed{Generation: generation, Node: model.Memory.Node, Err: err}
	case "kinds":
		return ResourceKindsFailed{Generation: generation, Err: err}
	case "instances":
		return ResourceInstancesFailed{Generation: generation, Err: err}
	case "detail":
		return ResourceInstanceFailed{Generation: generation, Err: err}
	case "time":
		return TimeLoaded{Generation: generation, Node: model.Time.Node, Err: err}
	case "time-check":
		return TimeChecked{Generation: generation, Node: model.Time.Node, Server: model.Time.Check.Server, Err: err}
	case "cpu":
		return CPULoaded{Generation: generation, Node: model.CPU.Node, Err: err}
	}
	return nil
}

func closeRead(model Model, key string) Model {
	model = invalidateRead(model, key)
	model.requests.cancelThrough(key, model.requestIDs[key])
	return model
}

// ResultScope lets presentation adapters refresh only data touched by a result.
func ResultScope(message Message) string {
	switch message.(type) {
	case NodeServicesLoaded:
		return "node-services"
	case serviceLogOpened, ServiceLogBatchLoaded:
		return "logs"
	case dmesgOpened, DmesgBatchLoaded:
		return "dmesg"
	case clusterHealthOpened, ClusterHealthProgressLoaded:
		return "healthcheck"
	case eventFollowOpened, eventFollowBatch:
		return "events"
	}
	_, key, _ := identifyRead(message, 0)
	return key
}

func trackStream(model Model, key string, effect Effect) (Model, Effect) {
	model = invalidateRead(model, key)
	id, owner := model.requestIDs[key], model.requests
	return model, func(ctx context.Context, deps Dependencies) Message {
		streamCtx, finish, err := owner.begin(ctx, key, id, 0)
		if err != nil {
			switch key {
			case "logs":
				return serviceLogOpened{Generation: model.Generation, StreamGeneration: model.logGeneration, Err: err}
			case "dmesg":
				return dmesgOpened{Generation: model.Generation, StreamGeneration: model.dmesgGeneration, Err: err}
			case "healthcheck":
				return clusterHealthOpened{Generation: model.Generation, StreamGeneration: model.healthGeneration, Err: err}
			case "event-follow":
				return eventFollowOpened{Generation: model.Generation, StreamGeneration: model.eventGeneration, Err: err}
			}
			return nil
		}
		message := effect(streamCtx, deps)
		failed := false
		switch opened := message.(type) {
		case serviceLogOpened:
			failed = opened.Err != nil || opened.Stream == nil
		case dmesgOpened:
			failed = opened.Err != nil || opened.Stream == nil
		case clusterHealthOpened:
			failed = opened.Err != nil || opened.Stream == nil
		case eventFollowOpened:
			failed = opened.Err != nil || opened.Stream == nil
		}
		if failed {
			finish()
		}
		return message
	}
}

func streamOpenIntent(message Message, next Model) string {
	switch message.(type) {
	case OpenServiceLogs, ReconnectServiceLogs:
		return "logs"
	case OpenDmesg, ReconnectDmesg:
		return "dmesg"
	case OpenClusterHealth:
		return "healthcheck"
	case ToggleEventFollow:
		if next.Events.Following {
			return "event-follow"
		}
	}
	return ""
}
func streamCloseIntent(message Message, next Model) []string {
	switch message.(type) {
	case CloseServiceLogs:
		return []string{"logs"}
	case CloseDmesg:
		return []string{"dmesg"}
	case CloseClusterHealth:
		return []string{"healthcheck"}
	case StopEventFollow:
		return []string{"event-follow"}
	case ToggleEventFollow:
		if !next.Events.Following {
			return []string{"event-follow"}
		}
	case ServiceLogBatchLoaded:
		if next.Logs.EOF || next.Logs.Status == Failed {
			return []string{"logs"}
		}
	case DmesgBatchLoaded:
		if next.Dmesg.EOF || next.Dmesg.Status == Failed {
			return []string{"dmesg"}
		}
	case ClusterHealthProgressLoaded:
		if next.HealthCheck.EOF || next.HealthCheck.Status == Failed {
			return []string{"healthcheck"}
		}
	case eventFollowBatch:
		if !next.Events.Following {
			return []string{"event-follow"}
		}
	}
	return nil
}

func stampResultID(field *uint64, stamp uint64) uint64 {
	id := *field
	if stamp != 0 {
		*field = stamp
	}
	return id
}

// IsCurrentResult prevents frontends from opening prompts for replaced reads.
func IsCurrentResult(model Model, message Message) bool {
	_, key, id := identifyRead(message, 0)
	if id != 0 && model.requestIDs[key] != id {
		return false
	}
	_, operationID := identifyOperation(message, 0)
	return operationID == 0 || operationID == model.operationID
}
