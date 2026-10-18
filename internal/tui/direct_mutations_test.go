package tui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func directMutationSession(t *testing.T, writes bool, services domain.ServiceSet, reboot func(context.Context, string, ports.RebootMode) error) *application.Session {
	t.Helper()
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}, {ID: "worker-id", Name: "worker-1", Role: domain.NodeRoleWorker}}}, nil
			}},
			ServiceReader:     &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) { return services, nil }},
			NodeController:    &testkit.FakeNodeController{RebootFunc: reboot},
			ServiceController: &testkit.FakeServiceController{},
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: writes})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestDirectServiceActionPreparesTypedPlanAndRequiresExplicitConfirm(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = directMutationSession(t, true, domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "kubelet"}}}, nil)
	model.sessionCtx = context.Background()
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "kubelet"}}}}
	model.services = model.services.normalizeSelection(model.cluster.services.Value.Services, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	model, command := updateDirect(model, keyPress('S'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	assert.Equal(t, application.ServiceActionStart, model.mutation.prompt.serviceKind)
	assert.Contains(t, ansi.Strip(model.View().Content), "Start kubelet@cp-1? (y/n)")

	model, _ = updateDirect(model, keyPress('x'))
	assert.NotNil(t, model.mutation.prompt, "unrecognized input cannot be treated as approval or cancellation")
	model, _ = updateDirect(model, keyPress('n'))
	assert.Nil(t, model.mutation.prompt, "explicit n cancels the prompt")
}

func TestDirectMutationPromptDoesNotOpenWithWritesDisabled(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "kubelet"}}}}
	model.services = model.services.normalizeSelection(model.cluster.services.Value.Services, 0)

	model, command := updateDirect(model, keyPress('S'))
	assert.Nil(t, command)
	assert.Nil(t, model.mutation.prompt)
}

