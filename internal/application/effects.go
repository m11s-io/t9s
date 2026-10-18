package application

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type Dependencies struct {
	ContextCatalog     ports.ContextCatalog
	SessionFactory     ports.SessionFactory
	KubernetesResolver ports.KubernetesResolver
	openSession        func(context.Context, string, uint64) (ports.Session, error)
	sessionRequests    func() *requestOwner
}

type Effect func(context.Context, Dependencies) Message

func loadContexts(contextOverride string, generation uint64) Effect {
	return func(ctx context.Context, dependencies Dependencies) Message {
		if dependencies.ContextCatalog == nil {
			return LoadFailed{Generation: generation, Err: fmt.Errorf("load contexts: context catalog is not configured")}
		}

		contexts, err := dependencies.ContextCatalog.List(ctx)
		if err != nil {
			return LoadFailed{Generation: generation, Err: err}
		}

		contextName, err := resolveContext(contexts, contextOverride)
		if err != nil {
			return LoadFailed{Generation: generation, Err: err}
		}

		return ContextsLoaded{
			Generation:  generation,
			Contexts:    contexts,
			ContextName: contextName,
		}
	}
}

func resolveContext(contexts []domain.ClusterContext, contextOverride string) (string, error) {
	if contextOverride != "" {
		for _, clusterContext := range contexts {
			if clusterContext.Name == contextOverride {
				return contextOverride, nil
			}
		}
		return "", fmt.Errorf("Talos context %q is not available", contextOverride)
	}

	for _, clusterContext := range contexts {
		if clusterContext.Current {
			return clusterContext.Name, nil
		}
	}

	return "", fmt.Errorf("Talos context catalog has no current context")
}

func openSession(contextName string, generation uint64) Effect {
	return func(ctx context.Context, dependencies Dependencies) Message {
		if dependencies.SessionFactory == nil {
			return LoadFailed{Generation: generation, Err: fmt.Errorf("open session: session factory is not configured")}
		}

		var session ports.Session
		var err error
		if dependencies.openSession != nil {
			session, err = dependencies.openSession(ctx, contextName, generation)
		} else {
			session, err = dependencies.SessionFactory.Open(ctx, contextName)
		}
		if err != nil {
			return LoadFailed{Generation: generation, Err: err}
		}
		if session == nil {
			return LoadFailed{Generation: generation, Err: fmt.Errorf("open session: session factory returned no session")}
		}
		nodes := session.Nodes()
		if nodes == nil {
			return LoadFailed{Generation: generation, Err: fmt.Errorf("load nodes: node reader is not configured")}
		}

		var kubernetesReader ports.KubernetesNodeReader
		if dependencies.KubernetesResolver != nil {
			kubernetesReader, err = dependencies.KubernetesResolver.Resolve(ctx, contextName)
			if err != nil {
				kubernetesReader = nil
			}
		}

		opened := SessionOpened{Generation: generation, Session: session, KubernetesNodes: kubernetesReader}
		if dependencies.sessionRequests != nil {
			opened.requests = dependencies.sessionRequests()
		}
		return opened
	}
}

func loadNodes(reader ports.NodeReader, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return LoadFailed{Generation: generation, Err: fmt.Errorf("load nodes: node reader is not configured")}
		}

		nodes, err := reader.List(ctx)
		if err != nil {
			return LoadFailed{Generation: generation, Err: err}
		}

		return NodesLoaded{Generation: generation, Nodes: nodes}
	}
}

type Runner struct {
	dependencies Dependencies

	mu         sync.Mutex
	requests   *requestOwner
	cancel     context.CancelFunc
	session    ports.Session
	generation uint64
	active     bool
}

func NewRunner(dependencies Dependencies) *Runner {
	return &Runner{dependencies: dependencies}
}

func (r *Runner) Run(ctx context.Context, effect Effect) Message {
	if effect == nil {
		return nil
	}

	dependencies := r.dependencies
	if dependencies.SessionFactory != nil {
		dependencies.openSession = r.replaceSession
		dependencies.sessionRequests = func() *requestOwner {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.requests
		}
	}

	return effect(ctx, dependencies)
}

