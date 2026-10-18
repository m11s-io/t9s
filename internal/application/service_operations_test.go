package application

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
)

func serviceOperationSession(writes bool, adapter *testkit.FakeSession) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{sdkSession: adapter, lifetimeCtx: ctx, lifetimeCancel: cancel, writesEnabled: writes}
}

func serviceOperationAdapter(controller ports.ServiceController) *testkit.FakeSession {
	return &testkit.FakeSession{
		NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "node-id", Name: "cp-1", Role: domain.NodeRoleControl}}}, nil
		}},
		EtcdReader: &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
			return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
				{MemberID: 1, Hostname: "cp-1", StatusKnown: true},
				{MemberID: 2, Hostname: "cp-2", StatusKnown: true},
				{MemberID: 3, Hostname: "cp-3", StatusKnown: true},
			}}, nil
		}},
		ServiceController: controller,
	}
}

func TestServiceExecutionNeedsBackendPermissionAndConfirmation(t *testing.T) {
	var calls atomic.Int32
	controller := &testkit.FakeServiceController{RestartFunc: func(context.Context, string, string) error { calls.Add(1); return nil }}
	readOnly := serviceOperationSession(false, serviceOperationAdapter(controller))
	defer readOnly.Close()
	if _, err := readOnly.PrepareServiceAction(context.Background(), ServiceActionRequest{Kind: ServiceActionRestart, Node: "node-id", Service: "kubelet"}); !errors.Is(err, ErrWritesDisabled) {
		t.Fatalf("prepare without backend permission = %v, want ErrWritesDisabled", err)
	}

	writeSession := serviceOperationSession(true, serviceOperationAdapter(controller))
	defer writeSession.Close()
	plan, err := writeSession.PrepareServiceAction(context.Background(), ServiceActionRequest{Kind: ServiceActionRestart, Node: "node-id", Service: "kubelet"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeSession.ExecuteServiceAction(context.Background(), plan, Confirmation{}); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("execution without confirmation = %v, want ErrConfirmationRequired", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("controller calls = %d, want 0", calls.Load())
	}
	if _, err := writeSession.ExecuteServiceAction(context.Background(), plan, Confirmation{Approved: true}); err != nil {
		t.Fatalf("confirmed execution after rejected confirmation: %v", err)
	}
}

func TestServicePlanCannotReplay(t *testing.T) {
	var calls atomic.Int32
	controller := &testkit.FakeServiceController{StartFunc: func(context.Context, string, string) error { calls.Add(1); return nil }}
	session := serviceOperationSession(true, serviceOperationAdapter(controller))
	defer session.Close()
	plan, err := session.PrepareServiceAction(context.Background(), ServiceActionRequest{Kind: ServiceActionStart, Node: "cp-1", Service: "kubelet"})
	if err != nil {
		t.Fatal(err)
	}
	confirmation := Confirmation{Approved: true}
	if _, err := session.ExecuteServiceAction(context.Background(), plan, confirmation); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ExecuteServiceAction(context.Background(), plan, confirmation); !errors.Is(err, ErrPlanUsed) {
		t.Fatalf("second execution = %v, want ErrPlanUsed", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("controller calls = %d, want 1", calls.Load())
	}
}

func TestEtcdServiceStopRechecksQuorumBeforeDispatch(t *testing.T) {
	var calls atomic.Int32
	controller := &testkit.FakeServiceController{StopFunc: func(context.Context, string, string) error { calls.Add(1); return nil }}
	adapter := serviceOperationAdapter(controller)
	adapter.EtcdReader = &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
		return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true}}}, nil
	}}
	session := serviceOperationSession(true, adapter)
	defer session.Close()
	plan, err := session.PrepareServiceAction(context.Background(), ServiceActionRequest{Kind: ServiceActionStop, Node: "cp-1", Service: "etcd"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Assessment().Blocked == "" {
		t.Fatal("plan should be blocked because stopping the sole voter loses quorum")
	}
	if _, err := session.ExecuteServiceAction(context.Background(), plan, Confirmation{Approved: true}); !errors.Is(err, ErrActionBlocked) {
		t.Fatalf("execution error = %v, want ErrActionBlocked", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("controller calls = %d, want 0", calls.Load())
	}
}

func TestServiceExecutionRejectsChangedCanonicalNodeIdentity(t *testing.T) {
	var identity atomic.Value
	identity.Store("before")
	controller := &testkit.FakeServiceController{RestartFunc: func(context.Context, string, string) error { return nil }}
	adapter := serviceOperationAdapter(controller)
	adapter.NodeReader = &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: identity.Load().(string), Name: "cp-1", Role: domain.NodeRoleControl}}}, nil
	}}
	session := serviceOperationSession(true, adapter)
	defer session.Close()
	plan, err := session.PrepareServiceAction(context.Background(), ServiceActionRequest{Kind: ServiceActionRestart, Node: "cp-1", Service: "kubelet"})
	if err != nil {
		t.Fatal(err)
	}
	identity.Store("after")
	if _, err := session.ExecuteServiceAction(context.Background(), plan, Confirmation{Approved: true}); !errors.Is(err, ErrAssessmentChanged) {
		t.Fatalf("execution error = %v, want ErrAssessmentChanged", err)
	}
}

