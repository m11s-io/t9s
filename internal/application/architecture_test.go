package application_test

import (
	"context"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceSelectionRejectsSupersededRequest(t *testing.T) {
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Session: &testkit.FakeSession{ResourceInstanceReader: &testkit.FakeResourceInstanceReader{ListFunc: func(_ context.Context, node, kind string) (domain.ResourceInstanceSet, error) {
		return domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{ID: kind}}}, nil
	}}}})
	model, older := application.Update(model, application.SelectResourceKind{Kind: "old", Node: "node"})
	model, newer := application.Update(model, application.SelectResourceKind{Kind: "new", Node: "node"})
	model, _ = application.Update(model, newer(t.Context(), application.Dependencies{}))
	model, _ = application.Update(model, older(t.Context(), application.Dependencies{}))
	require.Len(t, model.ResourceBrowser.Instances.Instances, 1)
	assert.Equal(t, "new", model.ResourceBrowser.Instances.Instances[0].ID)
}

func TestConfirmActionExecutesWithoutUIBuilder(t *testing.T) {
	calls := 0
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Session: &testkit.FakeSession{NodeController: &testkit.FakeNodeController{RebootFunc: func(context.Context, string, ports.RebootMode) error { calls++; return nil }}}})
	model, _ = application.Update(model, application.RequestAction{Kind: application.ActionReboot, Targets: []string{"worker"}})
	model, effect := application.Update(model, application.ConfirmPendingAction{})
	require.NotNil(t, effect)
	result := effect(t.Context(), application.Dependencies{})
	model, _ = application.Update(model, result)
	assert.Equal(t, 1, calls)
	require.Len(t, model.ActionResults, 1)
	assert.Empty(t, model.ActionResults[0].Err)
}

func TestSnapshotConfirmationRequiresWritePermissionAndPendingTarget(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		model, _ := application.NewModel("prod")
		model.WritesEnabled = enabled
		_, effect := application.Update(model, application.ConfirmEtcdSnapshotPrompt{Node: "worker", Path: "/tmp/example.db"})
		assert.Nil(t, effect)
	}
}

func TestNewPromptCannotReplaceExecutingAction(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Session: &testkit.FakeSession{NodeController: &testkit.FakeNodeController{RebootFunc: func(context.Context, string, ports.RebootMode) error { return nil }}}})
	model, _ = application.Update(model, application.RequestAction{Kind: application.ActionReboot, Targets: []string{"old"}})
	model, older := application.Update(model, application.ConfirmPendingAction{})
	require.NotNil(t, older)
	oldResult := older(t.Context(), application.Dependencies{})
	model, _ = application.Update(model, application.RequestAction{Kind: application.ActionReboot, Targets: []string{"new"}})
	assert.Nil(t, model.PendingAction, "a new prompt must not replace an executing action")
	model, _ = application.Update(model, oldResult)
	require.Len(t, model.ActionResults, 1)
	assert.Equal(t, "old", model.ActionResults[0].Target)
	assert.False(t, model.MutationActive())
}

func TestCloseReadPreventsUnstartedRPCAndRejectsItsResult(t *testing.T) {
	calls := 0
	model := withProcessReader(application.Model{Generation: 1}, &testkit.FakeProcessReader{ListFunc: func(context.Context, string) (domain.ProcessSet, error) {
		calls++
		return domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 42}}}, nil
	}})
	model, effect := application.Update(model, application.OpenProcesses{Node: "cp-1"})
	require.NotNil(t, effect)
	model, closeEffect := application.Update(model, application.CloseRead{Key: "processes"})
	require.Nil(t, closeEffect)
	before := model.Processes
	result := effect(t.Context(), application.Dependencies{})
	assert.Zero(t, calls, "a scheduled request must not start after its screen closes")
	model, next := application.Update(model, result)
	assert.Nil(t, next)
	assert.Equal(t, before, model.Processes, "a late canceled result must not change the closed screen")
}

