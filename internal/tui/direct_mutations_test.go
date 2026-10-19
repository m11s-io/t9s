package tui

import (
	"context"
	"errors"
	"fmt"
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
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
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
	model := newModel(context.Background(), nil, LaunchOptions{})
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
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
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
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = directMutationSession(t, false, domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "kubelet"}}}, nil)
	model.sessionCtx = context.Background()
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "kubelet"}}}}
	model.services = model.services.normalizeSelection(model.cluster.services.Value.Services, 0)
	model.nodes.marked = map[string]struct{}{"unrelated-node": {}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	// The UI launch flag authorizes the affordance, while the session remains
	// authoritative about write permission.
	model, command := updateDirect(model, keyPress('S'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Nil(t, model.mutation.prompt)
	assert.Equal(t, "writes are disabled for this session", model.view.notice)
	assert.True(t, model.nodes.isMarked("unrelated-node"), "preparation failure preserves unrelated marks")
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
	model := newModel(context.Background(), client, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = session
	model.sessionCtx = context.Background()
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
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
	model := newModel(context.Background(), client, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: nodes}}
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

func TestCompletedMutationKeepsTimeServerValidationVisible(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = directMutationSession(t, true, domain.ServiceSet{}, func(context.Context, string, ports.RebootMode) error { return nil })
	model.sessionCtx = context.Background()
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
	model.nodes = model.nodes.normalizeSelection(model.cluster.nodes.Value.Nodes, 0)

	model, command := updateDirect(model, keyPress('R'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	model, command = updateDirect(model, keyPress('y'))
	for model.mutation.busy {
		model, command = updateDirect(model, command())
	}
	assert.NotEmpty(t, model.mutation.notice, "the completed operation status remains available")

	model, command = updateDirect(model, keyPress('t'))
	require.Equal(t, viewTime, model.views.top().Kind)
	require.NotNil(t, command)
	model, _ = updateDirect(model, keyPress('c'))
	require.NotNil(t, model.timePrompt)
	model.timePrompt.input.SetValue("https://bad.example")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, model.timePrompt)
	require.NotEmpty(t, model.timePrompt.err)
	content := ansi.Strip(model.View().Content)
	assert.Contains(t, content, "TIME SERVER :")
	assert.Contains(t, content, model.timePrompt.err)
}

func TestActiveMutationDoesNotHideNodeOrServiceFilterInput(t *testing.T) {
	cases := []struct {
		name string
		view viewKind
	}{
		{name: "nodes", view: viewNodes},
		{name: "services", view: viewServices},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
			model.splash = false
			model.views = model.views.replaceRoot(viewFrame{Kind: test.view, Label: test.name})
			model.mutation.busy = true
			model.mutation.notice = "Mutation in progress…"
			if test.view == viewNodes {
				model.nodes.filtering = true
				model.nodes.filter = "worker"
			} else {
				model.services.filtering = true
				model.services.filter = "kubelet"
			}

			content := ansi.Strip(model.View().Content)
			query := "/worker"
			if test.view == viewServices {
				query = "/kubelet"
			}
			assert.Contains(t, content, query, "active input must take footer priority")
			assert.True(t, model.mutation.busy)
			assert.Equal(t, "Mutation in progress…", model.mutation.notice, "operation status remains stored independently")
		})
	}
}

func directDestructiveSession(t *testing.T, diskFunc func(context.Context, string) (domain.DiskSet, error), controller *testkit.FakeNodeController, writes bool) *application.Session {
	t.Helper()
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}, {ID: "cp-2-id", Name: "cp-2", Role: domain.NodeRoleControl}, {ID: "worker-id", Name: "worker-1", Role: domain.NodeRoleWorker}, {ID: "worker-2-id", Name: "worker-2", Role: domain.NodeRoleWorker}}}, nil
			}},
			DiskReader: &testkit.FakeDiskReader{ListFunc: diskFunc},
			EtcdReader: &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
				return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true}, {MemberID: 2, Hostname: "cp-2", StatusKnown: true}, {MemberID: 3, Hostname: "cp-3", StatusKnown: true}}}, nil
			}},
			NodeController: controller,
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: writes})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestDirectResetUsesTypedPlanOptionsAndExplicitIntentThenApproval(t *testing.T) {
	var got ports.ResetOptions
	disks := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb"}}}
	session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) { return disks, nil }, &testkit.FakeNodeController{ResetFunc: func(_ context.Context, target string, options ports.ResetOptions) error {
		require.Equal(t, "cp-1", target)
		got = options
		return nil
	}}, true)
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
	model.nodes = model.nodes.normalizeSelection(model.cluster.nodes.Value.Nodes, 0)
	model.nodes.marked = map[string]struct{}{"cp-id": {}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})

	model, command := updateDirect(model, keyPress('W'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	assert.Equal(t, application.ActionReset, model.mutation.prompt.nodeKind)
	assert.Contains(t, ansi.Strip(model.View().Content), "system /dev/sda")
	assert.Contains(t, ansi.Strip(model.View().Content), "user /dev/sdb")
	assert.Equal(t, "cp-1", model.mutation.prompt.assessment.RequiredToken)
	model.mutation.prompt.intent.SetValue("cp-2")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, ansi.Strip(model.View().Content), "typed confirmation does not match")
	assert.False(t, model.mutation.prompt.intentReady)
	assert.True(t, model.nodes.isMarked("cp-id"), "invalid typed token retains marks")
	model.mutation.prompt.intent.SetValue("")
	model, _ = updateDirect(model, keyPress('y'))
	assert.False(t, model.mutation.busy, "a key typed into intent cannot bypass the explicit sequence")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model, _ = updateDirect(model, keyPress(' '))
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model, _ = updateDirect(model, keyPress(' '))
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model, _ = updateDirect(model, keyPress(' '))
	assert.Equal(t, ports.WipeModeSystemDisk, model.mutation.prompt.reset.Mode)
	assert.True(t, model.mutation.prompt.reset.Graceful)
	assert.False(t, model.mutation.prompt.reset.Reboot)
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command, "changed options require a fresh backend assessment")
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	model.mutation.prompt.intent.SetValue("cp-1")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.True(t, model.mutation.prompt.intentReady)
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, ports.WipeModeSystemDisk, got.Mode)
	assert.True(t, got.Graceful)
	assert.False(t, got.Reboot)
	assert.Empty(t, model.nodes.marked, "confirmed node action clears marks")
}

