package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func directResourceModel(t *testing.T, kindReader *testkit.FakeResourceKindReader, instanceReader *testkit.FakeResourceInstanceReader) directModel {
	t.Helper()
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{ResourceKindReader: kindReader, ResourceInstanceReader: instanceReader}, nil
	}}})
	model := newDirectModel(context.Background(), client, LaunchOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	session, err := client.Open(ctx, "dev", application.Options{})
	require.NoError(t, err)
	model.session, model.sessionCtx, model.sessionCancel = session, ctx, cancel
	model.splash = false
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "node-id", Name: "worker-1"}}}}
	model.nodes = model.nodes.normalizeSelection(model.cluster.nodes.Value.Nodes, 0)
	return model
}

func TestDirectResourcesKeyboardLoadsKindsMetadataThenSelectedYAML(t *testing.T) {
	getCalls := 0
	kind := domain.ResourceKindSnapshot{Type: "Secrets.runtime.talos.dev", DisplayType: "Secret", DefaultNamespace: "runtime", Aliases: []string{"secrets"}, Sensitive: true}
	model := directResourceModel(t,
		&testkit.FakeResourceKindReader{ListFunc: func(context.Context) (domain.ResourceKindSet, error) {
			return domain.ResourceKindSet{Kinds: []domain.ResourceKindSnapshot{kind}}, nil
		}},
		&testkit.FakeResourceInstanceReader{
			ListFunc: func(_ context.Context, node, gotKind string) (domain.ResourceInstanceSet, error) {
				assert.Equal(t, "worker-1", node)
				assert.Equal(t, kind.Type, gotKind)
				return domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{Namespace: "runtime", Type: kind.Type, ID: "secret-a", Phase: "active"}}}, nil
			},
			GetFunc: func(_ context.Context, node, gotKind, id string) (domain.ResourceInstanceSnapshot, error) {
				getCalls++
				assert.Equal(t, "worker-1", node)
				assert.Equal(t, kind.Type, gotKind)
				assert.Equal(t, "secret-a", id)
				return domain.ResourceInstanceSnapshot{Namespace: "runtime", Type: kind.Type, ID: id, Version: "9", Phase: "active", YAML: "data:\n  token: example"}, nil
			},
		},
	)
	command := model.openDirectResources("")
	model, _ = updateDirect(model, command())
	require.Equal(t, viewResourceKinds, model.views.top().Kind)
	assert.Equal(t, directReady, model.resourceKinds.status)
	model, _ = updateDirect(model, keyPress('/'))
	model, _ = updateDirect(model, keyPress('q'))
	assert.Contains(t, model.View().Content, "/q", "q should be editable while filtering")
	assert.Equal(t, viewResourceKinds, model.views.top().Kind)
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	model, _ = updateDirect(model, command())
	require.Equal(t, viewResourceInstances, model.views.top().Kind)
	assert.Zero(t, getCalls, "metadata list must not load YAML")
	assert.Contains(t, model.View().Content, "secret-a")
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	model, _ = updateDirect(model, command())
	require.Equal(t, viewResourceDetail, model.views.top().Kind)
	assert.Equal(t, 1, getCalls)
	content := model.View().Content
	assert.Contains(t, content, "⚠ SENSITIVE")
	assert.Contains(t, content, "data:")
	assert.Contains(t, content, "token: example")
}

func TestDirectResourceAliasesOpenRequestedKind(t *testing.T) {
	kind := domain.ResourceKindSnapshot{Type: "MachineStatuses.runtime.talos.dev", DisplayType: "MachineStatus", Aliases: []string{"machine"}}
	model := directResourceModel(t,
		&testkit.FakeResourceKindReader{ListFunc: func(context.Context) (domain.ResourceKindSet, error) {
			return domain.ResourceKindSet{Kinds: []domain.ResourceKindSnapshot{kind}}, nil
		}},
		&testkit.FakeResourceInstanceReader{ListFunc: func(context.Context, string, string) (domain.ResourceInstanceSet, error) {
			return domain.ResourceInstanceSet{}, nil
		}},
	)
	command := model.runClusterCommand("res machine")
	require.NotNil(t, command)
	updated, command := model.Update(command())
	model = updated.(directModel)
	require.NotNil(t, command, "loading the requested kind should continue into its metadata list")
	model, _ = updateDirect(model, command())
	assert.Equal(t, viewResourceInstances, model.views.top().Kind, "the resources/res alias and kind argument should open the matching list")
	assert.Equal(t, kind.Type, model.views.top().ResourceKind)
}

