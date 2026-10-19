package tui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectInitialAndServicesViewsDoNotStartReads(t *testing.T) {
	var catalogCalls, sessionCalls, serviceCalls atomic.Int32
	client := application.NewClient(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			catalogCalls.Add(1)
			return nil, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
			sessionCalls.Add(1)
			return &testkit.FakeSession{ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
				serviceCalls.Add(1)
				return domain.ServiceSet{}, nil
			}}}, nil
		}},
	})

	model := newModel(context.Background(), client, LaunchOptions{})
	firstFrame := model.View().Content
	assert.Contains(t, firstFrame, "t9s")
	assert.Contains(t, firstFrame, "Loading")
	model.splash = false
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "node-alpha", Name: "etcd"}}}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	assert.Contains(t, model.View().Content, "node-alpha")
	assert.Zero(t, catalogCalls.Load())
	assert.Zero(t, sessionCalls.Load())
	assert.Zero(t, serviceCalls.Load())
}

func TestDirectServicesRefreshReadsAndRendersReplacementSnapshot(t *testing.T) {
	var calls atomic.Int32
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{}, nil
			}},
			ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
				calls.Add(1)
				return domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "service-after"}}}, nil
			}},
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	model := newModel(context.Background(), client, LaunchOptions{})
	model.splash = false
	model.session, model.sessionCtx = session, context.Background()
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "service-before"}}}}
	model.services = model.services.normalizeSelection(model.cluster.services.Value.Services, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	model, command := updateDirect(model, keyPress('r'))
	require.NotNil(t, command)
	assert.Zero(t, calls.Load(), "the refresh effect runs only when Bubble Tea executes the command")
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, int32(1), calls.Load())
	assert.Contains(t, model.View().Content, "service-after")
	assert.NotContains(t, model.View().Content, "service-before")
}

func TestDirectStartupFailureIsVisibleWithoutBackendDetails(t *testing.T) {
	const secret = "talosconfig is missing: private backend detail"
	client := application.NewClient(application.Dependencies{ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return nil, errors.New(secret)
	}}})
	model := newModel(context.Background(), client, LaunchOptions{})
	model, _ = applyDirectCommand(t, model, model.Init())
	assert.Contains(t, model.View().Content, "Unable to discover contexts")
	assert.NotContains(t, model.View().Content, secret)
}

func TestDirectQuitKeysReturnQuitCommand(t *testing.T) {
	for name, key := range map[string]tea.KeyPressMsg{
		"q":      keyPress('q'),
		"ctrl-c": {Code: 'c', Mod: tea.ModCtrl},
	} {
		t.Run(name, func(t *testing.T) {
			model := newModel(context.Background(), nil, LaunchOptions{})
			_, command := updateDirect(model, key)
			require.NotNil(t, command)
			assert.IsType(t, tea.QuitMsg{}, command())
		})
	}
}

func TestDirectParentCancellationAndQuitCancelBlockedContextDiscovery(t *testing.T) {
	for _, stop := range []string{"parent cancellation", "quit"} {
		t.Run(stop, func(t *testing.T) {
			started, canceled := make(chan struct{}), make(chan struct{})
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			client := application.NewClient(application.Dependencies{ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(ctx context.Context) ([]domain.ClusterContext, error) {
				close(started)
				<-ctx.Done()
				close(canceled)
				return nil, ctx.Err()
			}}})
			model := newModel(parent, client, LaunchOptions{})
			startup, ok := model.Init()().(tea.BatchMsg)
			require.True(t, ok)
			require.Len(t, startup, 3)
			result := make(chan tea.Msg, 1)
			go func() { result <- startup[0]() }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("context discovery did not start")
			}
			if stop == "parent cancellation" {
				cancelParent()
			} else {
				_, command := updateDirect(model, keyPress('q'))
				require.NotNil(t, command)
				assert.IsType(t, tea.QuitMsg{}, command())
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("stopping the root did not cancel context discovery")
			}
			select {
			case <-result:
			case <-time.After(time.Second):
				t.Fatal("canceled context discovery did not return")
			}
		})
	}
}

