package tui

import (
	"context"
	"sync/atomic"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectRootInitStartsSplashAndRefreshHeartbeat(t *testing.T) {
	root := newDirectModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
	command := root.Init()
	require.NotNil(t, command)
	batch, ok := command().(tea.BatchMsg)
	require.True(t, ok)
	require.Len(t, batch, 3, "startup includes context discovery, splash timer, and node refresh heartbeat")
	assert.Contains(t, root.View().Content, "Inspect Talos clusters")

	root, _ = updateDirect(root, splashDoneMsg{})
	assert.NotContains(t, root.View().Content, "Inspect Talos clusters")
}

func TestDirectContextSwitchClearsPendingMutationPrompt(t *testing.T) {
	root := newDirectModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
	root.mutation.prompt = &directMutationPrompt{nodeKind: application.ActionReboot}
	root.nodes.marked = map[string]struct{}{"node-1": {}}
	command := root.beginSessionOpen("dev")
	assert.NotNil(t, command)
	assert.Nil(t, root.mutation.prompt)
	assert.Empty(t, root.nodes.marked, "context switch clears marks to prevent cross-context retargeting")
}

func TestDirectContextPickerSelectionClearsMarksAndContextScopedNodeState(t *testing.T) {
	root := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1", Role: domain.NodeRoleWorker}})
	root.cluster.contextName = "prod"
	root.cluster.contexts = []domain.ClusterContext{{Name: "dev"}, {Name: "prod", Current: true}}
	root.nodes.filter = "worker"
	root.nodes.selectedID = "worker-id"
	root.nodes.marked = map[string]struct{}{"worker-id": {}}
	root.contexts = newContextsModel(root.cluster.contexts, "prod")

	root, _ = updateDirect(root, tea.KeyPressMsg{Code: tea.KeyUp})
	assert.Equal(t, "dev", root.contexts.values[root.contexts.control.Cursor()].Name)
	root, command := updateDirect(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	assert.Equal(t, "dev", root.cluster.contextName)
	assert.Empty(t, root.nodes.marked, "confirmed context selection clears cross-context action targets")
	assert.Empty(t, root.nodes.filter, "context switch clears the prior context's filter")
	assert.Empty(t, root.nodes.selectedID, "context switch clears the prior context's selected identity")
}

func TestDirectActionHintsFollowWritePermission(t *testing.T) {
	readOnly := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker"}})
	assert.NotContains(t, ansi.Strip(readOnly.View().Content), "<R> Reboot")

	writable := newDirectNodesModel(t, LaunchOptions{WritesEnabled: true}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker"}})
	content := ansi.Strip(writable.View().Content)
	assert.Contains(t, content, "<R> Reboot")
	assert.Contains(t, content, "<W> Reset")
}

func TestDirectRootSkipsSplashOnKeypress(t *testing.T) {
	root := newDirectModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
	root, _ = updateDirect(root, keyPress('x'))
	assert.NotContains(t, root.View().Content, "Inspect Talos clusters")
}

func TestDirectRootUsesOnlyActiveViewFilterForPalettePrecedence(t *testing.T) {
	root := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}, {ID: "worker-id", Name: "worker-1", Role: domain.NodeRoleWorker}})
	root.network.filtering = true // A retained filter in an inactive feature must not block global commands.
	root, _ = updateDirect(root, keyPress(':'))
	assert.True(t, root.palette.active)

	root.palette = root.palette.close()
	root.nodes.filtering = true
	for _, r := range "qrvC:" {
		root, _ = updateDirect(root, keyPress(r))
	}
	assert.False(t, root.palette.active)
	assert.Equal(t, "qrvC:", root.nodes.filter, "active filters treat command and action keys as query text")
}

func TestDirectCommandPaletteRendersPromptAndTypedTextInFooter(t *testing.T) {
	root := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker-1"}})
	const typed = "zxq-palette-probe-8472"
	assert.NotContains(t, ansi.Strip(root.View().Content), typed, "the unique query is absent from the underlying view")
	root, _ = updateDirect(root, keyPress(':'))
	for _, key := range typed {
		root, _ = updateDirect(root, keyPress(key))
	}
	content := ansi.Strip(root.View().Content)
	assert.Contains(t, content, "COMMAND :")
	assert.Contains(t, content, typed)
}