func TestDirectResourceDetailPinsIdentityAndDiscardsStaleABAKindResults(t *testing.T) {
	model := directResourceModel(t, &testkit.FakeResourceKindReader{}, &testkit.FakeResourceInstanceReader{})
	model.views = model.views.push(viewFrame{Kind: viewResourceKinds, Label: "resources"})
	kinds := domain.ResourceKindSet{Kinds: []domain.ResourceKindSnapshot{{Type: "A", DisplayType: "A"}, {Type: "B", DisplayType: "B"}}}
	request, _ := model.requests.beginWithContext("resources:kinds", model.generation(), model.sessionCtx)
	model, _ = updateDirect(model, directResourceKindsLoaded{request: request.stamp, values: kinds})
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	frameA := model.views.top()
	first, _ := model.requests.beginWithContext(directResourceInstanceSlot(frameA.NodeID, "A"), model.generation(), model.sessionCtx)
	model.popDirectResourceView()
	frameB := viewFrame{Kind: viewResourceInstances, NodeID: "node-id", NodeTarget: "worker-1", ResourceKind: "B"}
	model.views = model.views.push(frameB)
	b, _ := model.requests.beginWithContext(directResourceInstanceSlot(frameB.NodeID, "B"), model.generation(), model.sessionCtx)
	model.popDirectResourceView()
	model.views = model.views.push(frameA)
	second, _ := model.requests.beginWithContext(directResourceInstanceSlot(frameA.NodeID, "A"), model.generation(), model.sessionCtx)
	model, _ = updateDirect(model, directResourceInstancesLoaded{request: first.stamp, nodeID: frameA.NodeID, node: frameA.NodeTarget, kind: "A", values: domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{ID: "stale"}}}})
	assert.Empty(t, model.resourceInstances.values.Instances, "first A response must be discarded after A-B-A")
	model, _ = updateDirect(model, directResourceInstancesLoaded{request: b.stamp, nodeID: frameB.NodeID, node: frameB.NodeTarget, kind: "B", values: domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{ID: "wrong"}}}})
	assert.Empty(t, model.resourceInstances.values.Instances, "B response must not retarget the A view")
	model, _ = updateDirect(model, directResourceInstancesLoaded{request: second.stamp, nodeID: frameA.NodeID, node: frameA.NodeTarget, kind: "A", values: domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{ID: "fresh"}}}})
	assert.Equal(t, "fresh", model.resourceInstances.values.Instances[0].ID)
	firstItem := domain.ResourceInstanceSnapshot{Namespace: "runtime", ID: "first"}
	selectedItem := domain.ResourceInstanceSnapshot{Namespace: "runtime", ID: "selected"}
	model.resourceInstances = newResourceInstancesModel(application.ResourceBrowserState{}).setInstances(domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{firstItem, selectedItem}}, directReady, "")
	model.resourceInstances.control.SetCursor(1)
	model.resourceInstances.selectedKey = resourceInstanceIdentity(selectedItem)
	model.resourceInstances = model.resourceInstances.setInstances(domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{selectedItem, firstItem}}, directReady, "")
	assert.Equal(t, "selected", model.resourceInstances.selectedValue().ID, "a refreshed list should preserve selected instance identity")

	// Pin detail identity in the frame; a list reorder/filter cannot retarget it.
	detailFrame := viewFrame{Kind: viewResourceDetail, NodeID: "node-id", NodeTarget: "worker-1", ResourceKind: "A", ResourceNamespace: "ns", ResourceID: "pinned"}
	model.views = model.views.push(detailFrame)
	detailRequest, _ := model.requests.beginWithContext(directResourceDetailSlot("node-id", "A", "ns", "pinned"), model.generation(), model.sessionCtx)
	model.resourceInstances = model.resourceInstances.setInstances(domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{ID: "other"}, {Namespace: "ns", ID: "pinned"}}}, directReady, "")
	model.resourceInstances = model.resourceInstances.update(keyPress('/'))
	model.resourceInstances = model.resourceInstances.update(keyPress('o'))
	model, _ = updateDirect(model, directResourceDetailLoaded{request: detailRequest.stamp, nodeID: "node-id", node: "worker-1", kind: "A", namespace: "ns", id: "pinned", value: domain.ResourceInstanceSnapshot{Namespace: "ns", ID: "pinned", YAML: "pinned: true"}})
	assert.Equal(t, "pinned", model.resourceDetail.value.ID)
	assert.Contains(t, model.View().Content, "pinned: true")
}

