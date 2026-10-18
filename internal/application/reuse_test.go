package application_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
)

func TestPlainGoCallerUsesReadsActionsAndStreamsWithoutTUI(t *testing.T) {
	var serviceCalls int
	stream := &testkit.FakeEventStream{Batches: []domain.EventSet{{Events: []domain.EventSnapshot{{Node: "cp-1", Kind: "updated"}}}}}
	adapter := &testkit.FakeSession{
		ProcessReader: &testkit.FakeProcessReader{ListFunc: func(context.Context, string) (domain.ProcessSet, error) {
			return domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 42, Command: "kubelet"}}}, nil
		}},
		NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "node-id", Name: "cp-1", Role: domain.NodeRoleControl}}}, nil
		}},
		ServiceController: &testkit.FakeServiceController{RestartFunc: func(context.Context, string, string) error {
			serviceCalls++
			return nil
		}},
		EventReader: &testkit.FakeEventReader{OpenFunc: func(context.Context) (ports.EventStream, error) { return stream, nil }},
	}
	client := application.NewClient(application.Dependencies{
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) { return adapter, nil }},
	})

	readOnly, err := client.Open(context.Background(), "prod", application.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readOnly.PrepareServiceAction(context.Background(), application.ServiceActionRequest{Kind: application.ServiceActionRestart, Node: "cp-1", Service: "kubelet"}); !errors.Is(err, application.ErrWritesDisabled) {
		t.Fatalf("read-only service preparation = %v, want ErrWritesDisabled", err)
	}
	if serviceCalls != 0 {
		t.Fatalf("service RPC calls after read-only rejection = %d, want 0", serviceCalls)
	}
	if err := readOnly.Close(); err != nil {
		t.Fatal(err)
	}

	session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	processes, err := session.Processes(context.Background(), "cp-1")
	if err != nil || len(processes.Processes) != 1 || processes.Processes[0].Command != "kubelet" {
		t.Fatalf("diagnostic process read = %#v, %v", processes, err)
	}

	plan, err := session.PrepareServiceAction(context.Background(), application.ServiceActionRequest{Kind: application.ServiceActionRestart, Node: "node-id", Service: "kubelet"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ExecuteServiceAction(context.Background(), plan, application.Confirmation{}); !errors.Is(err, application.ErrConfirmationRequired) {
		t.Fatalf("unconfirmed service execution = %v, want ErrConfirmationRequired", err)
	}
	if serviceCalls != 0 {
		t.Fatalf("service RPC calls before confirmation = %d, want 0", serviceCalls)
	}
	result, err := session.ExecuteServiceAction(context.Background(), plan, application.Confirmation{Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	if serviceCalls != 1 || len(result.Targets) != 1 || result.Targets[0].Outcome != domain.OperationAccepted {
		t.Fatalf("confirmed service execution = %#v, calls %d", result, serviceCalls)
	}

	events, err := session.FollowEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	batch, err := events.Next(context.Background())
	if err != nil || len(batch.Events) != 1 || batch.Events[0].Kind != "updated" {
		t.Fatalf("event stream first batch = %#v, %v", batch, err)
	}
	if _, err := events.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("event stream terminal read = %v, want io.EOF", err)
	}
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
	if !stream.Closed() {
		t.Fatal("normalized stream did not close its port stream")
	}

	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Processes(context.Background(), "cp-1"); !errors.Is(err, application.ErrSessionClosed) {
		t.Fatalf("read after session close = %v, want ErrSessionClosed", err)
	}
}