func TestDirectMutationPreparedClosesPaletteOpenedDuringPreparation(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = directMutationSession(t, true, domain.ServiceSet{}, nil)
	model.sessionCtx = context.Background()
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}}
	model.nodes = model.nodes.normalizeSelection(model.cluster.nodes.Value.Nodes, 0)

	model, prepare := updateDirect(model, keyPress('R'))
	require.NotNil(t, prepare)
	model, _ = updateDirect(model, keyPress(':'))
	require.True(t, model.palette.active, "palette opens while asynchronous preparation is pending")
	prepared := prepare()
	result, ok := prepared.(directNodePrepared)
	require.True(t, ok)
	require.NoError(t, result.err)
	require.NotNil(t, result.plan)
	assert.Equal(t, model.mutation.operation, result.operation)
	assert.Equal(t, model.generation(), result.generation)
	model, _ = updateDirect(model, prepared)

	content := ansi.Strip(model.View().Content)
	assert.False(t, model.palette.active, "a prepared action must dismiss a palette that would take precedence over its confirmation")
	require.NotNil(t, model.mutation.prompt)
	assert.Contains(t, content, "Reboot cp-1?")
	assert.NotContains(t, content, "COMMAND :")
}

func TestDirectRootHandlesKittyShiftedKeys(t *testing.T) {
	root := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker"}})
	root, _ = updateDirect(root, tea.KeyPressMsg{Code: ';', Text: ":", Mod: tea.ModShift})
	assert.True(t, root.palette.active, "Kitty shifted punctuation must open the command palette")

	root.palette = root.palette.close()
	root.options.WritesEnabled = true
	root.session = directMutationSession(t, true, domain.ServiceSet{}, func(context.Context, string, ports.RebootMode) error { return nil })
	root.sessionCtx = context.Background()
	root.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}, {ID: "worker-id", Name: "worker-1", Role: domain.NodeRoleWorker}}}}
	root.nodes = root.nodes.normalizeSelection(root.cluster.nodes.Value.Nodes, 0)
	kittyR := tea.KeyPressMsg{Code: 'r', Text: "R", Mod: tea.ModShift}
	assert.Equal(t, "R", kittyR.String())
	root, command := updateDirect(root, kittyR)
	assert.NotNil(t, command, "Kitty shift+r must route to reboot preparation")
	root, _ = applyDirectCommand(t, root, command)
	assert.NotNil(t, root.mutation.prompt, "shift+r must open the reboot confirmation prompt")
	if root.mutation.prompt != nil {
		assert.Equal(t, application.ActionReboot, root.mutation.prompt.nodeKind)
	}
	root, _ = updateDirect(root, keyPress('n'))
	root, _ = updateDirect(root, keyPress('/'))
	root, _ = updateDirect(root, kittyR)
	assert.Nil(t, root.mutation.prompt, "shift+r typed into an active filter must remain text")
	assert.Equal(t, "R", root.nodes.filter)
}

func TestDirectRootHandlesKittyShiftedLifecycleActions(t *testing.T) {
	for _, test := range []struct {
		key  rune
		kind application.ActionKind
	}{{'R', application.ActionReboot}, {'X', application.ActionShutdown}, {'B', application.ActionRollback}} {
		t.Run(string(test.key), func(t *testing.T) {
			root := newDirectNodesModel(t, LaunchOptions{WritesEnabled: true}, []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}})
			client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
				return &testkit.FakeSession{
					NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
						return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}, nil
					}},
					NodeController: &testkit.FakeNodeController{
						RebootFunc:   func(context.Context, string, ports.RebootMode) error { return nil },
						ShutdownFunc: func(context.Context, string, bool) error { return nil },
						RollbackFunc: func(context.Context, string) error { return nil },
					},
				}, nil
			}}})
			root.session, root.sessionCtx = func() (*application.Session, context.Context) {
				session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: true})
				require.NoError(t, err)
				t.Cleanup(func() { _ = session.Close() })
				return session, context.Background()
			}()
			kitty := tea.KeyPressMsg{Code: unicode.ToLower(test.key), Text: string(test.key), Mod: tea.ModShift}
			root, command := updateDirect(root, kitty)
			require.NotNil(t, command)
			root, _ = applyDirectCommand(t, root, command)
			require.NotNil(t, root.mutation.prompt)
			assert.Equal(t, test.kind, root.mutation.prompt.nodeKind)
		})
	}
}

func TestDirectEtcdLeaveHandlesKittyShiftEncoding(t *testing.T) {
	operations := &testkit.FakeEtcdOperations{}
	model := directEtcdModel(directEtcdMutationSessionWithTargetHealth(t, true, operations, true), true)
	kittyL := tea.KeyPressMsg{Code: 'l', Text: "L", Mod: tea.ModShift}
	model, command := updateDirect(model, kittyL)
	assert.Nil(t, command)
	require.NotNil(t, model.etcdPathPrompt)
	assert.Equal(t, application.EtcdActionLeaveCluster, model.etcdPathPrompt.kind)
}