func TestDirectOldGenerationNodeResultCannotReplaceSwitchedContext(t *testing.T) {
	model := newModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
	model.splash = false
	model.cluster.contextName = "prod"
	model.cluster.contexts = []domain.ClusterContext{{Name: "prod", Current: true}, {Name: "dev"}}
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "prod-worker", Name: "prod-worker"}}}}
	old, _ := model.requests.begin("cluster-nodes-services", model.generation())
	updated, command := updateDirect(model, directSelectContext{name: "dev"})
	model = updated
	require.NotNil(t, command)
	assert.Equal(t, "dev", model.cluster.contextName)
	model, _ = updateDirect(model, directNodeServicesLoaded{request: old.stamp, nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "stale-prod-worker", Name: "stale-prod-worker"}}}})
	assert.Equal(t, "dev", model.cluster.contextName)
	assert.NotContains(t, model.View().Content, "stale-prod-worker")
	assert.NotContains(t, model.View().Content, "prod-worker")
}

func TestDirectRefreshUpdatesHealthAndPreservesServiceSelection(t *testing.T) {
	serviceSet := domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "etcd"}, {Node: "worker-1", Name: "kubelet"}}}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1", Health: domain.HealthUnhealthy}}}, nil
			}},
			ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) { return serviceSet, nil }},
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	model := newModel(context.Background(), client, LaunchOptions{})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.services = directServiceState{Status: directReady, Value: serviceSet}
	model.services = model.services.normalizeSelection(serviceSet.Services, 0)
	model, _ = runDirectPalette(t, model, "services")
	assert.Equal(t, viewServices, model.views.top().Kind, "the test must enter the services view through the command palette")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyDown})
	selectedService := serviceKey(serviceSet.Services[1])
	assert.Equal(t, selectedService, model.services.selectedKey, "Down selects the second service before refresh")
	model, command := updateDirect(model, keyPress('r'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, selectedService, model.services.selectedKey, "refresh preserves the selected service identity")
	assert.Len(t, application.EvaluateObservations(directHealthInput(model.cluster)), 1)
	assert.Contains(t, model.View().Content, "worker-1")
}

func TestDirectRootNavigationKeysAndHints(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "one", Name: "one"}, {ID: "two", Name: "two"}, {ID: "three", Name: "three"}})
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Nil(t, command)
	assert.Equal(t, viewNodes, model.views.top().Kind, "Escape at the root leaves the current view active")
	model, _ = updateDirect(model, keyPress('G'))
	assert.Equal(t, "three", model.nodes.selectedID)
	model, _ = updateDirect(model, keyPress('g'))
	assert.Equal(t, "one", model.nodes.selectedID)
	view := ansi.Strip(model.View().Content)
	assert.Contains(t, view, "<?> Help")
	assert.Contains(t, view, "<:> Command")
	assert.Contains(t, view, "</> Filter")
	model, _ = updateDirect(model, keyPress('?'))
	model, command = updateDirect(model, keyPress('q'))
	assert.Nil(t, command)
	assert.Equal(t, viewNodes, model.views.top().Kind, "q in help returns to the child view without quitting")
}

func TestDirectServiceFilterConsumesQBeforeQuit(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "etcd"}}}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	model, _ = updateDirect(model, keyPress('/'))
	model, command := updateDirect(model, keyPress('q'))
	assert.Nil(t, command)
	assert.Equal(t, viewServices, model.views.top().Kind)
	assert.Equal(t, "q", model.services.filter)
}

