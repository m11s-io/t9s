package application

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
)

type gatedUpgradeStream struct {
	results  chan ports.UpgradeResult
	terminal ports.UpgradeResult
	done     chan struct{}
	cancel   chan struct{}
	waits    atomic.Int32
	once     sync.Once
}

func newGatedUpgradeStream(terminal ports.UpgradeResult) *gatedUpgradeStream {
	return &gatedUpgradeStream{results: make(chan ports.UpgradeResult, 1), terminal: terminal, done: make(chan struct{}), cancel: make(chan struct{})}
}
func (s *gatedUpgradeStream) Results() <-chan ports.UpgradeResult { return s.results }
func (s *gatedUpgradeStream) Cancel()                             { s.once.Do(func() { close(s.cancel) }) }
func (s *gatedUpgradeStream) Wait(ctx context.Context) (ports.UpgradeResult, error) {
	s.waits.Add(1)
	select {
	case <-s.done:
		return s.terminal, nil
	case <-ctx.Done():
		return ports.UpgradeResult{}, ctx.Err()
	}
}
func (s *gatedUpgradeStream) finish() { close(s.done) }

func upgradeOperationSession(nodes *testkit.FakeNodeReader, controller ports.NodeController) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{sdkSession: &testkit.FakeSession{NodeReader: nodes, NodeController: controller}, lifetimeCtx: ctx, lifetimeCancel: cancel, writesEnabled: true}
}

func upgradePlan(t *testing.T, session *Session, image string) *PreparedNodeAction {
	t.Helper()
	plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionUpgrade, Targets: []string{"worker-1"}, Image: image})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestUpgradeLeaseLivesThroughCleanup(t *testing.T) {
	terminal := ports.UpgradeResult{Outcome: ports.UpgradeOutcomeApplied, Done: true}
	upstream := newGatedUpgradeStream(terminal)
	nodes := &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "w1", Name: "worker-1", Role: domain.NodeRoleWorker, Version: "v1.14.0"}}}, nil
	}}
	controller := &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream { return upstream }}
	session := upgradeOperationSession(nodes, controller)
	defer session.Close()
	plan := upgradePlan(t, session, "installer:v1.15.0")
	stream, err := session.ExecuteUpgrade(context.Background(), plan, Confirmation{Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.admit(context.Background(), upgradePlan(t, session, "installer:v1.15.0").state, Confirmation{Approved: true}, time.Second); !errors.Is(err, ErrMutationBusy) {
		t.Fatalf("second mutation admission = %v, want ErrMutationBusy", err)
	}
	// The progress stream deliberately remains unread while terminal cleanup is pending.
	upstream.results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeInstalling}}
	select {
	case <-time.After(20 * time.Millisecond):
	case <-upstream.cancel:
		t.Fatal("operation canceled before cleanup completion")
	}
	upstream.finish()
	got, err := stream.Wait(context.Background())
	if err != nil || got.Outcome != ports.UpgradeOutcomeApplied {
		t.Fatalf("Wait() = (%+v, %v)", got, err)
	}
	_, release, err := session.admit(context.Background(), upgradePlan(t, session, "installer:v1.15.0").state, Confirmation{Approved: true}, time.Second)
	if err != nil {
		t.Fatalf("lease not released after upstream completion: %v", err)
	}
	release()
}

