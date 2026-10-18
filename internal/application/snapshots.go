package application

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

// NodeServicesLoaded comes from one observation, so summaries and detailed rows
// describe the same service inventory, including entries beyond table limits.
type NodeServicesLoaded struct {
	Generation, RequestID uint64
	ServicesRequestID     uint64
	Nodes                 domain.NodeSet
	Services              domain.ServiceSet
	Err                   error
}

func (NodeServicesLoaded) applicationMessage() {}

func loadNodeSnapshot(model Model) Effect {
	if model.nodeServices == nil {
		return loadNodes(model.nodeReader, model.Generation)
	}
	reader, generation := model.nodeServices, model.Generation
	return func(ctx context.Context, _ Dependencies) Message {
		nodes, services, err := reader.List(ctx)
		return NodeServicesLoaded{Generation: generation, Nodes: nodes, Services: services, Err: err}
	}
}

func startSessionLoads(model Model) (Model, Effect) {
	var effects []Effect
	var effect Effect
	model, effect = track(model, "nodes", loadNodeSnapshot(model))
	effects = append(effects, effect)
	if model.nodeServices == nil && model.serviceReader != nil {
		model.Services.Status = Loading
		model, effect = track(model, "services", loadServices(model.serviceReader, model.Generation))
		effects = append(effects, effect)
	}
	if model.eventReader != nil {
		model.Events.Status = Loading
		model, effect = track(model, "events", loadEvents(model.eventReader, model.Generation))
		effects = append(effects, effect)
	}
	if model.kubernetesReader != nil {
		model.Kubernetes.Status = Loading
		model, effect = track(model, "kubernetes", loadKubernetesNodes(model.kubernetesReader, model.Generation))
		effects = append(effects, effect)
	}
	return model, batch(effects...)
}

func startEtcdRead(model Model) (Model, Effect) {
	nodes := controlPlaneHostnames(model.Nodes.Value.Nodes)
	if model.etcdReader == nil || len(nodes) == 0 {
		return model, nil
	}
	model.Etcd.Status = Loading
	return track(model, "etcd", loadEtcd(model.etcdReader, nodes, model.Generation))
}