func TestDirectContextPickerOrderingSelectionMarkerClampAndCancel(t *testing.T) {
	values := []domain.ClusterContext{
		{Name: "mgmt", Cluster: "management", Current: true},
		{Name: "prod", Cluster: "production"},
		{Name: "dev", Cluster: "development"},
	}
	model := newModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
	model.splash = false
	model.width, model.height = 80, 24
	model.cluster.contextName = "prod"
	model.cluster.contexts = values
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "underlying-service"}}}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	model.contexts = newContextsModel(values, "prod")
	view := model.View().Content
	assert.Less(t, indexText(view, "dev"), indexText(view, "prod(*)"))
	assert.Contains(t, view, "development")
	assert.NotContains(t, view, "mgmt(*)")
	assert.Contains(t, view, "contexts(all)[3]")
	assert.Contains(t, view, "NAME")
	assert.Contains(t, view, "CLUSTER")
	assert.Contains(t, view, "ENDPOINTS")
	assert.Contains(t, view, "NODES")
	assert.NotContains(t, view, "underlying-service", "the picker owns the content region while active")
	assert.NotContains(t, view, "<d> Detail", "the picker hides detail actions from the underlying view")
	assert.NotContains(t, view, "<l> Logs")

	for range 5 {
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyUp})
	}
	assert.Equal(t, "dev", model.contexts.values[model.contexts.control.Cursor()].Name, "up clamps at the first sorted context")
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Nil(t, command)
	assert.Equal(t, "prod", model.cluster.contextName, "Escape preserves the current context")
	assert.False(t, model.contexts.active)

	model.contexts = newContextsModel(values, "prod")
	for range 5 {
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	assert.Equal(t, "prod", model.contexts.values[model.contexts.control.Cursor()].Name, "down clamps at the last sorted context")
	model.contexts = newContextsModel(values, "prod")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyUp})
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	assert.Equal(t, "mgmt", model.cluster.contextName, "selecting a context starts a switch to the selected row")
}

func TestDirectControlCQuitsFromContextAndPaletteOverlays(t *testing.T) {
	for _, overlay := range []string{"context picker", "command palette"} {
		t.Run(overlay, func(t *testing.T) {
			model := newModel(context.Background(), nil, LaunchOptions{})
			if overlay == "context picker" {
				model.cluster.contexts = []domain.ClusterContext{{Name: "prod"}}
				model.contexts = newContextsModel(model.cluster.contexts, "prod")
			} else {
				model, _ = updateDirect(model, keyPress(':'))
			}
			_, command := updateDirect(model, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			require.NotNil(t, command)
			assert.IsType(t, tea.QuitMsg{}, command())
		})
	}
}

func TestDirectPaletteUnknownEmptyAndEscapeBehavior(t *testing.T) {
	var catalogCalls, sessionCalls atomic.Int32
	client := application.NewClient(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			catalogCalls.Add(1)
			return nil, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
			sessionCalls.Add(1)
			return &testkit.FakeSession{}, nil
		}},
	})
	for _, value := range []string{"rm -rf anything", " "} {
		model := newModel(context.Background(), client, LaunchOptions{})
		model.splash = false
		model.view.notice = "keep"
		model, command := runDirectPalette(t, model, value)
		assert.NotNil(t, command)
		assert.False(t, model.palette.active)
		assert.Equal(t, viewNodes, model.views.top().Kind)
		assert.Contains(t, model.view.notice, "Unknown command")
	}
	assert.Zero(t, catalogCalls.Load())
	assert.Zero(t, sessionCalls.Load())
	empty := newModel(context.Background(), nil, LaunchOptions{})
	empty.splash = false
	empty.view.notice = "keep"
	empty, _ = updateDirect(empty, keyPress(':'))
	empty, command := updateDirect(empty, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	empty, _ = updateDirect(empty, command())
	assert.False(t, empty.palette.active)
	assert.Equal(t, "keep", empty.view.notice, "submitting an empty palette must close without changing state")
	assert.Equal(t, viewNodes, empty.views.top().Kind)

	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.view.notice = "keep"
	model, _ = updateDirect(model, keyPress(':'))
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Nil(t, command)
	assert.Equal(t, "keep", model.view.notice)
	assert.False(t, model.palette.active)
}