func (r *Runner) Cancel() {
	_ = r.stopActive()
}

func (r *Runner) Close() error {
	return r.stopActive()
}

func (r *Runner) stopActive() error {
	r.mu.Lock()
	cancel := r.cancel
	session := r.session
	requests := r.requests
	r.requests = nil
	r.cancel = nil
	r.session = nil
	r.generation++
	r.active = false
	r.mu.Unlock()

	if requests != nil {
		requests.close()
	}
	if cancel != nil {
		cancel()
	}
	if session != nil {
		return session.Close()
	}

	return nil
}

func (r *Runner) replaceSession(effectCtx context.Context, contextName string, generation uint64) (ports.Session, error) {
	r.mu.Lock()
	if generation < r.generation || generation == r.generation && r.active {
		r.mu.Unlock()
		return nil, context.Canceled
	}
	oldCancel := r.cancel
	oldSession := r.session
	oldRequests := r.requests
	sessionCtx, cancel := context.WithCancel(effectCtx)
	requests := newRequestOwner(sessionCtx)
	r.requests = requests
	r.cancel = cancel
	r.session = nil
	r.generation = generation
	r.active = true
	r.mu.Unlock()

	if oldRequests != nil {
		oldRequests.close()
	}
	if oldCancel != nil {
		oldCancel()
	}
	if oldSession != nil {
		_ = oldSession.Close()
	}

	session, err := r.dependencies.SessionFactory.Open(sessionCtx, contextName)
	if err != nil {
		if session != nil {
			_ = session.Close()
		}
		r.clearFailedSession(requests, cancel)
		return nil, err
	}
	if session == nil {
		r.clearFailedSession(requests, cancel)
		return nil, fmt.Errorf("open session: session factory returned no session")
	}
	nodes := session.Nodes()
	if nodes == nil {
		_ = session.Close()
		r.clearFailedSession(requests, cancel)
		return nil, fmt.Errorf("load nodes: node reader is not configured")
	}

	r.mu.Lock()
	if requests != r.requests || !r.active || sessionCtx.Err() != nil {
		r.mu.Unlock()
		_ = session.Close()
		r.clearFailedSession(requests, cancel)
		return nil, context.Canceled
	}
	r.session = session
	r.mu.Unlock()

	return session, nil
}

func (r *Runner) clearFailedSession(requests *requestOwner, cancel context.CancelFunc) {
	r.mu.Lock()
	if requests == r.requests {
		r.requests = nil
		r.cancel = nil
		r.session = nil
		r.active = false
	}
	r.mu.Unlock()
	requests.close()
	cancel()
}

func loadServices(reader ports.ServiceReader, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return ServicesLoaded{Generation: generation}
		}
		services, err := reader.List(ctx)
		if err != nil {
			return ServicesFailed{Generation: generation, Err: err}
		}
		return ServicesLoaded{Generation: generation, Services: services}
	}
}

func loadEvents(reader ports.EventReader, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return EventsLoaded{Generation: generation}
		}
		events, err := reader.List(ctx)
		if err != nil {
			return EventsFailed{Generation: generation, Err: err}
		}
		return EventsLoaded{Generation: generation, Events: events}
	}
}

func loadEtcd(reader ports.EtcdReader, controlPlaneNodes []string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return EtcdLoaded{Generation: generation}
		}
		etcd, err := reader.List(ctx, controlPlaneNodes)
		if err != nil {
			return EtcdFailed{Generation: generation, Err: err}
		}
		return EtcdLoaded{Generation: generation, Etcd: etcd}
	}
}

func loadProcesses(reader ports.ProcessReader, node string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return ProcessesLoaded{Generation: generation, Node: node}
		}
		processes, err := reader.List(ctx, node)
		if err != nil {
			return ProcessesFailed{Generation: generation, Node: node, Err: err}
		}
		return ProcessesLoaded{Generation: generation, Node: node, Processes: processes}
	}
}