func TestDirectDiskWipeUsesPinnedDeviceAndMethod(t *testing.T) {
	var gotNode, gotDevice string
	var gotMethod ports.DeviceWipeMethod
	disks := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb", Serial: "stable-b"}}}
	session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) { return disks, nil }, &testkit.FakeNodeController{WipeDeviceFunc: func(_ context.Context, node, device string, method ports.DeviceWipeMethod) error {
		gotNode, gotDevice, gotMethod = node, device, method
		return nil
	}}, true)
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.disks = newDirectDisksModel().setResult("cp-1", disks, nil)
	model.disks.filter = "stable-b"
	model.disks = model.disks.normalizeSelection(0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewDisks, Label: "cp-1 > disks", NodeTarget: "cp-1", NodeID: "cp-id"})

	model, command := updateDirect(model, keyPress('W'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	assert.Equal(t, application.ActionWipeDevice, model.mutation.prompt.nodeKind)
	assert.Equal(t, "cp-1", model.mutation.prompt.deviceWipe.Node)
	assert.Equal(t, "/dev/sdb", model.mutation.prompt.deviceWipe.Device)
	assert.Equal(t, "/dev/sdb", model.mutation.prompt.assessment.RequiredToken)
	assert.Equal(t, "", gotNode)
	assert.Equal(t, "", gotDevice)
	assert.Equal(t, ports.DeviceWipeMethod(0), gotMethod)
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model, _ = updateDirect(model, keyPress(' '))
	assert.Equal(t, ports.DeviceWipeZeroes, model.mutation.prompt.deviceWipe.Method)
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	model.mutation.prompt.intent.SetValue("/dev/sdb")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, model.mutation.prompt.intentReady)
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	model, command = applyDirectCommand(t, model, command)
	require.True(t, model.mutation.busy)
	model, duplicate := updateDirect(model, keyPress('y'))
	assert.Nil(t, duplicate, "duplicate approval while execution is accepted has no command")
	for model.mutation.busy {
		model, command = updateDirect(model, command())
	}
	assert.Equal(t, "cp-1", gotNode)
	assert.Equal(t, "/dev/sdb", gotDevice)
	assert.Equal(t, ports.DeviceWipeZeroes, gotMethod)
}