func TestSameNodeRefreshRejectsCompletedOlderResult(t *testing.T) {
	calls := 0
	model := withProcessReader(application.Model{Generation: 1}, &testkit.FakeProcessReader{ListFunc: func(context.Context, string) (domain.ProcessSet, error) {
		calls++
		return domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: int32(calls)}}}, nil
	}})
	model, older := application.Update(model, application.OpenProcesses{Node: "cp-1"})
	oldResult := older(t.Context(), application.Dependencies{})
	model, newer := application.Update(model, application.RefreshProcesses{})
	newResult := newer(t.Context(), application.Dependencies{})
	model, _ = application.Update(model, newResult)
	model, next := application.Update(model, oldResult)
	assert.Nil(t, next)
	require.Len(t, model.Processes.Value.Processes, 1)
	assert.EqualValues(t, 2, model.Processes.Value.Processes[0].PID)
	assert.Equal(t, application.Ready, model.Processes.Status)
	assert.Equal(t, 2, calls)
}

func TestResourceDetailRejectsCompletedOlderSelection(t *testing.T) {
	calls := []string{}
	model := withResourceReaders(application.Model{Generation: 1, ResourceBrowser: application.ResourceBrowserState{SelectedKind: "MachineStatus", SelectedNode: "cp-1"}}, nil, &testkit.FakeResourceInstanceReader{GetFunc: func(_ context.Context, node, kind, id string) (domain.ResourceInstanceSnapshot, error) {
		calls = append(calls, id)
		return domain.ResourceInstanceSnapshot{ID: id, YAML: "value: " + id}, nil
	}})
	model, older := application.Update(model, application.OpenResourceInstance{ID: "old"})
	oldResult := older(t.Context(), application.Dependencies{})
	model, newer := application.Update(model, application.OpenResourceInstance{ID: "new"})
	model, _ = application.Update(model, newer(t.Context(), application.Dependencies{}))
	model, next := application.Update(model, oldResult)
	assert.Nil(t, next)
	assert.Equal(t, "new", model.ResourceBrowser.Detail.ID)
	assert.Equal(t, "value: new", model.ResourceBrowser.Detail.YAML)
	assert.Equal(t, application.Ready, model.ResourceBrowser.DetailStatus)
	assert.Equal(t, []string{"old", "new"}, calls)
}

func TestSnapshotConfirmationDispatchesOnlyOnce(t *testing.T) {
	calls := 0
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model = withEtcdOperations(model, &testkit.FakeEtcdOperations{SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
		calls++
		return domain.EtcdSnapshotResult{Node: node, Path: path}, nil
	}})
	model, _ = application.Update(model, application.RequestEtcdSnapshotPrompt{Node: "cp-1", MemberHostname: "cp-1"})
	confirm := application.ConfirmEtcdSnapshotPrompt{Node: "cp-1", Path: "/tmp/snapshot.db"}
	model, first := application.Update(model, confirm)
	require.NotNil(t, first)
	model, duplicate := application.Update(model, confirm)
	assert.Nil(t, duplicate, "a second confirmation while the snapshot runs must not dispatch again")
	model, _ = application.Update(model, first(t.Context(), application.Dependencies{}))
	_, duplicate = application.Update(model, confirm)
	assert.Nil(t, duplicate, "a completed snapshot must require a new prompt")
	assert.Equal(t, 1, calls)
	assert.Equal(t, application.Ready, model.EtcdSnapshot.Status)
}

func TestNodeRefreshPreservesEarlierKubernetesObservation(t *testing.T) {
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.KubernetesNodesLoaded{Generation: model.Generation, Nodes: map[string]domain.KubernetesNodeSnapshot{"worker": {Conditions: []domain.KubernetesCondition{{Type: "Ready", Status: "True"}}}}})
	for _, result := range []application.Message{
		application.NodesLoaded{Generation: model.Generation, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "worker"}}}},
		application.NodeServicesLoaded{Generation: model.Generation, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "worker"}}}},
	} {
		model, _ = application.Update(model, result)
		assert.Equal(t, domain.KubernetesReady, model.Nodes.Value.Nodes[0].Kubernetes)
	}
}