// defaultEtcdSnapshotPathEffect prefills the snapshot path prompt. The clock
// read lives here, not in the reducer, so the reducer stays pure.
func defaultEtcdSnapshotPathEffect(contextName, memberHostname, node string, generation uint64) Effect {
	return func(context.Context, Dependencies) Message {
		return EtcdSnapshotPromptOpened{
			Generation:     generation,
			Node:           node,
			MemberHostname: memberHostname,
			DefaultPath:    defaultEtcdSnapshotPath(contextName, memberHostname, time.Now().UTC()),
		}
	}
}

func runEtcdSnapshot(operations ports.EtcdOperations, node, path string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if operations == nil {
			return EtcdSnapshotFailed{Generation: generation, Err: fmt.Errorf("etcd operations are not configured")}
		}
		result, err := operations.Snapshot(ctx, node, path)
		if err != nil {
			return EtcdSnapshotFailed{Generation: generation, Err: err}
		}
		return EtcdSnapshotSucceeded{Generation: generation, Result: result}
	}
}

// runEtcdMembership performs the destructive membership RPC. It is only ever
// reached after the mandatory pre-removal snapshot has succeeded (the reducer
// emits it from EtcdSnapshotSucceeded) and never for a blocked action.
func runEtcdMembership(operations ports.EtcdOperations, pending PendingEtcdAction, generation uint64) Effect {
	fail := func(err error) Message {
		return EtcdActionFailed{Generation: generation, MemberHostname: pending.MemberHostname, Err: err}
	}
	if operations == nil {
		return func(context.Context, Dependencies) Message {
			return fail(fmt.Errorf("etcd operations are not configured"))
		}
	}

	return func(ctx context.Context, _ Dependencies) Message {
		var err error
		switch pending.Kind {
		case EtcdActionRemoveMember:
			err = operations.RemoveMemberByID(ctx, pending.Node, pending.MemberID)
		case EtcdActionLeaveCluster:
			err = operations.LeaveCluster(ctx, pending.Node)
		default:
			err = fmt.Errorf("unsupported etcd membership action %q", pending.Kind)
		}
		if err != nil {
			return fail(err)
		}

		return EtcdActionSucceeded{Generation: generation, MemberHostname: pending.MemberHostname}
	}
}

func runEtcdMaintenance(operations ports.EtcdOperations, pending PendingEtcdAction, generation uint64) Effect {
	if operations == nil {
		return func(context.Context, Dependencies) Message {
			return EtcdActionFailed{Generation: generation, MemberHostname: pending.MemberHostname, Err: fmt.Errorf("etcd operations are not configured")}
		}
	}
	return func(ctx context.Context, _ Dependencies) Message {
		var err error
		switch pending.Kind {
		case EtcdActionDefragment:
			err = operations.Defragment(ctx, pending.Node)
		case EtcdActionDisarmAlarms:
			err = operations.DisarmAlarms(ctx, pending.Node)
		case EtcdActionForfeitLeadership:
			err = operations.ForfeitLeadership(ctx, pending.Node)
		default:
			err = fmt.Errorf("unsupported etcd action %q", pending.Kind)
		}
		if err != nil {
			return EtcdActionFailed{Generation: generation, MemberHostname: pending.MemberHostname, Err: err}
		}
		return EtcdActionSucceeded{Generation: generation, MemberHostname: pending.MemberHostname}
	}
}

func requestUpgradeImage(controller ports.NodeController, target string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if controller == nil {
			return UpgradePromptOpened{Target: target, Generation: generation}
		}
		image, err := controller.CurrentInstallImage(ctx, target)
		if err != nil {
			return UpgradePromptOpened{Target: target, Generation: generation}
		}
		return UpgradePromptOpened{Target: target, Image: image, Generation: generation}
	}
}