func TestUpgradeWaitRetainsRecoveryResultAfterCancellation(t *testing.T) {
	recoveryErr := errors.New("uncordon failed")
	terminal := ports.UpgradeResult{Outcome: ports.UpgradeOutcomeAppliedWithRecoveryWarning, Warning: "uncordon failed", Err: recoveryErr, Done: true}
	upstream := newGatedUpgradeStream(terminal)
	session := upgradeOperationSession(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "worker-1", Role: domain.NodeRoleWorker, Version: "v1.14.0"}}}, nil
	}}, &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream { return upstream }})
	defer session.Close()
	stream, err := session.ExecuteUpgrade(context.Background(), upgradePlan(t, session, "installer:v1.15.0"), Confirmation{Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := stream.Wait(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Wait error = %v", err)
	}
	upstream.finish()
	got, err := stream.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != ports.UpgradeOutcomeAppliedWithRecoveryWarning || !errors.Is(got.Err, recoveryErr) {
		t.Fatalf("recovery terminal = %+v", got)
	}
}

func TestUpgradeWaitIsRepeatableAndConcurrent(t *testing.T) {
	upstream := newGatedUpgradeStream(ports.UpgradeResult{Outcome: ports.UpgradeOutcomeApplied, Done: true})
	session := upgradeOperationSession(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "worker-1", Role: domain.NodeRoleWorker, Version: "v1.14.0"}}}, nil
	}}, &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream { return upstream }})
	defer session.Close()
	stream, err := session.ExecuteUpgrade(context.Background(), upgradePlan(t, session, "installer:v1.15.0"), Confirmation{Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	upstream.finish()
	const callers = 8
	var wg sync.WaitGroup
	wg.Add(callers)
	errs := make(chan error, callers)
	for range callers {
		go func() {
			defer wg.Done()
			result, err := stream.Wait(context.Background())
			if err == nil && result.Outcome != ports.UpgradeOutcomeApplied {
				err = errors.New("wrong terminal outcome")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestUpgradeRejectsStaleVersionWithoutDispatch(t *testing.T) {
	var reads atomic.Int32
	var dispatches atomic.Int32
	nodes := &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		version := "v1.14.0"
		if reads.Add(1) > 1 {
			version = "v1.14.1"
		}
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "w1", Name: "worker-1", Role: domain.NodeRoleWorker, Version: version}}}, nil
	}}
	session := upgradeOperationSession(nodes, &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream {
		dispatches.Add(1)
		return newGatedUpgradeStream(ports.UpgradeResult{})
	}})
	defer session.Close()
	_, err := session.ExecuteUpgrade(context.Background(), upgradePlan(t, session, "installer:v1.15.0"), Confirmation{Approved: true})
	if !errors.Is(err, ErrAssessmentChanged) {
		t.Fatalf("ExecuteUpgrade error = %v, want ErrAssessmentChanged", err)
	}
	if dispatches.Load() != 0 {
		t.Fatalf("upgrade dispatches = %d, want 0", dispatches.Load())
	}
}

func TestUpgradeCanceledCallerWaitDoesNotReleaseLease(t *testing.T) {
	upstream := newGatedUpgradeStream(ports.UpgradeResult{Outcome: ports.UpgradeOutcomeApplied, Done: true})
	session := upgradeOperationSession(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "worker-1", Role: domain.NodeRoleWorker, Version: "v1.14.0"}}}, nil
	}}, &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream { return upstream }})
	defer session.Close()
	stream, err := session.ExecuteUpgrade(context.Background(), upgradePlan(t, session, "installer:v1.15.0"), Confirmation{Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := stream.Wait(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait error = %v", err)
	}
	other := upgradePlan(t, session, "installer:v1.15.0")
	if _, _, err := session.admit(context.Background(), other.state, Confirmation{Approved: true}, time.Second); !errors.Is(err, ErrMutationBusy) {
		t.Fatalf("lease released after local wait ended: %v", err)
	}
	upstream.finish()
	if _, err := stream.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeRequiresPreparedCurrentImageAndPolicy(t *testing.T) {
	var etcdReads atomic.Int32
	var dispatches atomic.Int32
	nodes := &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl, Version: "v1.14.0"}}}, nil
	}}
	adapter := &testkit.FakeSession{
		NodeReader: nodes,
		EtcdReader: &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
			if etcdReads.Add(1) == 1 {
				return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true}, {MemberID: 2, Hostname: "cp-2", StatusKnown: true}, {MemberID: 3, Hostname: "cp-3", StatusKnown: true}}}, nil
			}
			return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true}, {MemberID: 2, Hostname: "cp-2", StatusKnown: true, Errors: []string{"unhealthy"}}, {MemberID: 3, Hostname: "cp-3", StatusKnown: true}}}, nil
		}},
		NodeController: &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream {
			dispatches.Add(1)
			return newGatedUpgradeStream(ports.UpgradeResult{})
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	session := &Session{sdkSession: adapter, lifetimeCtx: ctx, lifetimeCancel: cancel, writesEnabled: true}
	defer session.Close()
	plan, err := session.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionUpgrade, Targets: []string{"cp-id"}, Image: "installer:v1.15.0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ExecuteUpgrade(context.Background(), plan, Confirmation{}); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("unapproved execution = %v", err)
	}
	if dispatches.Load() != 0 {
		t.Fatalf("unapproved upgrade dispatches = %d", dispatches.Load())
	}
	if _, err := session.ExecuteUpgrade(context.Background(), plan, Confirmation{Approved: true}); !errors.Is(err, ErrActionBlocked) {
		t.Fatalf("changed quorum execution = %v, want ErrActionBlocked", err)
	}
	if dispatches.Load() != 0 {
		t.Fatalf("blocked upgrade dispatches = %d", dispatches.Load())
	}
}