func TestDirectDestructivePreparationBlocksReadOnlyAndUnknownDisksWithoutLeakingErrors(t *testing.T) {
	cases := []struct {
		name     string
		diskFunc func(context.Context, string) (domain.DiskSet, error)
		want     string
	}{
		{name: "read-only", diskFunc: func(context.Context, string) (domain.DiskSet, error) {
			return domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb", ReadOnly: true}}}, nil
		}, want: "read-only"},
		{name: "unknown", diskFunc: func(context.Context, string) (domain.DiskSet, error) {
			return domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb"}}}, errors.New("Bearer secret-disk-error")
		}, want: "inventory"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			session := directDestructiveSession(t, test.diskFunc, &testkit.FakeNodeController{WipeDeviceFunc: func(context.Context, string, string, ports.DeviceWipeMethod) error { calls.Add(1); return nil }}, true)
			model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
			model.splash, model.session, model.sessionCtx = false, session, context.Background()
			listed := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb"}}}
			model.disks = newDirectDisksModel().setResult("cp-1", listed, nil)
			model.disks.control.SetCursor(1)
			model.views = model.views.replaceRoot(viewFrame{Kind: viewDisks, Label: "cp-1 > disks", NodeTarget: "cp-1", NodeID: "cp-id"})
			model, command := updateDirect(model, keyPress('W'))
			require.NotNil(t, command)
			model, _ = applyDirectCommand(t, model, command)
			require.NotNil(t, model.mutation.prompt)
			prompt := ansi.Strip(model.View().Content)
			assert.Contains(t, prompt, "BLOCKED:")
			assert.Contains(t, prompt, test.want)
			assert.NotContains(t, prompt, "secret-disk-error")
			assert.NotContains(t, prompt, "(y/n)")
			model, command = updateDirect(model, keyPress('y'))
			assert.Nil(t, command)
			assert.Zero(t, calls.Load())
		})
	}
}

func TestBlockedResetPromptCancelsFromInitialIntentField(t *testing.T) {
	for _, cancel := range []struct {
		name string
		key  tea.KeyPressMsg
	}{{name: "n", key: keyPress('n')}, {name: "N", key: keyPress('N')}, {name: "escape", key: tea.KeyPressMsg{Code: tea.KeyEscape}}} {
		t.Run(cancel.name, func(t *testing.T) {
			disks := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true, ReadOnly: true}, {DeviceName: "/dev/sdb"}}}
			var resetCalls atomic.Int32
			session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) { return disks, nil }, &testkit.FakeNodeController{ResetFunc: func(context.Context, string, ports.ResetOptions) error {
				resetCalls.Add(1)
				return nil
			}}, true)
			model := directResetPromptModel(t, session, []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}})
			model, command := updateDirect(model, keyPress('W'))
			require.NotNil(t, command)
			model, _ = applyDirectCommand(t, model, command)
			require.NotNil(t, model.mutation.prompt)
			assert.True(t, model.mutation.prompt.intent.Focused())
			require.NotEmpty(t, model.mutation.prompt.assessment.Blocked)
			model, command = updateDirect(model, cancel.key)
			assert.Nil(t, command)
			assert.Nil(t, model.mutation.prompt)
			assert.False(t, model.mutation.busy)
			assert.Zero(t, resetCalls.Load())
		})
	}
}

func TestBlockedDeviceWipePromptCancelsFromInitialIntentField(t *testing.T) {
	for _, cancel := range []struct {
		name string
		key  tea.KeyPressMsg
	}{{name: "n", key: keyPress('n')}, {name: "N", key: keyPress('N')}, {name: "escape", key: tea.KeyPressMsg{Code: tea.KeyEscape}}} {
		t.Run(cancel.name, func(t *testing.T) {
			listed := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb", ReadOnly: true}}}
			var wipeCalls atomic.Int32
			session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) { return listed, nil }, &testkit.FakeNodeController{WipeDeviceFunc: func(context.Context, string, string, ports.DeviceWipeMethod) error {
				wipeCalls.Add(1)
				return nil
			}}, true)
			model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
			model.splash, model.session, model.sessionCtx = false, session, context.Background()
			model.disks = newDirectDisksModel().setResult("cp-1", listed, nil)
			model.disks.control.SetCursor(1)
			model.views = model.views.replaceRoot(viewFrame{Kind: viewDisks, Label: "cp-1 > disks", NodeTarget: "cp-1", NodeID: "cp-id"})
			model, command := updateDirect(model, keyPress('W'))
			require.NotNil(t, command)
			model, _ = applyDirectCommand(t, model, command)
			require.NotNil(t, model.mutation.prompt)
			assert.True(t, model.mutation.prompt.intent.Focused())
			require.NotEmpty(t, model.mutation.prompt.assessment.Blocked)
			model, command = updateDirect(model, cancel.key)
			assert.Nil(t, command)
			assert.Nil(t, model.mutation.prompt)
			assert.False(t, model.mutation.busy)
			assert.Zero(t, wipeCalls.Load())
		})
	}
}