func TestDirectServiceDetailMutationUsesPinnedServiceIdentity(t *testing.T) {
	first := domain.ServiceSnapshot{Node: "cp-1", Name: "kubelet"}
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = directMutationSession(t, true, domain.ServiceSet{Services: []domain.ServiceSnapshot{first}}, nil)
	model.sessionCtx = context.Background()
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{first}}}
	model.services = model.services.normalizeSelection(model.cluster.services.Value.Services, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"}).push(viewFrame{Kind: viewServiceDetail, Label: "kubelet@cp-1", ServiceKey: serviceKey(first)})
	model.cluster.services.Value.Services = []domain.ServiceSnapshot{{Node: "worker-1", Name: "machined"}, {Node: "cp-1", Name: "kubelet", State: "running"}}

	model, command := updateDirect(model, keyPress('S'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	assert.Equal(t, "kubelet@cp-1", model.mutation.prompt.label)
	assert.Contains(t, ansi.Strip(model.View().Content), "Start kubelet@cp-1? (y/n)")
}

func TestDirectMutationPreparationErrorsAreNormalized(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = directMutationSession(t, false, domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "kubelet"}}}, nil)
	model.sessionCtx = context.Background()
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "kubelet"}}}}
	model.services = model.services.normalizeSelection(model.cluster.services.Value.Services, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	// The UI launch flag authorizes the affordance, while the session remains
	// authoritative about write permission.
	model, command := updateDirect(model, keyPress('S'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Nil(t, model.mutation.prompt)
	assert.Equal(t, "writes are disabled for this session", model.view.notice)
}

func TestDirectNodeConfirmationRejectsStaleAssessmentWithoutControllerCall(t *testing.T) {
	var reads, calls atomic.Int32
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			if reads.Add(1) == 1 {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}, nil
			}
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-renamed", Role: domain.NodeRoleControl}}}, nil
		}}, NodeController: &testkit.FakeNodeController{RebootFunc: func(context.Context, string, ports.RebootMode) error { calls.Add(1); return nil }}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: true})
	require.NoError(t, err)
	defer session.Close()
	model := newDirectModel(context.Background(), client, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = session
	model.sessionCtx = context.Background()
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
	model.nodes = model.nodes.normalizeSelection(model.cluster.nodes.Value.Nodes, 0)

	model, command := updateDirect(model, keyPress('R'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, int32(0), calls.Load())
	assert.Contains(t, model.view.notice, "assessment changed")
}

func TestDirectBulkMutationKeepsIntermediateAndTerminalResultsInspectable(t *testing.T) {
	nodes := []domain.NodeSnapshot{{ID: "cp-1", Name: "cp-1", Role: domain.NodeRoleControl}, {ID: "cp-2", Name: "cp-2", Role: domain.NodeRoleControl}, {ID: "worker-1", Name: "worker-1", Role: domain.NodeRoleWorker}}
	var calls atomic.Int32
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) { return domain.NodeSet{Nodes: nodes}, nil }}, NodeController: &testkit.FakeNodeController{RebootFunc: func(context.Context, string, ports.RebootMode) error { calls.Add(1); return nil }}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: true})
	require.NoError(t, err)
	defer session.Close()
	model := newDirectModel(context.Background(), client, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: nodes}}
	model.nodes = model.nodes.normalizeSelection(nodes, 0)
	model.nodes.marked = map[string]struct{}{"cp-1": {}, "cp-2": {}, "worker-1": {}}

	model, command := updateDirect(model, keyPress('R'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	progress := 0
	for model.mutation.busy {
		message := command()
		model, command = updateDirect(model, message)
		if _, ok := message.(directMutationProgress); ok {
			progress++
		}
	}
	assert.Equal(t, int32(3), calls.Load())
	assert.GreaterOrEqual(t, progress, 1, "observer outcomes are delivered before the terminal aggregate")
	assert.Len(t, model.mutation.results, 3)
	assert.Contains(t, model.mutation.notice, "3/3 accepted; completion not verified")
	assert.Contains(t, model.mutation.notice, "cp-1 accepted")
	assert.Contains(t, model.mutation.notice, "cp-2 accepted")
	assert.Contains(t, model.mutation.notice, "worker-1 accepted")
}

func TestDirectMutationSummariesDistinguishAcceptedAndUnconfirmedWithoutRawErrors(t *testing.T) {
	accepted := renderMutationResults([]application.TargetResult{{Target: "cp-1", Outcome: domain.OperationAccepted}}, 2)
	assert.Contains(t, accepted, "1/2 accepted; completion not verified")
	unconfirmed := renderMutationResults([]application.TargetResult{{Target: "worker-1", Outcome: domain.OperationUnconfirmed, Err: errors.New("Bearer secret-token")}}, 2)
	assert.Contains(t, unconfirmed, "unconfirmed")
	assert.NotContains(t, unconfirmed, "secret-token")
}

func TestDirectBlockedMutationPromptShowsRiskAndCannotBeConfirmed(t *testing.T) {
	prompt := &directMutationPrompt{node: &application.PreparedNodeAction{}, nodeKind: application.ActionReboot,
		assessment: application.Assessment{Targets: []string{"cp-1"}, Blocked: "would drop etcd below quorum"}}
	assert.Contains(t, renderDirectMutationPrompt(prompt), "would drop etcd below quorum")
	assert.NotContains(t, renderDirectMutationPrompt(prompt), "(y/n)")
}

func TestDirectNodeActionAcceptedOperationSurvivesViewNavigationAndBlocksDuplicateConfirm(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = directMutationSession(t, true, domain.ServiceSet{}, func(ctx context.Context, _ string, _ ports.RebootMode) error {
		close(started)
		<-finish
		return nil
	})
	model.sessionCtx = context.Background()
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
	model.nodes = model.nodes.normalizeSelection(model.cluster.nodes.Value.Nodes, 0)

	model, command := updateDirect(model, keyPress('R'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)

	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyDown})
	assert.Equal(t, viewNodes, model.views.top().Kind)

	done := make(chan tea.Msg, 1)
	go func() { done <- command() }()
	<-started
	model, duplicate := updateDirect(model, keyPress('y'))
	assert.Nil(t, duplicate)
	assert.True(t, model.mutation.busy)
	close(finish)
	model, next := updateDirect(model, <-done)
	require.NotNil(t, next)
	model, _ = applyDirectCommand(t, model, next)
	assert.False(t, model.mutation.busy)
}