func TestConcurrentActionFamiliesShareExclusion(t *testing.T) {
	started, resume := make(chan struct{}), make(chan struct{})
	controller := &testkit.FakeServiceController{RestartFunc: func(ctx context.Context, _, _ string) error {
		close(started)
		select {
		case <-resume:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	session := serviceOperationSession(true, serviceOperationAdapter(controller))
	defer session.Close()
	servicePlan, err := session.PrepareServiceAction(context.Background(), ServiceActionRequest{Kind: ServiceActionRestart, Node: "cp-1", Service: "kubelet"})
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, executeErr := session.ExecuteServiceAction(context.Background(), servicePlan, Confirmation{Approved: true})
		finished <- executeErr
	}()
	<-started
	nodePlan := newPreparedNodeAction(session, NodeActionRequest{Kind: ActionReboot, Targets: []string{"cp-2"}}, Assessment{Targets: []string{"cp-2"}})
	if _, _, err := session.admit(context.Background(), nodePlan.state, Confirmation{Approved: true}, time.Second); !errors.Is(err, ErrMutationBusy) {
		t.Fatalf("node admission during service execution = %v, want ErrMutationBusy", err)
	}
	close(resume)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestServiceCanceledAfterDispatchIsUnconfirmed(t *testing.T) {
	started := make(chan struct{})
	transportErr := errors.New("service transport lost acknowledgement")
	controller := &testkit.FakeServiceController{StopFunc: func(ctx context.Context, _, _ string) error {
		close(started)
		<-ctx.Done()
		return transportErr
	}}
	session := serviceOperationSession(true, serviceOperationAdapter(controller))
	defer session.Close()
	plan, err := session.PrepareServiceAction(context.Background(), ServiceActionRequest{Kind: ServiceActionStop, Node: "cp-1", Service: "kubelet"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct {
		result MutationResult
		err    error
	}, 1)
	go func() {
		result, executeErr := session.ExecuteServiceAction(ctx, plan, Confirmation{Approved: true})
		finished <- struct {
			result MutationResult
			err    error
		}{result, executeErr}
	}()
	<-started
	cancel()
	got := <-finished
	if !errors.Is(got.err, context.Canceled) || !errors.Is(got.err, transportErr) {
		t.Fatalf("execution error = %v, want context.Canceled and the transport error", got.err)
	}
	if len(got.result.Targets) != 1 || got.result.Targets[0].Outcome != domain.OperationUnconfirmed || !errors.Is(got.result.Targets[0].Err, context.Canceled) || !errors.Is(got.result.Targets[0].Err, transportErr) {
		t.Fatalf("target result = %#v, want one unconfirmed target preserving cancellation and transport errors", got.result.Targets)
	}
}

func TestServicePreparationPropagatesCanceledEtcdRead(t *testing.T) {
	controller := &testkit.FakeServiceController{RestartFunc: func(context.Context, string, string) error { return nil }}
	adapter := serviceOperationAdapter(controller)
	etcdReadStarted := make(chan struct{})
	readErr := errors.New("etcd read stopped during cancellation")
	adapter.EtcdReader = &testkit.FakeEtcdReader{ListFunc: func(ctx context.Context, _ []string) (domain.EtcdSet, error) {
		close(etcdReadStarted)
		<-ctx.Done()
		return domain.EtcdSet{}, readErr
	}}
	session := serviceOperationSession(true, adapter)
	defer session.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, prepareErr := session.PrepareServiceAction(ctx, ServiceActionRequest{Kind: ServiceActionRestart, Node: "cp-1", Service: "etcd"})
		finished <- prepareErr
	}()
	<-etcdReadStarted
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) || !errors.Is(err, readErr) {
		t.Fatalf("prepare with canceled etcd read = %v, want cancellation and the read error", err)
	}
}

func TestServiceExecutionDoesNotDispatchAfterCanceledFreshEtcdRead(t *testing.T) {
	var reads atomic.Int32
	started := make(chan struct{})
	var calls atomic.Int32
	controller := &testkit.FakeServiceController{RestartFunc: func(context.Context, string, string) error { calls.Add(1); return nil }}
	adapter := serviceOperationAdapter(controller)
	adapter.EtcdReader = &testkit.FakeEtcdReader{ListFunc: func(ctx context.Context, _ []string) (domain.EtcdSet, error) {
		if reads.Add(1) == 1 {
			return domain.EtcdSet{}, errors.New("membership temporarily unavailable")
		}
		close(started)
		<-ctx.Done()
		return domain.EtcdSet{}, nil
	}}
	session := serviceOperationSession(true, adapter)
	defer session.Close()
	plan, err := session.PrepareServiceAction(context.Background(), ServiceActionRequest{Kind: ServiceActionRestart, Node: "cp-1", Service: "etcd"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, executeErr := session.ExecuteServiceAction(ctx, plan, Confirmation{Approved: true})
		finished <- executeErr
	}()
	<-started
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("execution after canceled fresh etcd read = %v, want context.Canceled", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("controller calls = %d, want 0", calls.Load())
	}
}
