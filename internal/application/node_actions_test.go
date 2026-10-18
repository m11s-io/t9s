package application

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
)

func nodeOperationSession(adapter *testkit.FakeSession) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{sdkSession: adapter, lifetimeCtx: ctx, lifetimeCancel: cancel, writesEnabled: true}
}

func nodeOperationAdapter(nodes *testkit.FakeNodeReader, etcd *testkit.FakeEtcdReader, controller ports.NodeController) *testkit.FakeSession {
	return &testkit.FakeSession{NodeReader: nodes, EtcdReader: etcd, NodeController: controller}
}

func TestNodeActionsRecheckCurrentQuorum(t *testing.T) {
	var etcdCalls atomic.Int32
	adapter := nodeOperationAdapter(
		&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl}}}, nil
		}},
		&testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
			if etcdCalls.Add(1) == 1 {
				return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
					{MemberID: 1, Hostname: "cp-1", StatusKnown: true},
					{MemberID: 2, Hostname: "cp-2", StatusKnown: true},
					{MemberID: 3, Hostname: "cp-3", StatusKnown: true},
				}}, nil
			}
			return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true}}}, nil
		}},
		&testkit.FakeNodeController{RebootFunc: func(context.Context, string, ports.RebootMode) error {
			t.Fatal("reboot dispatched after refreshed quorum became unsafe")
			return nil
		}},
	)
	session := nodeOperationSession(adapter)
	defer session.Close()
	plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionReboot, Targets: []string{"cp-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Assessment().Blocked != "" {
		t.Fatalf("prepared action unexpectedly blocked: %q", plan.Assessment().Blocked)
	}
	if _, err := session.ExecuteNodeAction(context.Background(), plan, Confirmation{Approved: true}, nil); !errors.Is(err, ErrActionBlocked) {
		t.Fatalf("execute error = %v, want ErrActionBlocked", err)
	}
}

func TestNodeActionCanonicalizesAndDeduplicatesTargets(t *testing.T) {
	adapter := nodeOperationAdapter(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "node-id", Name: "worker-1", Addresses: []string{"10.0.0.1"}, Role: domain.NodeRoleWorker}}}, nil
	}}, nil, &testkit.FakeNodeController{})
	session := nodeOperationSession(adapter)
	defer session.Close()
	plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionReboot, Targets: []string{"node-id", "10.0.0.1", "worker-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Assessment().Targets; !reflect.DeepEqual(got, []string{"worker-1"}) {
		t.Fatalf("canonical deduplicated targets = %v, want [worker-1]", got)
	}
}

func TestNodePreparationRejectsUnknownDiscoveredRoles(t *testing.T) {
	actions := []NodeActionRequest{
		{Kind: ActionReboot, Targets: []string{"worker-unknown"}},
		{Kind: ActionShutdown, Targets: []string{"worker-unknown"}},
		{Kind: ActionRollback, Targets: []string{"worker-unknown"}},
		{Kind: ActionUpgrade, Targets: []string{"worker-unknown"}, Image: "installer:v1.15.0"},
	}
	for _, request := range actions {
		t.Run(string(request.Kind), func(t *testing.T) {
			adapter := nodeOperationAdapter(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				// Mirrors partial discovery fallback: a resolvable node survives an
				// aggregate discovery error with no classified role.
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "unknown-id", Name: "worker-unknown", Role: domain.NodeRoleUnknown, Problem: "role unavailable"}}}, nil
			}}, nil, &testkit.FakeNodeController{})
			session := nodeOperationSession(adapter)
			defer session.Close()
			plan, err := session.PrepareNodeAction(context.Background(), request)
			if err == nil && plan.Assessment().Blocked == "" {
				t.Fatalf("unknown-role target authorized for %s", request.Kind)
			}
		})
	}
}

func TestNodeExecutionRejectsKnownRoleBecomingUnknown(t *testing.T) {
	actions := []ActionKind{ActionReboot, ActionShutdown, ActionRollback}
	for _, kind := range actions {
		t.Run(string(kind), func(t *testing.T) {
			var reads atomic.Int32
			var calls atomic.Int32
			controller := &testkit.FakeNodeController{
				RebootFunc:   func(context.Context, string, ports.RebootMode) error { calls.Add(1); return nil },
				ShutdownFunc: func(context.Context, string, bool) error { calls.Add(1); return nil },
				RollbackFunc: func(context.Context, string) error { calls.Add(1); return nil },
			}
			adapter := nodeOperationAdapter(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				role := domain.NodeRoleWorker
				if reads.Add(1) > 1 {
					role = domain.NodeRoleUnknown
				}
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1", Role: role, Problem: "role unavailable"}}}, nil
			}}, nil, controller)
			session := nodeOperationSession(adapter)
			defer session.Close()
			plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: kind, Targets: []string{"worker-1"}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := session.ExecuteNodeAction(context.Background(), plan, Confirmation{Approved: true}, nil); !errors.Is(err, ErrAssessmentChanged) {
				t.Fatalf("execute error = %v, want ErrAssessmentChanged", err)
			}
			if calls.Load() != 0 {
				t.Fatalf("controller calls = %d, want 0 after role becomes unknown", calls.Load())
			}
		})
	}
}