func TestUnblockedDestructiveTokenCanContainN(t *testing.T) {
	prompt := preparedResetPrompt([]string{"worker-1"}, []application.ResetPreview{{Node: "worker-1", Known: true, SystemDisk: "/dev/sda"}})
	prompt.assessment.RequiredToken = "token-n"
	prompt.field = 3
	prompt.intent.SetValue("token-")
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.mutation.prompt = prompt
	model, _ = updateDirect(model, keyPress('n'))
	require.NotNil(t, model.mutation.prompt, "n in an unblocked typed token must not cancel")
	assert.Equal(t, "token-n", model.mutation.prompt.intent.Value())
}

func TestDirectDiskWipeRequiresReprepareWhenPinnedDeviceIdentityChanges(t *testing.T) {
	var reads, calls atomic.Int32
	session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) {
		serial := "serial-a"
		if reads.Add(1) > 1 {
			serial = "serial-b"
		}
		return domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb", Serial: serial}}}, nil
	}, &testkit.FakeNodeController{WipeDeviceFunc: func(context.Context, string, string, ports.DeviceWipeMethod) error { calls.Add(1); return nil }}, true)
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	listed := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb", Serial: "serial-a"}}}
	model.disks = newDirectDisksModel().setResult("cp-1", listed, nil)
	model.disks.control.SetCursor(1)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewDisks, Label: "cp-1 > disks", NodeTarget: "cp-1", NodeID: "cp-id"})
	model, command := updateDirect(model, keyPress('W'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	model.mutation.prompt.intent.SetValue("/dev/sdb")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	model, command = applyDirectCommand(t, model, command)
	for model.mutation.busy {
		model, command = updateDirect(model, command())
	}
	assert.Zero(t, calls.Load(), "a reused device path must not dispatch a wipe")
	assert.Contains(t, model.view.notice, "assessment changed")
}

func TestDirectBulkResetShowsPerTargetProgressWithExactBulkToken(t *testing.T) {
	var calls atomic.Int32
	disks := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb"}}}
	session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) { return disks, nil }, &testkit.FakeNodeController{ResetFunc: func(context.Context, string, ports.ResetOptions) error { calls.Add(1); return nil }}, true)
	workers := []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1", Role: domain.NodeRoleWorker}, {ID: "worker-2-id", Name: "worker-2", Role: domain.NodeRoleWorker}}
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: workers}}
	model.nodes = model.nodes.normalizeSelection(workers, 0)
	model.nodes.marked = map[string]struct{}{"worker-id": {}, "worker-2-id": {}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})
	model, command := updateDirect(model, keyPress('W'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	assert.Equal(t, ports.WipeModeSystemDisk, model.mutation.prompt.reset.Mode, "bulk reset starts with the only mode that supports multiple nodes")
	assert.Equal(t, []string{"worker-1", "worker-2"}, model.mutation.prompt.assessment.Targets)
	assert.Equal(t, "wipe 2 nodes", model.mutation.prompt.assessment.RequiredToken)
	for _, char := range "wipe 2 nodes" {
		model, _ = updateDirect(model, keyPress(char))
	}
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	model, command = applyDirectCommand(t, model, command)
	progress := 0
	for model.mutation.busy {
		message := command()
		model, command = updateDirect(model, message)
		if _, ok := message.(directMutationProgress); ok {
			progress++
		}
	}
	assert.Equal(t, int32(2), calls.Load())
	assert.GreaterOrEqual(t, progress, 1)
	assert.Len(t, model.mutation.results, 2)
	assert.Contains(t, model.mutation.notice, "2/2 accepted; completion not verified")
}

func TestDirectResetHonorsReadOnlySession(t *testing.T) {
	var calls atomic.Int32
	disks := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}}}
	session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) { return disks, nil }, &testkit.FakeNodeController{ResetFunc: func(context.Context, string, ports.ResetOptions) error { calls.Add(1); return nil }}, false)
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
	model.nodes = model.nodes.normalizeSelection(model.cluster.nodes.Value.Nodes, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})
	model, command := updateDirect(model, keyPress('W'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Nil(t, model.mutation.prompt)
	assert.Equal(t, "writes are disabled for this session", model.view.notice)
	assert.Zero(t, calls.Load())
}

func TestDirectResetRequiresReprepareWhenDerivedDiskScopeChanges(t *testing.T) {
	var reads, calls atomic.Int32
	session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) {
		disks := []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb"}}
		if reads.Add(1) > 1 {
			disks = append(disks, domain.DiskSnapshot{DeviceName: "/dev/sdc"})
		}
		return domain.DiskSet{Disks: disks}, nil
	}, &testkit.FakeNodeController{ResetFunc: func(context.Context, string, ports.ResetOptions) error { calls.Add(1); return nil }}, true)
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
	model.nodes = model.nodes.normalizeSelection(model.cluster.nodes.Value.Nodes, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})
	model, command := updateDirect(model, keyPress('W'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	model.mutation.prompt.intent.SetValue("cp-1")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	model, command = applyDirectCommand(t, model, command)
	for model.mutation.busy {
		model, command = updateDirect(model, command())
	}
	assert.Zero(t, calls.Load(), "a newly discovered user disk cannot silently join the prepared wipe scope")
	assert.Contains(t, model.view.notice, "assessment changed")
}

func TestBlockedReadOnlySystemDiskResetCanChangeToUserDisks(t *testing.T) {
	disks := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true, ReadOnly: true}, {DeviceName: "/dev/sdb"}}}
	var resetCalls atomic.Int32
	var got ports.ResetOptions
	session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) { return disks, nil }, &testkit.FakeNodeController{ResetFunc: func(_ context.Context, _ string, options ports.ResetOptions) error {
		resetCalls.Add(1)
		got = options
		return nil
	}}, true)
	model := directResetPromptModel(t, session, []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}})
	model, command := updateDirect(model, keyPress('W'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	require.NotEmpty(t, model.mutation.prompt.assessment.Blocked)
	for range 3 {
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	}
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyRight})
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Equal(t, ports.WipeModeUserDisks, model.mutation.prompt.reset.Mode)
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command, "blocked options must allow fresh backend preparation")
	model, _ = applyDirectCommand(t, model, command)
	require.Empty(t, model.mutation.prompt.assessment.Blocked)
	model.mutation.prompt.intent.SetValue("cp-1")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, int32(1), resetCalls.Load())
	assert.Equal(t, ports.WipeModeUserDisks, got.Mode)
	assert.Equal(t, []string{"/dev/sdb"}, got.UserDisks)
}

