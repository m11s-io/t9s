package application

import (
	"context"
	"fmt"
	"sync"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type Dependencies struct {
	ContextCatalog     ports.ContextCatalog
	SessionFactory     ports.SessionFactory
	KubernetesResolver ports.KubernetesResolver
	openSession        func(context.Context, string, uint64) (ports.Session, error)
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

		return SessionOpened{Generation: generation, Nodes: nodes, NodeController: session.NodeActions(), ServiceController: session.ServiceActions(), Services: session.Services(), Logs: session.ServiceLogs(), Events: session.Events(), Etcd: session.Etcd(), Processes: session.Processes(), Disks: session.Disks(), Network: session.Network(), ResourceKinds: session.ResourceKinds(), Resources: session.Resources(), KubernetesNodes: kubernetesReader}
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
	activeCtx  context.Context
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
	r.activeCtx = nil
	r.cancel = nil
	r.session = nil
	r.generation++
	r.active = false
	r.mu.Unlock()

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
	sessionCtx, cancel := context.WithCancel(effectCtx)
	r.activeCtx = sessionCtx
	r.cancel = cancel
	r.session = nil
	r.generation = generation
	r.active = true
	r.mu.Unlock()

	if oldCancel != nil {
		oldCancel()
	}
	if oldSession != nil {
		_ = oldSession.Close()
	}

	session, err := r.dependencies.SessionFactory.Open(sessionCtx, contextName)
	if err != nil {
		r.clearFailedSession(generation, cancel)
		return nil, err
	}
	if session == nil {
		r.clearFailedSession(generation, cancel)
		return nil, fmt.Errorf("open session: session factory returned no session")
	}
	nodes := session.Nodes()
	if nodes == nil {
		_ = session.Close()
		r.clearFailedSession(generation, cancel)
		return nil, fmt.Errorf("load nodes: node reader is not configured")
	}

	managed := managedSession{Session: session, nodes: nodes, logs: session.ServiceLogs(), events: session.Events(), etcd: session.Etcd(), ctx: sessionCtx}
	r.mu.Lock()
	if generation != r.generation || !r.active {
		r.mu.Unlock()
		_ = session.Close()
		return nil, context.Canceled
	}
	r.session = managed
	r.mu.Unlock()

	return managed, nil
}

func (r *Runner) clearFailedSession(generation uint64, cancel context.CancelFunc) {
	r.mu.Lock()
	if generation == r.generation {
		r.activeCtx = nil
		r.cancel = nil
		r.session = nil
		r.active = false
	}
	r.mu.Unlock()
	cancel()
}

type managedSession struct {
	ports.Session
	nodes  ports.NodeReader
	logs   ports.ServiceLogReader
	events ports.EventReader
	etcd   ports.EtcdReader
	ctx    context.Context
}

func (s managedSession) ServiceLogs() ports.ServiceLogReader {
	if s.logs == nil {
		return nil
	}
	return boundLogReader{ServiceLogReader: s.logs, ctx: s.ctx}
}

func (s managedSession) Nodes() ports.NodeReader {
	if s.nodes == nil {
		return nil
	}

	return boundNodeReader{NodeReader: s.nodes, ctx: s.ctx}
}

func (s managedSession) Events() ports.EventReader {
	if s.events == nil {
		return nil
	}
	return boundEventReader{EventReader: s.events, ctx: s.ctx}
}

func (s managedSession) Etcd() ports.EtcdReader {
	if s.etcd == nil {
		return nil
	}
	return boundEtcdReader{EtcdReader: s.etcd, ctx: s.ctx}
}

type boundEtcdReader struct {
	ports.EtcdReader
	ctx context.Context
}

func (r boundEtcdReader) List(callCtx context.Context, controlPlaneNodes []string) (domain.EtcdSet, error) {
	ctx, cancel := context.WithCancel(r.ctx)
	stop := context.AfterFunc(callCtx, cancel)
	defer func() {
		stop()
		cancel()
	}()

	return r.EtcdReader.List(ctx, controlPlaneNodes)
}

type boundNodeReader struct {
	ports.NodeReader
	ctx context.Context
}

type boundEventReader struct {
	ports.EventReader
	ctx context.Context
}

func (r boundEventReader) List(callCtx context.Context) (domain.EventSet, error) {
	ctx, cancel := context.WithCancel(r.ctx)
	stop := context.AfterFunc(callCtx, cancel)
	defer func() {
		stop()
		cancel()
	}()

	return r.EventReader.List(ctx)
}

func (r boundNodeReader) List(callCtx context.Context) (domain.NodeSet, error) {
	ctx, cancel := context.WithCancel(r.ctx)
	stop := context.AfterFunc(callCtx, cancel)
	defer func() {
		stop()
		cancel()
	}()

	return r.NodeReader.List(ctx)
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
			return ProcessesLoaded{Generation: generation}
		}
		processes, err := reader.List(ctx, node)
		if err != nil {
			return ProcessesFailed{Generation: generation, Err: err}
		}
		return ProcessesLoaded{Generation: generation, Processes: processes}
	}
}

func loadDisks(reader ports.DiskReader, node string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return DisksLoaded{Generation: generation}
		}
		disks, err := reader.List(ctx, node)
		if err != nil {
			return DisksFailed{Generation: generation, Err: err}
		}
		return DisksLoaded{Generation: generation, Disks: disks}
	}
}

func loadNetwork(reader ports.NetworkReader, node string, generation uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		if reader == nil {
			return NetworkLoaded{Generation: generation}
		}
		set, err := reader.List(ctx, node)
		if err != nil {
			return NetworkFailed{Generation: generation, Err: err}
		}
		return NetworkLoaded{Generation: generation, Network: set}
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

type boundLogReader struct {
	ports.ServiceLogReader
	ctx context.Context
}

func (r boundLogReader) Open(callCtx context.Context, request domain.LogRequest) (ports.ServiceLogStream, error) {
	ctx, cancel := context.WithCancel(r.ctx)
	stop := context.AfterFunc(callCtx, cancel)
	stream, err := r.ServiceLogReader.Open(ctx, request)
	if err != nil {
		stop()
		cancel()
		return nil, err
	}
	return &boundLogStream{ServiceLogStream: stream, cancel: cancel, stop: stop}, nil
}

type boundLogStream struct {
	ports.ServiceLogStream
	cancel context.CancelFunc
	stop   func() bool
}

func (s *boundLogStream) Close() error {
	if s.stop != nil {
		s.stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
	return s.ServiceLogStream.Close()
}