func TestNodeExecutionRejectsChangedCanonicalIdentity(t *testing.T) {
	var nodeID atomic.Value
	nodeID.Store("before")
	adapter := nodeOperationAdapter(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: nodeID.Load().(string), Name: "worker-1", Role: domain.NodeRoleWorker}}}, nil
	}}, nil, &testkit.FakeNodeController{})
	session := nodeOperationSession(adapter)
	defer session.Close()
	plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionReboot, Targets: []string{"worker-1"}})
	if err != nil {
		t.Fatal(err)
	}
	nodeID.Store("after")
	if _, err := session.ExecuteNodeAction(context.Background(), plan, Confirmation{Approved: true}, nil); !errors.Is(err, ErrAssessmentChanged) {
		t.Fatalf("execute error = %v, want ErrAssessmentChanged", err)
	}
}

func TestBulkActionHoldsLeaseUntilAllTargetsFinish(t *testing.T) {
	started := make(chan struct{})
	resume := make(chan struct{})
	var once sync.Once
	controller := &testkit.FakeNodeController{RebootFunc: func(ctx context.Context, target string, _ ports.RebootMode) error {
		if target == "cp-1" {
			once.Do(func() { close(started) })
			select {
			case <-resume:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}}
	adapter := nodeOperationAdapter(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "cp-1", Role: domain.NodeRoleWorker}, {Name: "cp-2", Role: domain.NodeRoleWorker}}}, nil
	}}, nil, controller)
	session := nodeOperationSession(adapter)
	defer session.Close()
	plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionReboot, Targets: []string{"cp-1", "cp-2"}})
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, executeErr := session.ExecuteNodeAction(context.Background(), plan, Confirmation{Approved: true}, nil)
		finished <- executeErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("bulk controller did not start")
	}
	other := newPreparedNodeAction(session, NodeActionRequest{Kind: ActionShutdown, Targets: []string{"cp-2"}}, Assessment{Targets: []string{"cp-2"}})
	if _, _, err := session.admit(context.Background(), other.state, Confirmation{Approved: true}, time.Second); !errors.Is(err, ErrMutationBusy) {
		t.Fatalf("admission while bulk target is active = %v, want ErrMutationBusy", err)
	}
	close(resume)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("bulk action did not finish after targets resumed")
	}
}

func TestBulkNodeActionPreservesResultsWithoutObserver(t *testing.T) {
	transportErr := errors.New("controller lost acknowledgement")
	controller := &testkit.FakeNodeController{RebootFunc: func(_ context.Context, target string, _ ports.RebootMode) error {
		if target == "cp-2" {
			return transportErr
		}
		return nil
	}}
	adapter := nodeOperationAdapter(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "cp-1", Role: domain.NodeRoleWorker}, {Name: "cp-2", Role: domain.NodeRoleWorker}}}, nil
	}}, nil, controller)
	session := nodeOperationSession(adapter)
	defer session.Close()
	plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionReboot, Targets: []string{"cp-1", "cp-2"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.ExecuteNodeAction(context.Background(), plan, Confirmation{Approved: true}, nil)
	if !errors.Is(err, transportErr) {
		t.Fatalf("execute error = %v, want wrapped transport error", err)
	}
	if len(result.Targets) != 2 {
		t.Fatalf("got %d target results, want 2", len(result.Targets))
	}
	byTarget := map[string]TargetResult{}
	for _, target := range result.Targets {
		byTarget[target.Target] = target
	}
	if got := byTarget["cp-1"].Outcome; got != domain.OperationAccepted {
		t.Fatalf("cp-1 outcome = %v, want accepted", got)
	}
	if got := byTarget["cp-2"].Outcome; got != domain.OperationUnconfirmed || !errors.Is(byTarget["cp-2"].Err, transportErr) {
		t.Fatalf("cp-2 result = %+v, want unconfirmed with inspectable transport error", byTarget["cp-2"])
	}
}

func TestNodePreparationPropagatesCanceledOptionalEtcdRead(t *testing.T) {
	readErr := errors.New("etcd read failed")
	controller := &testkit.FakeNodeController{}
	adapter := nodeOperationAdapter(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "cp-1", Role: domain.NodeRoleControl}}}, nil
	}}, &testkit.FakeEtcdReader{ListFunc: func(ctx context.Context, _ []string) (domain.EtcdSet, error) {
		return domain.EtcdSet{}, readErr
	}}, controller)
	session := nodeOperationSession(adapter)
	defer session.Close()
	ctx, cancel := context.WithCancel(context.Background())
	// The reader cancels the caller context while returning an ordinary error.
	adapter.EtcdReader = &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
		cancel()
		return domain.EtcdSet{}, readErr
	}}
	_, err := session.PrepareNodeAction(ctx, NodeActionRequest{Kind: ActionReboot, Targets: []string{"cp-1"}})
	if !errors.Is(err, context.Canceled) || !errors.Is(err, readErr) {
		t.Fatalf("prepare error = %v, want cancellation and backend read error", err)
	}
}

