package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
)

func downtimeSession(t *testing.T, members func() ([]domain.EtcdMemberSnapshot, error), snapshot func(context.Context, string, string) (domain.EtcdSnapshotResult, error), upgrade func(context.Context, string, string) ports.UpgradeStream) *Session {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{lifetimeCtx: ctx, lifetimeCancel: cancel, writesEnabled: true, sdkSession: &testkit.FakeSession{
		NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Addresses: []string{"10.0.0.1"}, Role: domain.NodeRoleControl, Version: "v1.14.4"}}}, nil
		}},
		EtcdReader: &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
			m, e := members()
			return domain.EtcdSet{Members: m}, e
		}},
		EtcdOps:        &testkit.FakeEtcdOperations{SnapshotFunc: snapshot},
		NodeController: &testkit.FakeNodeController{UpgradeStreamFunc: upgrade},
	}}
	t.Cleanup(func() { s.Close() })
	return s
}
func healthySingleton() []domain.EtcdMemberSnapshot {
	return []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true}}
}
func downtimeRequest() NodeActionRequest {
	return NodeActionRequest{Kind: ActionUpgrade, Targets: []string{"cp-id"}, Image: "installer:v1.14.5", AllowControlPlaneDowntime: true, SnapshotPath: "/tmp/t9s-upgrade-test.db"}
}
func TestUpgradeDowntimeEligibility(t *testing.T) {
	for _, tc := range []struct {
		name     string
		members  []domain.EtcdMemberSnapshot
		readErr  error
		eligible bool
	}{
		{name: "healthy singleton", members: healthySingleton(), eligible: true},
		{name: "unknown membership", readErr: errors.New("unavailable")},
		{name: "empty membership"},
		{name: "unknown health", members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1"}}},
		{name: "unhealthy", members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true, Errors: []string{"failed"}}}},
		{name: "alarm", members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true, Alarms: []string{"NOSPACE"}}}},
		{name: "wrong member", members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "other", StatusKnown: true}}},
		{name: "learner", members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true, IsLearner: true}}},
		{name: "extra learner", members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true}, {MemberID: 2, Hostname: "cp-2", IsLearner: true}}},
		{name: "multiple voters", members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true}, {MemberID: 2, Hostname: "cp-2", StatusKnown: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := downtimeSession(t, func() ([]domain.EtcdMemberSnapshot, error) { return tc.members, tc.readErr }, nil, nil)
			ordinary := downtimeRequest()
			ordinary.AllowControlPlaneDowntime = false
			ordinary.SnapshotPath = ""
			p, e := s.PrepareNodeAction(context.Background(), ordinary)
			if e != nil {
				t.Fatal(e)
			}
			if p.Assessment().ControlPlaneDowntimeAvailable != tc.eligible {
				t.Fatalf("eligibility=%v want %v", p.Assessment(), tc.eligible)
			}
			if tc.eligible && p.Assessment().Blocked == "" {
				t.Fatal("ordinary upgrade must still refuse singleton quorum loss")
			}
			p, e = s.PrepareNodeAction(context.Background(), downtimeRequest())
			if e != nil {
				t.Fatal(e)
			}
			a := p.Assessment()
			if (a.Blocked == "") != tc.eligible {
				t.Fatalf("opt-in assessment=%+v", a)
			}
			if tc.eligible && (!strings.Contains(a.Warning, "Kubernetes API") || a.SnapshotPath == "" || a.RequiredToken == "") {
				t.Fatalf("missing explicit confirmation scope: %+v", a)
			}
		})
	}
}
func TestUpgradeDowntimeSnapshotBeforeDispatch(t *testing.T) {
	var calls []string
	s := downtimeSession(t, func() ([]domain.EtcdMemberSnapshot, error) {
		calls = append(calls, "read")
		return healthySingleton(), nil
	}, func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
		calls = append(calls, "snapshot")
		return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 128, SHA256: strings.Repeat("a", 64)}, nil
	}, func(_ context.Context, node, image string) ports.UpgradeStream {
		calls = append(calls, "upgrade")
		if node != "cp-1" || image != "installer:v1.14.5" {
			t.Fatalf("dispatch=%s %s", node, image)
		}
		u := newGatedUpgradeStream(ports.UpgradeResult{Done: true, Outcome: ports.UpgradeOutcomeApplied})
		u.finish()
		return u
	})
	p, e := s.PrepareNodeAction(context.Background(), downtimeRequest())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ExecuteUpgrade(context.Background(), p, Confirmation{Approved: true}); !errors.Is(e, ErrConfirmationRequired) {
		t.Fatalf("plain y admitted downtime: %v", e)
	}
	stream, e := s.ExecuteUpgrade(context.Background(), p, Confirmation{Approved: true, Token: p.Assessment().RequiredToken})
	if e != nil {
		t.Fatal(e)
	}
	result, e := stream.Wait(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if result.Snapshot == nil || result.Snapshot.Path != downtimeRequest().SnapshotPath {
		t.Fatalf("missing snapshot receipt: %+v", result)
	}
	if !reflect.DeepEqual(calls, []string{"read", "read", "snapshot", "read", "upgrade"}) {
		t.Fatalf("order=%v", calls)
	}
}
func TestUpgradeDowntimeStopsBeforeDispatch(t *testing.T) {
	for _, scenario := range []string{"snapshot fails", "invalid receipt", "health changes before snapshot", "health changes after snapshot", "member changes after snapshot", "membership changes after snapshot", "canceled snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			reads, snapshots, dispatches := 0, 0, 0
			s := downtimeSession(t, func() ([]domain.EtcdMemberSnapshot, error) {
				reads++
				m := healthySingleton()
				if scenario == "health changes before snapshot" && reads > 1 || scenario == "health changes after snapshot" && reads > 2 {
					m[0].Errors = []string{"failed"}
				}
				if scenario == "member changes after snapshot" && reads > 2 {
					m[0].MemberID = 2
				}
				if scenario == "membership changes after snapshot" && reads > 2 {
					m = append(m, domain.EtcdMemberSnapshot{MemberID: 2, Hostname: "cp-2", StatusKnown: true})
				}
				return m, nil
			}, func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
				snapshots++
				if scenario == "snapshot fails" {
					return domain.EtcdSnapshotResult{}, errors.New("disk full")
				}
				if scenario == "invalid receipt" {
					return domain.EtcdSnapshotResult{}, nil
				}
				if scenario == "canceled snapshot" {
					return domain.EtcdSnapshotResult{}, context.Canceled
				}
				return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 128, SHA256: strings.Repeat("a", 64)}, nil
			}, func(context.Context, string, string) ports.UpgradeStream { dispatches++; return nil })
			p, e := s.PrepareNodeAction(context.Background(), downtimeRequest())
			if e != nil {
				t.Fatal(e)
			}
			_, e = s.ExecuteUpgrade(context.Background(), p, Confirmation{Approved: true, Token: p.Assessment().RequiredToken})
			if e == nil || dispatches != 0 {
				t.Fatalf("error=%v dispatches=%d", e, dispatches)
			}
			if scenario == "health changes before snapshot" && snapshots != 0 {
				t.Fatal("snapshot after health became unsafe")
			}
			if s.mutationBusy {
				t.Fatal("mutation lease leaked")
			}
		})
	}
}
func TestUpgradeDowntimeOptionsAreScoped(t *testing.T) {
	for _, r := range []NodeActionRequest{
		{Kind: ActionReboot, Targets: []string{"cp-1"}, AllowControlPlaneDowntime: true, SnapshotPath: "/tmp/s.db"},
		{Kind: ActionUpgrade, Targets: []string{"cp-1"}, Image: "installer:v1.14.5", AllowControlPlaneDowntime: true},
		{Kind: ActionUpgrade, Targets: []string{"cp-1"}, Image: "installer:v1.14.5", SnapshotPath: "/tmp/s.db"},
	} {
		if e := validateNodeActionRequest(r); e == nil {
			t.Fatalf("accepted invalid options: %+v", r)
		}
	}
}

