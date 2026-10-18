package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type combinedReadSession struct {
	*testkit.FakeSession
	reader ports.NodeServiceReader
}

func (s combinedReadSession) NodeServices() ports.NodeServiceReader { return s.reader }

type combinedReadFunc func(context.Context) (domain.NodeSet, domain.ServiceSet, error)

func (f combinedReadFunc) List(ctx context.Context) (domain.NodeSet, domain.ServiceSet, error) {
	return f(ctx)
}

func TestClusterReadsAreIndependent(t *testing.T) {
	serviceCalls, eventCalls := 0, 0
	session := openReadTestSession(t, &testkit.FakeSession{
		NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{}, errors.New("node read failed")
		}},
		ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
			serviceCalls++
			return domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "worker", Name: "containerd"}}}, nil
		}},
		EventReader: &testkit.FakeEventReader{ListFunc: func(context.Context) (domain.EventSet, error) {
			eventCalls++
			return domain.EventSet{Events: []domain.EventSnapshot{{Node: "worker", Kind: "info"}}}, nil
		}},
	})

	_, services, err := session.NodeServices(context.Background())
	require.ErrorContains(t, err, "node read failed")
	require.Len(t, services.Services, 1)
	events, err := session.Events(context.Background())
	require.NoError(t, err)
	require.Len(t, events.Events, 1)
	assert.Equal(t, 1, serviceCalls)
	assert.Equal(t, 1, eventCalls)
}

func TestNodeServicesUseUncappedObservation(t *testing.T) {
	const count = 300
	nodes := domain.NodeSet{Nodes: make([]domain.NodeSnapshot, count)}
	services := domain.ServiceSet{Services: make([]domain.ServiceSnapshot, count)}
	for i := 0; i < count; i++ {
		nodes.Nodes[i] = domain.NodeSnapshot{Name: "node"}
		services.Services[i] = domain.ServiceSnapshot{Name: "service"}
	}
	calls := 0
	session := openReadTestSession(t, combinedReadSession{
		FakeSession: &testkit.FakeSession{},
		reader: combinedReadFunc(func(context.Context) (domain.NodeSet, domain.ServiceSet, error) {
			calls++
			return nodes, services, nil
		}),
	})

	gotNodes, gotServices, err := session.NodeServices(context.Background())
	require.NoError(t, err)
	assert.Equal(t, count, len(gotNodes.Nodes))
	assert.Equal(t, count, len(gotServices.Services))
	assert.Equal(t, 1, calls)
}

func TestEtcdDiscoversTargetsWithoutUI(t *testing.T) {
	var gotTargets []string
	set := domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{Hostname: "cp-1", MemberID: 1}}}
	session := openReadTestSession(t, &testkit.FakeSession{
		NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{
				{Name: "cp-1", Role: domain.NodeRoleControl},
				{Name: "worker-1", Role: domain.NodeRoleWorker},
			}}, nil
		}},
		EtcdReader: &testkit.FakeEtcdReader{ListFunc: func(_ context.Context, targets []string) (domain.EtcdSet, error) {
			gotTargets = append([]string(nil), targets...)
			return set, nil
		}},
	})

	got, err := session.Etcd(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"cp-1"}, gotTargets)
	assert.Equal(t, set, got)
}

func TestEtcdWithoutDiscoveredTargetsSettlesWithError(t *testing.T) {
	called := false
	session := openReadTestSession(t, &testkit.FakeSession{
		NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "worker", Role: domain.NodeRoleWorker}}}, nil
		}},
		EtcdReader: &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
			called = true
			return domain.EtcdSet{}, nil
		}},
	})

	_, err := session.Etcd(context.Background())
	require.ErrorContains(t, err, "no control-plane nodes")
	assert.False(t, called)
}

func TestEnrichNodesDoesNotMutateInputs(t *testing.T) {
	original := domain.NodeSnapshot{Name: "cp-1", Addresses: []string{"10.0.0.1"}, Kubernetes: domain.KubernetesUnknown}
	nodes := domain.NodeSet{Nodes: []domain.NodeSnapshot{original}}
	kube := map[string]domain.KubernetesNodeSnapshot{
		"cp-1": {KubeletVersion: "v1.30.1", Conditions: []domain.KubernetesCondition{{Type: "Ready", Status: "True"}}},
	}
	got := application.EnrichNodes(nodes, kube)

	assert.Equal(t, domain.KubernetesUnknown, nodes.Nodes[0].Kubernetes)
	assert.Nil(t, nodes.Nodes[0].KubernetesNode)
	assert.Equal(t, domain.KubernetesReady, got.Nodes[0].Kubernetes)
	assert.Equal(t, "v1.30.1", got.Nodes[0].KubernetesNode.KubeletVersion)
	assert.Nil(t, nodes.Nodes[0].KubernetesNode)
}

func TestClusterHealthRequestPreservesNodeIPSelection(t *testing.T) {
	nodes := []domain.NodeSnapshot{
		{Name: "cp-host", Role: domain.NodeRoleControl, Addresses: []string{"10.0.0.5"}},
		{Name: "cp-no-ip", Role: domain.NodeRoleControl},
		{Name: "10.0.0.6", Role: domain.NodeRoleWorker, Addresses: []string{"bad", "10.0.0.7"}},
		{Name: "worker-host", Role: domain.NodeRoleWorker},
	}

	request := application.ClusterHealthRequestFromNodes(nodes, 2*time.Minute)

	assert.Equal(t, []string{"10.0.0.5"}, request.ControlPlaneNodes)
	assert.Equal(t, []string{"10.0.0.7"}, request.WorkerNodes)
	assert.Equal(t, 2*time.Minute, request.WaitTimeout)
}