func TestNodeCanceledAfterDispatchPreservesControllerError(t *testing.T) {
	transportErr := errors.New("node controller lost acknowledgement")
	started := make(chan struct{})
	controller := &testkit.FakeNodeController{RebootFunc: func(ctx context.Context, _ string, _ ports.RebootMode) error {
		close(started)
		<-ctx.Done()
		return transportErr
	}}
	adapter := nodeOperationAdapter(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "worker-1", Role: domain.NodeRoleWorker}}}, nil
	}}, nil, controller)
	session := nodeOperationSession(adapter)
	defer session.Close()
	plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionReboot, Targets: []string{"worker-1"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct {
		result MutationResult
		err    error
	}, 1)
	go func() {
		result, executeErr := session.ExecuteNodeAction(ctx, plan, Confirmation{Approved: true}, nil)
		finished <- struct {
			result MutationResult
			err    error
		}{result, executeErr}
	}()
	select {
	case <-started:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("node controller did not start")
	}
	got := <-finished
	if !errors.Is(got.err, context.Canceled) || !errors.Is(got.err, transportErr) {
		t.Fatalf("execute error = %v, want cancellation and controller error", got.err)
	}
	if len(got.result.Targets) != 1 || got.result.Targets[0].Outcome != domain.OperationUnconfirmed || !errors.Is(got.result.Targets[0].Err, transportErr) {
		t.Fatalf("target result = %+v, want unconfirmed with controller error", got.result.Targets)
	}
}

func TestBulkNodeObserverCallsAreSerializedOncePerTarget(t *testing.T) {
	controller := &testkit.FakeNodeController{RebootFunc: func(context.Context, string, ports.RebootMode) error { return nil }}
	adapter := nodeOperationAdapter(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "cp-1", Role: domain.NodeRoleWorker}, {Name: "cp-2", Role: domain.NodeRoleWorker}, {Name: "cp-3", Role: domain.NodeRoleWorker}}}, nil
	}}, nil, controller)
	session := nodeOperationSession(adapter)
	defer session.Close()
	plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionReboot, Targets: []string{"cp-1", "cp-2", "cp-3"}})
	if err != nil {
		t.Fatal(err)
	}
	var active atomic.Int32
	var calls atomic.Int32
	var mu sync.Mutex
	seen := make(map[string]int)
	result, err := session.ExecuteNodeAction(context.Background(), plan, Confirmation{Approved: true}, func(target TargetResult) {
		if active.Add(1) != 1 {
			t.Error("observer calls overlapped")
		}
		defer active.Add(-1)
		mu.Lock()
		seen[target.Target]++
		mu.Unlock()
		calls.Add(1)
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || len(result.Targets) != 3 {
		t.Fatalf("observer calls/results = %d/%d, want 3/3", calls.Load(), len(result.Targets))
	}
	for _, target := range []string{"cp-1", "cp-2", "cp-3"} {
		if seen[target] != 1 {
			t.Errorf("observer calls for %s = %d, want 1", target, seen[target])
		}
	}
}

func TestUpgradePreparationRequiresSingleTargetAndImage(t *testing.T) {
	adapter := &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl, Version: "v1.13.3"}, {ID: "n2", Name: "cp-2", Role: domain.NodeRoleWorker}}}, nil
	}}, NodeController: &testkit.FakeNodeController{}}
	session := nodeOperationSession(adapter)
	defer session.Close()
	for _, request := range []NodeActionRequest{
		{Kind: ActionUpgrade, Targets: []string{"cp-1", "cp-2"}, Image: "installer:v1.15.0"},
		{Kind: ActionUpgrade, Targets: []string{"cp-1"}},
	} {
		if _, err := session.PrepareNodeAction(context.Background(), request); err == nil {
			t.Fatalf("PrepareNodeAction(%+v) succeeded, want validation error", request)
		}
	}
	plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionUpgrade, Targets: []string{"n1"}, Image: "installer:v1.15.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Assessment().Targets, []string{"cp-1"}) {
		t.Fatalf("canonical targets = %v, want [cp-1]", plan.Assessment().Targets)
	}
	if plan.Assessment().Warning == "" {
		t.Fatal("upgrade preparation should include existing minor-skip warning")
	}
	if _, err := session.ExecuteNodeAction(context.Background(), plan, Confirmation{Approved: true}, nil); err == nil {
		t.Fatal("ExecuteNodeAction dispatched/prepared an upgrade, want explicit rejection")
	}
}

func TestWorkerUpgradePreparationKeepsMinorSkipWarning(t *testing.T) {
	adapter := &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1", Role: domain.NodeRoleWorker, Version: "v1.13.3"}}}, nil
	}}, NodeController: &testkit.FakeNodeController{}}
	session := nodeOperationSession(adapter)
	defer session.Close()
	plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionUpgrade, Targets: []string{"worker-1"}, Image: "installer:v1.15.0"})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Assessment().Warning; got != "skips intermediate Talos minor releases" {
		t.Fatalf("worker upgrade warning = %q, want minor-skip warning", got)
	}
}