func TestUpgradeDowntimeRejectsNodeReplacementAfterSnapshot(t *testing.T) {
	replaced := false
	s := downtimeSession(t, func() ([]domain.EtcdMemberSnapshot, error) { return healthySingleton(), nil }, func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
		replaced = true
		return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 128, SHA256: strings.Repeat("a", 64)}, nil
	}, func(context.Context, string, string) ports.UpgradeStream {
		t.Error("replacement node must not receive upgrade")
		return nil
	})
	s.sdkSession.(*testkit.FakeSession).NodeReader = &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		id := "cp-id"
		if replaced {
			id = "replacement"
		}
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: id, Name: "cp-1", Role: domain.NodeRoleControl, Version: "v1.14.4"}}}, nil
	}}
	p, e := s.PrepareNodeAction(context.Background(), downtimeRequest())
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.ExecuteUpgrade(context.Background(), p, Confirmation{Approved: true, Token: p.Assessment().RequiredToken})
	if !errors.Is(e, ErrAssessmentChanged) {
		t.Fatalf("replacement error=%v", e)
	}
}

func TestUpgradeDowntimeRejectsWorkerAndAdditionalControlPlane(t *testing.T) {
	for _, worker := range []bool{true, false} {
		s := downtimeSession(t, func() ([]domain.EtcdMemberSnapshot, error) { return healthySingleton(), nil }, nil, nil)
		s.sdkSession.(*testkit.FakeSession).NodeReader = &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			nodes := []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}
			if worker {
				nodes[0].Role = domain.NodeRoleWorker
			} else {
				nodes = append(nodes, domain.NodeSnapshot{Name: "cp-2", Role: domain.NodeRoleControl})
			}
			return domain.NodeSet{Nodes: nodes}, nil
		}}
		p, e := s.PrepareNodeAction(context.Background(), downtimeRequest())
		if e != nil {
			t.Fatal(e)
		}
		if p.Assessment().Blocked == "" || p.Assessment().ControlPlaneDowntimeAvailable {
			t.Fatalf("unsafe exception: %+v", p.Assessment())
		}
	}
}