// loadResetPreview reads each target's disk inventory and returns the overlay
// preview plus the risk assessment for the default options. A read failure or
// missing reader leaves that node's preview Known=false; the overlay still
// opens so the operator sees the unknown scope rather than nothing.
func loadResetPreview(reader ports.DiskReader, nodes []domain.NodeSnapshot, etcd EtcdState, targets []string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		previews := make(map[string]ResetPreview, len(targets))
		for _, target := range targets {
			if reader == nil {
				previews[target] = ResetPreview{Node: target}
				continue
			}
			set, err := reader.List(ctx, target)
			if err != nil {
				previews[target] = ResetPreview{Node: target, Err: err.Error()}
				continue
			}
			previews[target] = BuildResetPreview(target, set)
		}
		// Only the mode-independent quorum block is shown up front; the disk
		// scope depends on the mode the operator picks in the overlay, so the
		// reducer re-derives it authoritatively (and surfaces any refusal) when
		// the operator submits.
		options := ports.ResetOptions{Mode: ports.WipeModeAll, Reboot: true}
		warning, blocked := resetActionRisk(nodes, etcd, targets, options)
		return ResetPromptOpened{
			Generation: generation,
			Targets:    append([]string(nil), targets...),
			Preview:    previews,
			Warning:    warning,
			Blocked:    blocked,
		}
	}
}

func loadDisks(reader ports.DiskReader, node string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return DisksLoaded{Generation: generation, Node: node}
		}
		disks, err := reader.List(ctx, node)
		if err != nil {
			return DisksFailed{Generation: generation, Node: node, Err: err}
		}
		return DisksLoaded{Generation: generation, Node: node, Disks: disks}
	}
}

func loadNetwork(reader ports.NetworkReader, node string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return NetworkLoaded{Generation: generation, Node: node}
		}
		set, err := reader.List(ctx, node)
		if err != nil {
			return NetworkFailed{Generation: generation, Node: node, Err: err}
		}
		return NetworkLoaded{Generation: generation, Node: node, Network: set}
	}
}

func loadResourceKinds(reader ports.ResourceKindReader, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return ResourceKindsLoaded{Generation: generation}
		}
		kinds, err := reader.List(ctx)
		if err != nil {
			return ResourceKindsFailed{Generation: generation, Err: err}
		}
		return ResourceKindsLoaded{Generation: generation, Kinds: kinds}
	}
}

func loadResourceInstances(reader ports.ResourceInstanceReader, node, kind string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return ResourceInstancesLoaded{Generation: generation}
		}
		instances, err := reader.List(ctx, node, kind)
		if err != nil {
			return ResourceInstancesFailed{Generation: generation, Err: err}
		}
		return ResourceInstancesLoaded{Generation: generation, Instances: instances}
	}
}

func loadKubernetesNodes(reader ports.KubernetesNodeReader, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		nodes, err := reader.List(ctx)
		if err != nil {
			return KubernetesNodesFailed{Generation: generation, Err: err}
		}
		return KubernetesNodesLoaded{Generation: generation, Nodes: nodes}
	}
}

func loadResourceInstance(reader ports.ResourceInstanceReader, node, kind, id string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return ResourceInstanceLoaded{Generation: generation}
		}
		instance, err := reader.Get(ctx, node, kind, id)
		if err != nil {
			return ResourceInstanceFailed{Generation: generation, Err: err}
		}
		return ResourceInstanceLoaded{Generation: generation, Instance: instance}
	}
}

func loadNetstat(reader ports.NetstatReader, node string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return NetstatLoaded{Generation: generation}
		}
		set, err := reader.List(ctx, node)
		if err != nil {
			return NetstatFailed{Generation: generation, Node: node, Err: err}
		}
		return NetstatLoaded{Generation: generation, Node: node, Sockets: set}
	}
}

func loadMounts(reader ports.MountReader, node string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return MountsLoaded{Generation: generation, Node: node}
		}
		set, err := reader.List(ctx, node)
		if err != nil {
			return MountsFailed{Generation: generation, Node: node, Err: err}
		}
		return MountsLoaded{Generation: generation, Node: node, Mounts: set}
	}
}