func TestDirectResourceReadCancellationAndSafeFailures(t *testing.T) {
	model := directResourceModel(t, &testkit.FakeResourceKindReader{ListFunc: func(context.Context) (domain.ResourceKindSet, error) {
		return domain.ResourceKindSet{}, errors.New("raw transport detail")
	}}, &testkit.FakeResourceInstanceReader{})
	command := model.openDirectResources("")
	request := model.requests.slots["resources:kinds"].request
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Error(t, request.ctx.Err())
	command = model.openDirectResources("")
	model, _ = updateDirect(model, command())
	assert.NotContains(t, model.View().Content, "raw transport detail")
	assert.Contains(t, model.View().Content, "Resource kinds are unavailable")
}

func TestDirectResourcePartialReadKeepsUsefulRows(t *testing.T) {
	model := directResourceModel(t, &testkit.FakeResourceKindReader{}, &testkit.FakeResourceInstanceReader{})
	frame := viewFrame{Kind: viewResourceInstances, NodeID: "node-id", NodeTarget: "worker-1", ResourceKind: "kind"}
	model.views = model.views.push(frame)
	request, _ := model.requests.beginWithContext(directResourceInstanceSlot(frame.NodeID, frame.ResourceKind), model.generation(), model.sessionCtx)
	model, _ = updateDirect(model, directResourceInstancesLoaded{request: request.stamp, nodeID: frame.NodeID, node: frame.NodeTarget, kind: frame.ResourceKind, values: domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{Namespace: "runtime", ID: "available"}}}, err: errors.New("raw rpc error")})
	assert.Equal(t, directPartial, model.resourceInstances.status)
	assert.Contains(t, model.View().Content, "available")
	assert.Contains(t, model.View().Content, "Resource instances are unavailable")
	assert.NotContains(t, model.View().Content, "raw rpc error")
}

func TestDirectResourceKindKeyboardSelectionSurvivesRefreshReorder(t *testing.T) {
	reads := 0
	model := directResourceModel(t, &testkit.FakeResourceKindReader{ListFunc: func(context.Context) (domain.ResourceKindSet, error) {
		reads++
		return domain.ResourceKindSet{Kinds: []domain.ResourceKindSnapshot{{Type: "kind-b", DisplayType: "B"}, {Type: "kind-a", DisplayType: "A"}}}, nil
	}}, &testkit.FakeResourceInstanceReader{})
	model.views = model.views.push(viewFrame{Kind: viewResourceKinds, Label: "resources"})
	model.resourceKinds = newResourceKindsModel(application.ResourceBrowserState{}).setKinds(domain.ResourceKindSet{Kinds: []domain.ResourceKindSnapshot{{Type: "kind-a", DisplayType: "A"}, {Type: "kind-b", DisplayType: "B"}}}, directReady, "")
	model, _ = updateDirect(model, keyPress('j'))
	assert.Equal(t, "kind-b", model.resourceKinds.selectedValue().Type)
	model, command := updateDirect(model, keyPress('r'))
	require.NotNil(t, command)
	model, _ = updateDirect(model, command())
	assert.Equal(t, 1, reads)
	assert.Equal(t, "kind-b", model.resourceKinds.selectedValue().Type, "refresh should retain the row chosen with j even after reorder")
}