func TestChangingResourceKindInvalidatesPendingDetail(t *testing.T) {
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Session: &testkit.FakeSession{ResourceInstanceReader: &testkit.FakeResourceInstanceReader{GetFunc: func(context.Context, string, string, string) (domain.ResourceInstanceSnapshot, error) {
		return domain.ResourceInstanceSnapshot{ID: "old-detail"}, nil
	}}}})
	model, _ = application.Update(model, application.SelectResourceKind{Kind: "old", Node: "worker"})
	model, detail := application.Update(model, application.OpenResourceInstance{ID: "id"})
	result := detail(t.Context(), application.Dependencies{})
	model, _ = application.Update(model, application.SelectResourceKind{Kind: "new", Node: "worker"})
	model, _ = application.Update(model, result)
	assert.Empty(t, model.ResourceBrowser.Detail.ID)
}

type combinedObservation struct{}

func (combinedObservation) List(context.Context) (domain.NodeSet, domain.ServiceSet, error) {
	return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "worker"}}}, domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "worker", Name: "old"}}}, nil
}

type combinedSession struct{ *testkit.FakeSession }

func (combinedSession) NodeServices() ports.NodeServiceReader { return combinedObservation{} }

func TestNodeObservationCannotReplaceNewerServiceRefresh(t *testing.T) {
	model, _ := application.NewModel("prod")

	reader := &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
		return domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "worker", Name: "new"}}}, nil
	}}
	session := combinedSession{FakeSession: &testkit.FakeSession{ServiceReader: reader}}
	model, combined := application.Update(model, application.SessionOpened{Generation: model.Generation, Session: session})
	oldResult := combined(t.Context(), application.Dependencies{})
	model, newer := application.Update(model, application.RefreshServices{})
	model, _ = application.Update(model, newer(t.Context(), application.Dependencies{}))
	model, _ = application.Update(model, oldResult)
	require.Len(t, model.Services.Value.Services, 1)
	assert.Equal(t, "new", model.Services.Value.Services[0].Name)
	assert.Equal(t, "worker", model.Nodes.Value.Nodes[0].Name)
}

func TestConfirmedUpgradeBlocksWritesBeforeStreamStarts(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model, _ = application.Update(model, application.RequestAction{Kind: application.ActionUpgrade, Targets: []string{"worker"}, Image: "image:v1.14.4"})
	model, effect := application.Update(model, application.ConfirmPendingAction{})
	require.NotNil(t, effect)
	assert.True(t, model.Upgrade.Active)
	model, next := application.Update(model, application.RequestAction{Kind: application.ActionReboot, Targets: []string{"other"}})
	assert.Nil(t, model.PendingAction)
	assert.Nil(t, next)
}

func TestFailedCombinedObservationCompletesBothLoadingStates(t *testing.T) {
	model, _ := application.NewModel("prod")
	model, batch := application.Update(model, application.SessionOpened{Generation: model.Generation, Session: failingCombinedSession{&testkit.FakeSession{}}})
	require.NotNil(t, batch)
	model, _ = application.Update(model, batch(t.Context(), application.Dependencies{}))
	assert.Equal(t, application.Failed, model.Nodes.Status)
	assert.Equal(t, application.Failed, model.Services.Status)
	assert.NotEmpty(t, model.Services.Err)
}

type failingCombinedSession struct{ *testkit.FakeSession }

func (failingCombinedSession) NodeServices() ports.NodeServiceReader {
	return failingCombinedObservation{}
}

type failingCombinedObservation struct{}

func (failingCombinedObservation) List(context.Context) (domain.NodeSet, domain.ServiceSet, error) {
	return domain.NodeSet{}, domain.ServiceSet{}, context.DeadlineExceeded
}

func TestNewNodePromptInvalidatesEarlierPromptKind(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Session: &testkit.FakeSession{NodeController: &testkit.FakeNodeController{CurrentInstallImageFunc: func(context.Context, string) (string, error) { return "image:v1.14.4", nil }}}})
	model, older := application.Update(model, application.RequestUpgradePrompt{Target: "worker"})
	require.NotNil(t, older)
	oldResult := older(t.Context(), application.Dependencies{})
	model, _ = application.Update(model, application.RequestResetPrompt{Targets: []string{"worker"}})
	assert.False(t, application.IsCurrentResult(model, oldResult))
}
