package application

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

func (s *Session) NodeServices(ctx context.Context) (domain.NodeSet, domain.ServiceSet, error) {
	operationCtx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.NodeSet{}, domain.ServiceSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil {
		return domain.NodeSet{}, domain.ServiceSet{}, ErrCapabilityUnavailable
	}
	if combined, ok := s.sdkSession.(ports.NodeServiceSession); ok {
		if reader := combined.NodeServices(); reader != nil {
			return reader.List(operationCtx)
		}
	}

	var nodes domain.NodeSet
	var services domain.ServiceSet
	var readErrs []error
	if reader := s.sdkSession.Nodes(); reader != nil {
		nodes, err = reader.List(operationCtx)
		if err != nil {
			readErrs = append(readErrs, err)
		}
	} else {
		readErrs = append(readErrs, ErrCapabilityUnavailable)
	}
	if reader := s.sdkSession.Services(); reader != nil {
		services, err = reader.List(operationCtx)
		if err != nil {
			readErrs = append(readErrs, err)
		}
	} else {
		readErrs = append(readErrs, ErrCapabilityUnavailable)
	}
	return nodes, services, errors.Join(readErrs...)
}

func (s *Session) Services(ctx context.Context) (domain.ServiceSet, error) {
	operationCtx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.ServiceSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Services() == nil {
		return domain.ServiceSet{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Services().List(operationCtx)
}

func (s *Session) Events(ctx context.Context) (domain.EventSet, error) {
	operationCtx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.EventSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Events() == nil {
		return domain.EventSet{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Events().List(operationCtx)
}

func (s *Session) Etcd(ctx context.Context) (domain.EtcdSet, error) {
	operationCtx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.EtcdSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil {
		return domain.EtcdSet{}, ErrCapabilityUnavailable
	}
	nodeReader := s.sdkSession.Nodes()
	if nodeReader == nil {
		return domain.EtcdSet{}, ErrCapabilityUnavailable
	}
	etcdReader := s.sdkSession.Etcd()
	if etcdReader == nil {
		return domain.EtcdSet{}, ErrCapabilityUnavailable
	}
	nodes, nodeErr := nodeReader.List(operationCtx)
	targets := controlPlaneHostnames(nodes.Nodes)
	if len(targets) == 0 {
		if nodeErr != nil {
			return domain.EtcdSet{}, nodeErr
		}
		return domain.EtcdSet{}, errors.New("no control-plane nodes available for etcd observation")
	}
	etcd, etcdErr := etcdReader.List(operationCtx, targets)
	return etcd, errors.Join(nodeErr, etcdErr)
}

func (s *Session) KubernetesNodes(ctx context.Context) (map[string]domain.KubernetesNodeSnapshot, error) {
	operationCtx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if s.kubernetesNodes == nil {
		return nil, ErrCapabilityUnavailable
	}
	return s.kubernetesNodes.List(operationCtx)
}

// HealthInput carries both observed values and whether each observation is
// authoritative. An unknown collection is omitted from diagnosis.
type HealthInput struct {
	Nodes      domain.NodeSet
	NodesKnown bool
	Etcd       domain.EtcdSet
	EtcdKnown  bool
}

// EnrichNodes returns copied node snapshots correlated with Kubernetes data.
func EnrichNodes(nodes domain.NodeSet, kubernetesNodes map[string]domain.KubernetesNodeSnapshot) domain.NodeSet {
	enriched := domain.NodeSet{ObservedAt: nodes.ObservedAt, Nodes: make([]domain.NodeSnapshot, len(nodes.Nodes))}
	for index, node := range nodes.Nodes {
		enriched.Nodes[index] = node
		enriched.Nodes[index].Addresses = append([]string(nil), node.Addresses...)
		enriched.Nodes[index].KubernetesNode = cloneKubernetesNode(node.KubernetesNode)
		snapshot, ok := lookupKubernetesCorrelation(node, kubernetesNodes)
		if !ok {
			continue
		}
		enriched.Nodes[index].KubernetesNode = cloneKubernetesNode(&snapshot)
		enriched.Nodes[index].Kubernetes = kubernetesReadiness(snapshot)
	}
	return enriched
}

func cloneKubernetesNode(node *domain.KubernetesNodeSnapshot) *domain.KubernetesNodeSnapshot {
	if node == nil {
		return nil
	}
	copy := *node
	copy.Roles = append([]string(nil), node.Roles...)
	copy.Conditions = append([]domain.KubernetesCondition(nil), node.Conditions...)
	return &copy
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

// ClusterHealthRequestFromNodes builds the ClusterInfo request from the node
// snapshots. Only parseable IP addresses are included because Talos rejects
// hostnames in these fields.
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