func TestDirectResourceInstanceKeyboardSelectionSurvivesRefreshReorder(t *testing.T) {
	reads := 0
	model := directResourceModel(t, &testkit.FakeResourceKindReader{}, &testkit.FakeResourceInstanceReader{ListFunc: func(context.Context, string, string) (domain.ResourceInstanceSet, error) {
		reads++
		values := []domain.ResourceInstanceSnapshot{{Namespace: "ns", ID: "b"}, {Namespace: "ns", ID: "a"}}
		if reads == 1 {
			values = []domain.ResourceInstanceSnapshot{{Namespace: "ns", ID: "a"}, {Namespace: "ns", ID: "b"}}
		}
		return domain.ResourceInstanceSet{Instances: values}, nil
	}})
	frame := viewFrame{Kind: viewResourceInstances, NodeID: "node-id", NodeTarget: "worker-1", ResourceKind: "kind"}
	model.views = model.views.push(frame)
	model.resourceInstances = newResourceInstancesModel(application.ResourceBrowserState{}).setInstances(domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{Namespace: "ns", ID: "a"}, {Namespace: "ns", ID: "b"}}}, directReady, "")
	model, _ = updateDirect(model, keyPress('j'))
	assert.Equal(t, "b", model.resourceInstances.selectedValue().ID)
	model, command := updateDirect(model, keyPress('r'))
	require.NotNil(t, command)
	model, _ = updateDirect(model, command())
	assert.Equal(t, 1, reads)
	assert.Equal(t, "b", model.resourceInstances.selectedValue().ID, "refresh should retain the row chosen with j even after reorder")
}

func TestDirectResourceCommandReentryReplacesExistingBrowserBranch(t *testing.T) {
	kinds := []domain.ResourceKindSnapshot{{Type: "kind-a", DisplayType: "A"}, {Type: "kind-b", DisplayType: "B"}}
	model := directResourceModel(t, &testkit.FakeResourceKindReader{ListFunc: func(context.Context) (domain.ResourceKindSet, error) {
		return domain.ResourceKindSet{Kinds: kinds}, nil
	}}, &testkit.FakeResourceInstanceReader{ListFunc: func(context.Context, string, string) (domain.ResourceInstanceSet, error) {
		return domain.ResourceInstanceSet{}, nil
	}})
	model.views = model.views.push(viewFrame{Kind: viewResourceKinds, Label: "resources"})
	frameA := viewFrame{Kind: viewResourceInstances, NodeID: "node-id", NodeTarget: "worker-1", ResourceKind: "kind-a"}
	model.views = model.views.push(frameA)
	model.resourceKinds = newResourceKindsModel(application.ResourceBrowserState{}).setKinds(domain.ResourceKindSet{Kinds: kinds}, directReady, "")
	model.resourceInstances = newResourceInstancesModel(application.ResourceBrowserState{}).setInstances(domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{ID: "A-only"}}}, directReady, "")
	model = runDirectPaletteThroughCommands(t, model, "res kind-b")
	assert.Equal(t, "kind-b", model.views.top().ResourceKind)
	resourceFrames := 0
	for _, frame := range model.views {
		if frame.Kind == viewResourceKinds {
			resourceFrames++
		}
	}
	assert.Equal(t, 1, resourceFrames, "reopening resources should reuse its browser branch")
	model, _ = updateDirect(model, keyPress('q'))
	assert.Equal(t, viewResourceKinds, model.views.top().Kind)
	assert.NotContains(t, model.View().Content, "A-only", "back should show the new browser scope, not stale kind A rows")
	model, _ = updateDirect(model, keyPress('q'))
	assert.Equal(t, viewNodes, model.views.top().Kind)
}