func TestBlockedEmptyUserDiskResetCanChangeToSystemDisk(t *testing.T) {
	disks := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}}}
	var resetCalls atomic.Int32
	var got ports.ResetOptions
	session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) { return disks, nil }, &testkit.FakeNodeController{ResetFunc: func(_ context.Context, _ string, options ports.ResetOptions) error {
		resetCalls.Add(1)
		got = options
		return nil
	}}, true)
	model := directResetPromptModel(t, session, []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}})
	model, command := updateDirect(model, keyPress('W'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	for range 3 {
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	}
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyLeft})
	assert.Equal(t, ports.WipeModeUserDisks, model.mutation.prompt.reset.Mode)
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotEmpty(t, model.mutation.prompt.assessment.Blocked)
	for range 3 {
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	}
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyRight})
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Equal(t, ports.WipeModeSystemDisk, model.mutation.prompt.reset.Mode)
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.Empty(t, model.mutation.prompt.assessment.Blocked)
	model.mutation.prompt.intent.SetValue("cp-1")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, int32(1), resetCalls.Load())
	assert.Equal(t, ports.WipeModeSystemDisk, got.Mode)
}

func directResetPromptModel(t *testing.T, session *application.Session, nodes []domain.NodeSnapshot) model {
	t.Helper()
	root := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	root.splash, root.session, root.sessionCtx = false, session, context.Background()
	root.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: nodes}}
	root.nodes = root.nodes.normalizeSelection(nodes, 0)
	root.views = root.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})
	return root
}

