package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/ports"
)

// admitForTest keeps the process-wide mutation lease from leaking into other
// tests, including when an assertion fails before the explicit release.
func admitForTest(t *testing.T, session *Session, plan *preparedState, confirmation Confirmation) (context.Context, func(), error) {
	t.Helper()
	ctx, release, err := session.admit(context.Background(), plan, confirmation, time.Second)
	if release != nil {
		t.Cleanup(release)
	}
	return ctx, release, err
}

func planTestSession(writes bool) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{lifetimeCtx: ctx, lifetimeCancel: cancel, writesEnabled: writes}
}

func planTestNode(session *Session, token string) PreparedNodeAction {
	return newPreparedNodeAction(session, NodeActionRequest{Kind: ActionReboot, Targets: []string{"node-a"}}, Assessment{Targets: []string{"node-a"}, RequiredToken: token})
}

func TestPlanCopySharesOneShotState(t *testing.T) {
	session := planTestSession(true)
	defer session.Close()
	plan := planTestNode(session, "")
	copy := plan

	_, release, err := admitForTest(t, session, plan.state, Confirmation{Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, _, err := admitForTest(t, session, copy.state, Confirmation{Approved: true}); !errors.Is(err, ErrPlanUsed) {
		t.Fatalf("copy replay error = %v, want ErrPlanUsed", err)
	}
}

func TestPlanRejectsWrongSessionAndZeroValue(t *testing.T) {
	first, second := planTestSession(true), planTestSession(true)
	defer first.Close()
	defer second.Close()
	plan := planTestNode(first, "")

	if _, _, err := admitForTest(t, second, plan.state, Confirmation{Approved: true}); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("cross-session error = %v, want ErrInvalidPlan", err)
	}
	if _, _, err := admitForTest(t, first, nil, Confirmation{Approved: true}); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("zero-value error = %v, want ErrInvalidPlan", err)
	}
	if _, _, err := admitForTest(t, first, plan.state, Confirmation{Approved: true}); err != nil {
		t.Fatalf("wrong-session attempt consumed plan: %v", err)
	}
}

func TestInvalidConfirmationDoesNotConsumePlan(t *testing.T) {
	session := planTestSession(true)
	defer session.Close()
	plan := planTestNode(session, "node-a")

	if _, _, err := admitForTest(t, session, plan.state, Confirmation{Approved: true, Token: "wrong"}); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("invalid token error = %v, want ErrConfirmationRequired", err)
	}
	_, release, err := admitForTest(t, session, plan.state, Confirmation{Approved: true, Token: "node-a"})
	if err != nil {
		t.Fatalf("valid confirmation after invalid attempt: %v", err)
	}
	release()
}

func TestBusyDoesNotConsumePlan(t *testing.T) {
	session := planTestSession(true)
	defer session.Close()
	first, second := planTestNode(session, ""), planTestNode(session, "")
	_, release, err := admitForTest(t, session, first.state, Confirmation{Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := admitForTest(t, session, second.state, Confirmation{Approved: true}); !errors.Is(err, ErrMutationBusy) {
		t.Fatalf("concurrent admission error = %v, want ErrMutationBusy", err)
	}
	release()
	_, releaseSecond, err := admitForTest(t, session, second.state, Confirmation{Approved: true})
	if err != nil {
		t.Fatalf("busy attempt consumed plan: %v", err)
	}
	releaseSecond()
}

func TestAdmittedAttemptConsumesBeforeSafetyRead(t *testing.T) {
	session := planTestSession(true)
	defer session.Close()
	plan := planTestNode(session, "")
	_, release, err := admitForTest(t, session, plan.state, Confirmation{Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	// Admission happens before the caller performs any fresh safety read. A
	// failure after this boundary must not make the attempt replayable.
	release()
	if _, _, err := admitForTest(t, session, plan.state, Confirmation{Approved: true}); !errors.Is(err, ErrPlanUsed) {
		t.Fatalf("replay after safety-read attempt error = %v, want ErrPlanUsed", err)
	}
}

func TestPlanAssessmentReturnsDeepCopy(t *testing.T) {
	session := planTestSession(true)
	defer session.Close()
	plan := newPreparedNodeAction(session, NodeActionRequest{Kind: ActionReset, Targets: []string{"node-a"}, Reset: &ports.ResetOptions{UserDisks: []string{"/dev/sdb"}}}, Assessment{
		Targets:      []string{"node-a"},
		ResetPreview: []ResetPreview{{Node: "node-a", UserDisks: []string{"/dev/sdb"}}},
	})

	assessment := plan.Assessment()
	assessment.Targets[0] = "changed"
	assessment.ResetPreview[0].UserDisks[0] = "changed"
	if got := plan.Assessment(); got.Targets[0] != "node-a" || got.ResetPreview[0].UserDisks[0] != "/dev/sdb" {
		t.Fatalf("assessment mutation changed plan: %+v", got)
	}
}

func TestPlanRequestIsCanonicalDeepCopy(t *testing.T) {
	session := planTestSession(true)
	defer session.Close()
	request := NodeActionRequest{Kind: ActionReset, Targets: []string{"node-a"}, Reset: &ports.ResetOptions{UserDisks: []string{"/dev/sdb"}}}
	plan := newPreparedNodeAction(session, request, Assessment{})
	request.Targets[0] = "changed"
	request.Reset.UserDisks[0] = "changed"
	if got := plan.state.nodeRequest; got.Targets[0] != "node-a" || got.Reset.UserDisks[0] != "/dev/sdb" {
		t.Fatalf("request mutation changed canonical plan: %+v", got)
	}
}

func TestAdmissionReleaseIsIdempotentAndUnblocksSession(t *testing.T) {
	session := planTestSession(true)
	defer session.Close()
	plan := planTestNode(session, "")
	_, release, err := admitForTest(t, session, plan.state, Confirmation{Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
	second := planTestNode(session, "")
	_, releaseSecond, err := admitForTest(t, session, second.state, Confirmation{Approved: true})
	if err != nil {
		t.Fatalf("second admission after release: %v", err)
	}
	releaseSecond()
}