func TestDirectWipeCompletionMarksDiskSnapshotLoading(t *testing.T) {
	initial := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sdb", Serial: "old-disk"}}}
	refreshed := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sdc", Serial: "replacement-disk"}}}
	newModel := func(t *testing.T, diskNode string, outcome domain.OperationOutcome) (model, tea.Cmd, *[]string) {
		var reads []string
		refreshing := false
		session := directDestructiveSession(t, func(_ context.Context, node string) (domain.DiskSet, error) {
			if refreshing {
				reads = append(reads, node)
				return refreshed, nil
			}
			return initial, nil
		}, &testkit.FakeNodeController{WipeDeviceFunc: func(context.Context, string, string, ports.DeviceWipeMethod) error {
			if outcome == domain.OperationUnconfirmed {
				return errors.New("wipe failed")
			}
			return nil
		}}, true)
		model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
		model.splash = false
		model.session, model.sessionCtx = session, context.Background()
		model.disks = newDirectDisksModel().setResult(diskNode, initial, nil)
		model.views = model.views.replaceRoot(viewFrame{Kind: viewDisks, Label: diskNode + " > disks", NodeID: diskNode + "-id", NodeTarget: diskNode})
		options := ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sdb", Method: ports.DeviceWipeFast}
		prepared, err := session.PrepareNodeAction(context.Background(), application.NodeActionRequest{
			Kind: application.ActionWipeDevice, Targets: []string{"cp-1"}, DeviceWipe: &options,
		})
		require.NoError(t, err)
		model.mutation.operation = 7
		model.mutation.busy = true
		model.mutationWork = directMutationWork{
			operation: 7, generation: model.generation(),
			plan: &directMutationPrompt{node: prepared, nodeKind: application.ActionWipeDevice, deviceWipe: &options, assessment: prepared.Assessment()},
		}
		result, operationErr := session.ExecuteNodeAction(context.Background(), prepared, application.Confirmation{Approved: true, Token: "/dev/sdb"}, nil)
		model, command := updateDirect(model, directMutationFinished{
			operation: 7, generation: model.generation(), result: result, err: operationErr,
		})
		if command != nil {
			refreshing = true
		}
		return model, command, &reads
	}

	t.Run("accepted matching target refreshes visible inventory", func(t *testing.T) {
		model, command, reads := newModel(t, "cp-1", domain.OperationAccepted)
		assert.Equal(t, directLoading, model.disks.status)
		require.NotNil(t, command, "accepted wipe starts a read refresh without claiming completion")
		model, _ = applyDirectCommand(t, model, command)
		assert.Equal(t, []string{"cp-1"}, *reads)
		assert.Equal(t, "cp-1", model.disks.node)
		assert.Equal(t, domain.OperationAccepted, model.mutation.results[0].Outcome)
		assert.Equal(t, refreshed, model.disks.values, "the post-dispatch read replaces the prior inventory")
		assert.Equal(t, directReady, model.disks.status)
	})

	t.Run("another displayed node is left untouched", func(t *testing.T) {
		model, command, reads := newModel(t, "worker-1", domain.OperationAccepted)
		assert.Nil(t, command)
		assert.Zero(t, len(*reads))
		assert.Equal(t, directReady, model.disks.status)
		assert.Equal(t, initial, model.disks.values)
	})
	t.Run("unconfirmed error does not refresh", func(t *testing.T) {
		model, command, reads := newModel(t, "cp-1", domain.OperationUnconfirmed)
		assert.Nil(t, command)
		assert.Zero(t, len(*reads))
		assert.Equal(t, directReady, model.disks.status)
		assert.Equal(t, initial, model.disks.values)
	})
}

func indexText(value, fragment string) int {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return i
		}
	}
	return -1
}