func loadMemory(reader ports.MemoryReader, node string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return MemoryLoaded{Generation: generation, Node: node}
		}
		snapshot, err := reader.List(ctx, node)
		if err != nil {
			return MemoryFailed{Generation: generation, Node: node, Err: err}
		}
		return MemoryLoaded{Generation: generation, Node: node, Memory: snapshot}
	}
}

func openDmesg(reader ports.DmesgReader, request domain.DmesgRequest, generation, streamGeneration uint64, old ports.DmesgStream) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if old != nil {
			_ = old.Close()
		}
		if reader == nil {
			return dmesgOpened{Generation: generation, StreamGeneration: streamGeneration, Err: fmt.Errorf("dmesg reader is not configured")}
		}
		stream, err := reader.Open(ctx, request)
		return dmesgOpened{Generation: generation, StreamGeneration: streamGeneration, Stream: stream, Err: err}
	}
}

func readDmesgBatch(stream ports.DmesgStream, generation, streamGeneration uint64) Effect {
	if stream == nil {
		return nil
	}
	return func(ctx context.Context, _ Dependencies) Message {
		batch, err := stream.Next(ctx)
		return DmesgBatchLoaded{Generation: generation, StreamGeneration: streamGeneration, Batch: batch, Err: err}
	}
}

func openClusterHealth(reader ports.ClusterHealthReader, request domain.ClusterHealthRequest, generation, streamGeneration uint64, old ports.ClusterHealthStream) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if old != nil {
			_ = old.Close()
		}
		if reader == nil {
			return clusterHealthOpened{Generation: generation, StreamGeneration: streamGeneration, Err: fmt.Errorf("cluster health reader is not configured")}
		}
		stream, err := reader.Open(ctx, request)
		return clusterHealthOpened{Generation: generation, StreamGeneration: streamGeneration, Stream: stream, Err: err}
	}
}

func readClusterHealthBatch(stream ports.ClusterHealthStream, generation, streamGeneration uint64) Effect {
	if stream == nil {
		return nil
	}
	return func(ctx context.Context, _ Dependencies) Message {
		progress, err := stream.Next(ctx)
		return ClusterHealthProgressLoaded{Generation: generation, StreamGeneration: streamGeneration, Progress: progress, Err: err}
	}
}

func closeClusterHealth(stream ports.ClusterHealthStream) Effect {
	if stream == nil {
		return nil
	}
	return func(context.Context, Dependencies) Message {
		_ = stream.Close()
		return nil
	}
}

func closeDmesg(stream ports.DmesgStream) Effect {
	if stream == nil {
		return nil
	}
	return func(context.Context, Dependencies) Message {
		_ = stream.Close()
		return nil
	}
}

func openServiceLogs(reader ports.ServiceLogReader, request domain.LogRequest, generation, streamGeneration uint64, old ports.ServiceLogStream) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if old != nil {
			_ = old.Close()
		}
		if reader == nil {
			return serviceLogOpened{Generation: generation, StreamGeneration: streamGeneration, Err: fmt.Errorf("log reader is not configured")}
		}
		stream, err := reader.Open(ctx, request)
		return serviceLogOpened{Generation: generation, StreamGeneration: streamGeneration, Stream: stream, Err: err}
	}
}

func readServiceLogBatch(stream ports.ServiceLogStream, generation, streamGeneration uint64) Effect {
	if stream == nil {
		return nil
	}
	return func(ctx context.Context, _ Dependencies) Message {
		batch, err := stream.Next(ctx)
		return ServiceLogBatchLoaded{Generation: generation, StreamGeneration: streamGeneration, Batch: batch, Err: err}
	}
}

func closeServiceLogs(stream ports.ServiceLogStream) Effect {
	if stream == nil {
		return nil
	}
	return func(context.Context, Dependencies) Message {
		_ = stream.Close()
		return nil
	}
}