func TestDirectServiceStopRequiresExplicitConfirmationAndCallsController(t *testing.T) {
	var stopped bool
	service := domain.ServiceSnapshot{Node: "cp-1", Name: "etcd"}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}, nil
			}},
			ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
				return domain.ServiceSet{Services: []domain.ServiceSnapshot{service}}, nil
			}},
			ServiceController: &testkit.FakeServiceController{StopFunc: func(context.Context, string, string) error {
				stopped = true
				return nil
			}},
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	model := newDirectModel(context.Background(), client, LaunchOptions{WritesEnabled: true})
	model.session, model.sessionCtx = session, context.Background()
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{service}}}
	model.services = model.services.normalizeSelection(model.cluster.services.Value.Services, 0)
	model.nodes.marked = map[string]struct{}{"unrelated-node": {}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})

	model, command := updateDirect(model, keyPress('T'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	assert.Equal(t, application.ServiceActionStop, model.mutation.prompt.serviceKind)
	assert.False(t, stopped, "preparation must not call the controller")
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.True(t, stopped)
	assert.True(t, model.nodes.isMarked("unrelated-node"), "service actions preserve unrelated node marks")
}

func TestDirectNodeMarksClearOnlyWhenActionIsConfirmed(t *testing.T) {
	root := newDirectNodesModel(t, LaunchOptions{WritesEnabled: true}, []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}})
	root.session = directMutationSession(t, true, domain.ServiceSet{}, func(context.Context, string, ports.RebootMode) error { return nil })
	root.sessionCtx = context.Background()
	root.nodes.marked = map[string]struct{}{"cp-id": {}}

	root, command := updateDirect(root, keyPress('R'))
	require.NotNil(t, command)
	root, _ = applyDirectCommand(t, root, command)
	require.NotNil(t, root.mutation.prompt)
	root.mutation.prompt.assessment.Blocked = "test block"
	root, command = updateDirect(root, keyPress('y'))
	assert.Nil(t, command)
	assert.True(t, root.nodes.isMarked("cp-id"), "blocked action retains marks")
	root, _ = updateDirect(root, keyPress('n'))
	assert.True(t, root.nodes.isMarked("cp-id"), "cancel retains marks")

	root, command = updateDirect(root, keyPress('R'))
	require.NotNil(t, command)
	root, _ = applyDirectCommand(t, root, command)
	require.NotNil(t, root.mutation.prompt)
	root, command = updateDirect(root, keyPress('y'))
	require.NotNil(t, command)
	assert.Empty(t, root.nodes.marked, "confirmed node action clears marks before the dispatched work begins")
}

func TestDirectRootHelpCommandAndChildBack(t *testing.T) {
	root := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker"}})
	root, _ = updateDirect(root, keyPress('?'))
	require.Equal(t, viewHelp, root.views.top().Kind)
	assert.Contains(t, root.View().Content, "HELP")
	root, _ = updateDirect(root, keyPress('q'))
	assert.Equal(t, viewNodes, root.views.top().Kind)

	root, _ = updateDirect(root, keyPress(':'))
	for _, r := range "help" {
		root, _ = updateDirect(root, keyPress(r))
	}
	root, command := updateDirect(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	root, _ = updateDirect(root, command())
	assert.Equal(t, viewHelp, root.views.top().Kind)
}

func TestDirectRootPromptHasPriorityOverPaletteAndGlobalNavigation(t *testing.T) {
	root := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker"}})
	prompt := newSnapshotPromptModel("worker", "worker", "/tmp/snapshot")
	root.etcdPathPrompt = &prompt
	root, _ = updateDirect(root, keyPress(':'))
	assert.False(t, root.palette.active)
	assert.Contains(t, root.etcdPathPrompt.input.Value(), ":", "the active text prompt receives input before global routing")
}

func TestDirectNodeRefreshHeartbeatReschedulesAndPausesOutsideNodeView(t *testing.T) {
	var reads atomic.Int32
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			reads.Add(1)
			return domain.NodeSet{}, nil
		}}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	root := newDirectModel(context.Background(), client, LaunchOptions{})
	root.session, root.sessionCtx = session, context.Background()
	root, command := updateDirect(root, nodeAutoRefreshTickMsg{})
	require.NotNil(t, command, "tick always reschedules the heartbeat")
	assert.Equal(t, application.Loading, root.cluster.nodes.Status)
	root.views = root.views.push(viewFrame{Kind: viewProcesses, Label: "worker > processes"})
	root.cluster.nodes.Status = application.Ready
	root, command = updateDirect(root, nodeAutoRefreshTickMsg{})
	require.NotNil(t, command)
	assert.Equal(t, application.Ready, root.cluster.nodes.Status)
	assert.Equal(t, int32(0), reads.Load(), "paused tick does not issue a node read")
}

func TestDirectNodeRefreshHeartbeatPausesDuringActiveFilterAndReschedules(t *testing.T) {
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	root := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-1", Name: "worker"}})
	root.session = session
	root.sessionCtx = context.Background()
	root.nodes.filtering = true
	root, command := updateDirect(root, nodeAutoRefreshTickMsg{})
	require.NotNil(t, command)
	assert.Equal(t, application.Ready, root.cluster.nodes.Status)
}