func TestResetPromptAt80x24PagesAllTargetsAndKeepsControlsVisible(t *testing.T) {
	previews := make([]application.ResetPreview, 7)
	targets := make([]string, len(previews))
	for index := range previews {
		targets[index] = fmt.Sprintf("worker-%d", index+1)
		previews[index] = application.ResetPreview{Node: targets[index], Known: true, SystemDisk: "/dev/sda", UserDisks: []string{"/dev/sdb"}}
	}
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.width, model.height = false, 80, 24
	model.mutation.prompt = preparedResetPrompt(targets, previews)
	first := ansi.Strip(model.View().Content)
	assert.Contains(t, first, "wipe 7 nodes")
	assert.Contains(t, first, "TYPE :")
	assert.Contains(t, first, "Page 1/")
	assert.Contains(t, first, "worker-1")
	assert.NotContains(t, first, "worker-7")
	model.mutation.prompt.intent.SetValue("wipe 2")
	for range 20 {
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	second := ansi.Strip(model.View().Content)
	assert.Contains(t, second, "Page 3/")
	assert.Contains(t, second, "worker-7")
	assert.Contains(t, second, "wipe 2", "paging must not edit the confirmation token")
	assert.Contains(t, second, "Type \"wipe 7 nodes\"")
	assert.Contains(t, second, "then confirm with y/n")
}

func TestResetPromptAt80x24PagesTwentyDiskRowsWithoutHidingToken(t *testing.T) {
	preview := application.ResetPreview{Node: "worker-1", Known: true, SystemDisk: "/dev/sda"}
	for index := 1; index <= 20; index++ {
		preview.UserDisks = append(preview.UserDisks, fmt.Sprintf("/dev/sd%c", 'a'+rune(index)))
	}
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.width, model.height = false, 80, 24
	model.mutation.prompt = preparedResetPrompt([]string{"worker-1"}, []application.ResetPreview{preview})
	first := ansi.Strip(model.View().Content)
	assert.Contains(t, first, "worker-1")
	assert.Contains(t, first, "TYPE :")
	assert.Contains(t, first, "Page 1/")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyPgDown})
	second := ansi.Strip(model.View().Content)
	assert.Contains(t, second, "Page 2/")
	assert.Contains(t, second, "TYPE :")
	assert.Contains(t, second, "Type \"worker-1\"")
}

func TestResetPromptAt80x20SingleNodeKeepsConfirmationControlsVisible(t *testing.T) {
	preview := application.ResetPreview{Node: "worker-1", Known: true, SystemDisk: "/dev/sda", UserDisks: []string{"/dev/sdb"}}
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.width, model.height = false, 80, 20
	model.mutation.prompt = preparedResetPrompt([]string{"worker-1"}, []application.ResetPreview{preview})
	view := ansi.Strip(model.View().Content)
	assert.Contains(t, view, "Type \"worker-1\"")
	assert.Contains(t, view, "TYPE :")
	assert.Contains(t, view, "then confirm with y/n")
}

func preparedResetPrompt(targets []string, previews []application.ResetPreview) *directMutationPrompt {
	input := destructiveIntentInput()
	return &directMutationPrompt{
		nodeKind:   application.ActionReset,
		reset:      &ports.ResetOptions{Mode: ports.WipeModeAll, Reboot: true},
		intent:     input,
		assessment: application.Assessment{Targets: append([]string(nil), targets...), RequiredToken: application.ResetConfirmationToken(targets), ResetPreview: previews},
	}
}

func TestDirectNodeActionAcceptedOperationSurvivesViewNavigationAndBlocksDuplicateConfirm(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = directMutationSession(t, true, domain.ServiceSet{}, func(ctx context.Context, _ string, _ ports.RebootMode) error {
		close(started)
		<-finish
		return nil
	})
	model.sessionCtx = context.Background()
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
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
	model, _ = updateDirect(model, keyPress('/'))
	model, _ = updateDirect(model, keyPress('c'))
	model, _ = updateDirect(model, keyPress('p'))
	assert.Contains(t, ansi.Strip(model.View().Content), "/cp", "node filter input remains visible during the accepted operation")
	assert.Equal(t, "Mutation in progress…", model.mutation.notice, "operation status remains available independently")
	close(finish)
	model, next := updateDirect(model, <-done)
	require.NotNil(t, next)
	model, _ = applyDirectCommand(t, model, next)
	assert.False(t, model.mutation.busy)
}
