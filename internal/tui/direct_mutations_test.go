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

func TestCompletedMutationKeepsTimeServerValidationVisible(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = directMutationSession(t, true, domain.ServiceSet{}, func(context.Context, string, ports.RebootMode) error { return nil })
	model.sessionCtx = context.Background()
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
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
			model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
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
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
	model.nodes = model.nodes.normalizeSelection(model.cluster.nodes.Value.Nodes, 0)
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
}

func TestDirectDiskWipeUsesPinnedDeviceAndMethod(t *testing.T) {
	var gotNode, gotDevice string
	var gotMethod ports.DeviceWipeMethod
	disks := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb", Serial: "stable-b"}}}
	session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) { return disks, nil }, &testkit.FakeNodeController{WipeDeviceFunc: func(_ context.Context, node, device string, method ports.DeviceWipeMethod) error {
		gotNode, gotDevice, gotMethod = node, device, method
		return nil
	}}, true)
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.disks = newDisksModel(application.DisksState{Status: application.Ready, Node: "cp-1", Value: disks})
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
			model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
			model.splash, model.session, model.sessionCtx = false, session, context.Background()
			listed := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb"}}}
			model.disks = newDisksModel(application.DisksState{Status: application.Ready, Node: "cp-1", Value: listed})
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

func TestDirectDiskWipeRequiresReprepareWhenPinnedDeviceIdentityChanges(t *testing.T) {
	var reads, calls atomic.Int32
	session := directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) {
		serial := "serial-a"
		if reads.Add(1) > 1 {
			serial = "serial-b"
		}
		return domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb", Serial: serial}}}, nil
	}, &testkit.FakeNodeController{WipeDeviceFunc: func(context.Context, string, string, ports.DeviceWipeMethod) error { calls.Add(1); return nil }}, true)
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	listed := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb", Serial: "serial-a"}}}
	model.disks = newDisksModel(application.DisksState{Status: application.Ready, Node: "cp-1", Value: listed})
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
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: workers}}
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
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
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
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
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