func runDirectPaletteThroughCommands(t *testing.T, model directModel, value string) directModel {
	t.Helper()
	model, _ = updateDirect(model, keyPress(':'))
	for _, character := range value {
		model, _ = updateDirect(model, keyPress(character))
	}
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	for steps := 0; command != nil && steps < 6; steps++ {
		model, command = updateDirect(model, command())
	}
	return model
}

func TestDirectResourceCanceledKindAliasDoesNotLeakIntoPlainOpen(t *testing.T) {
	kind := domain.ResourceKindSnapshot{Type: "kind-a", DisplayType: "A"}
	model := directResourceModel(t, &testkit.FakeResourceKindReader{ListFunc: func(context.Context) (domain.ResourceKindSet, error) {
		return domain.ResourceKindSet{Kinds: []domain.ResourceKindSnapshot{kind}}, nil
	}}, &testkit.FakeResourceInstanceReader{ListFunc: func(context.Context, string, string) (domain.ResourceInstanceSet, error) {
		return domain.ResourceInstanceSet{}, nil
	}})
	command := model.runClusterCommand("resources kind-a")
	require.NotNil(t, command)
	model, _ = updateDirect(model, keyPress('q'))
	assert.Empty(t, model.pendingResourceKind)
	command = model.runClusterCommand("resources")
	model, command = updateDirect(model, command())
	assert.Equal(t, viewResourceKinds, model.views.top().Kind, "plain resource open must remain on the kinds list")
	assert.Empty(t, model.pendingResourceKind)
	if command != nil {
		model, _ = updateDirect(model, command())
	}
	assert.Equal(t, viewResourceKinds, model.views.top().Kind)
}

func TestDirectResourceDetailShowsLoadingAndRetainsYAMLScrollAcrossResize(t *testing.T) {
	var yaml strings.Builder
	for index := 0; index < 70; index++ {
		fmt.Fprintf(&yaml, "line-%02d: value\n", index)
	}
	model := directResourceModel(t, &testkit.FakeResourceKindReader{}, &testkit.FakeResourceInstanceReader{GetFunc: func(_ context.Context, node, kind, id string) (domain.ResourceInstanceSnapshot, error) {
		return domain.ResourceInstanceSnapshot{Namespace: "ns", Type: kind, ID: id, YAML: yaml.String()}, nil
	}})
	model.width, model.height = 100, 32
	frame := viewFrame{Kind: viewResourceInstances, NodeID: "node-id", NodeTarget: "worker-1", ResourceKind: "kind"}
	model.views = model.views.push(frame)
	model.resourceInstances = newResourceInstancesModel(application.ResourceBrowserState{}).setInstances(domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{Namespace: "ns", ID: "selected"}}}, directReady, "")
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	assert.Contains(t, model.View().Content, "Loading resource…")
	model, _ = updateDirect(model, command())
	initial := model.View().Content
	assert.Contains(t, initial, "line-00: value")
	model, _ = updateDirect(model, keyPress('j'))
	model, _ = updateDirect(model, keyPress('j'))
	model, _ = updateDirect(model, keyPress('j'))
	model, _ = updateDirect(model, keyPress('j'))
	model, _ = updateDirect(model, keyPress('j'))
	afterScroll := model.View().Content
	assert.NotEqual(t, initial, afterScroll, "j should scroll the displayed YAML")
	assert.Contains(t, afterScroll, "line-05: value")
	model, _ = updateDirect(model, keyPress('G'))
	model, _ = updateDirect(model, tea.WindowSizeMsg{Width: 100, Height: 29})
	afterResize := model.View().Content
	assert.Contains(t, afterResize, "line-69: value", "G should reach the end and resizing should retain the scrolled position")
	assert.NotContains(t, afterResize, "line-00: value")
}