func TestUpgradeRejectsWrongKindForeignPlanAndCanceledAdmission(t *testing.T) {
	var dispatches atomic.Int32
	makeSession := func() *Session {
		return upgradeOperationSession(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "w1", Name: "worker-1", Role: domain.NodeRoleWorker, Version: "v1.14.0"}}}, nil
		}}, &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream {
			dispatches.Add(1)
			return newGatedUpgradeStream(ports.UpgradeResult{})
		}})
	}
	first, second := makeSession(), makeSession()
	defer first.Close()
	defer second.Close()
	nodePlan, err := first.PrepareNodeAction(context.Background(), NodeActionRequest{Kind: ActionReboot, Targets: []string{"worker-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.ExecuteUpgrade(context.Background(), nodePlan, Confirmation{Approved: true}); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("wrong-kind plan = %v", err)
	}
	foreign := upgradePlan(t, second, "installer:v1.15.0")
	if _, err := first.ExecuteUpgrade(context.Background(), foreign, Confirmation{Approved: true}); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("foreign plan = %v", err)
	}
	plan := upgradePlan(t, first, "installer:v1.15.0")
	first.writesEnabled = false
	if _, err := first.ExecuteUpgrade(context.Background(), plan, Confirmation{Approved: true}); !errors.Is(err, ErrWritesDisabled) {
		t.Fatalf("writes-disabled execution = %v", err)
	}
	first.writesEnabled = true
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := first.ExecuteUpgrade(canceled, plan, Confirmation{Approved: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled admission = %v", err)
	}
	stream, err := first.ExecuteUpgrade(context.Background(), plan, Confirmation{Approved: true})
	if err != nil {
		t.Fatalf("plan consumed by canceled admission: %v", err)
	}
	if dispatches.Load() != 1 {
		t.Fatalf("upgrade dispatches = %d, want 1", dispatches.Load())
	}
	stream.Cancel()
}

func TestUpgradeSessionCloseKeepsLeaseUntilTerminal(t *testing.T) {
	upstream := newGatedUpgradeStream(ports.UpgradeResult{Err: context.Canceled, Outcome: ports.UpgradeOutcomeAccepted, Done: true})
	controller := &testkit.FakeNodeController{UpgradeStreamFunc: func(ctx context.Context, _, _ string) ports.UpgradeStream {
		go func() { <-ctx.Done(); upstream.Cancel() }()
		return upstream
	}}
	session := upgradeOperationSession(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "worker-1", Role: domain.NodeRoleWorker, Version: "v1.14.0"}}}, nil
	}}, controller)
	stream, err := session.ExecuteUpgrade(context.Background(), upgradePlan(t, session, "installer:v1.15.0"), Confirmation{Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-upstream.cancel:
	case <-time.After(time.Second):
		t.Fatal("session close did not request upgrade cancellation")
	}
	session.mu.Lock()
	busy := session.mutationBusy
	session.mu.Unlock()
	if !busy {
		t.Fatal("session close released mutation lease before upstream terminal")
	}
	upstream.finish()
	result, err := stream.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("terminal error = %v, want context canceled", result.Err)
	}
	session.mu.Lock()
	busy = session.mutationBusy
	session.mu.Unlock()
	if busy {
		t.Fatal("terminal completion left mutation lease held")
	}
}

func TestUpgradePreservesCallerDeadlineWithoutUniversalTimeout(t *testing.T) {
	for _, tc := range []struct {
		name        string
		callerLimit time.Duration
	}{
		{name: "no caller deadline"},
		{name: "caller deadline", callerLimit: 10 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured context.Context
			upstream := newGatedUpgradeStream(ports.UpgradeResult{Outcome: ports.UpgradeOutcomeAccepted, Done: true})
			controller := &testkit.FakeNodeController{UpgradeStreamFunc: func(ctx context.Context, _, _ string) ports.UpgradeStream { captured = ctx; return upstream }}
			session := upgradeOperationSession(&testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "worker-1", Role: domain.NodeRoleWorker, Version: "v1.14.0"}}}, nil
			}}, controller)
			defer session.Close()
			callerCtx := context.Background()
			var cancel context.CancelFunc
			var expected time.Time
			if tc.callerLimit > 0 {
				expected = time.Now().Add(tc.callerLimit)
				callerCtx, cancel = context.WithDeadline(callerCtx, expected)
				defer cancel()
			}
			stream, err := session.ExecuteUpgrade(callerCtx, upgradePlan(t, session, "installer:v1.15.0"), Confirmation{Approved: true})
			if err != nil {
				t.Fatal(err)
			}
			deadline, ok := captured.Deadline()
			if tc.callerLimit == 0 {
				if ok {
					t.Fatalf("adapter context has unexpected deadline %v", deadline)
				}
			} else if !ok || !deadline.Equal(expected) {
				t.Fatalf("adapter deadline = (%v, %v), want caller deadline %v", deadline, ok, expected)
			}
			upstream.finish()
			if _, err := stream.Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
