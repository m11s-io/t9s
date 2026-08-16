package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resourceKindsTestState() application.ResourceBrowserState {
	return application.ResourceBrowserState{
		KindsStatus: application.Ready,
		Kinds: domain.ResourceKindSet{Kinds: []domain.ResourceKindSnapshot{
			{Type: "MachineStatuses.runtime.talos.dev", DisplayType: "MachineStatus", DefaultNamespace: "runtime"},
			{Type: "SecretsBundle.v1alpha1.talos.dev", DisplayType: "SecretsBundle", DefaultNamespace: "controlplane", Sensitive: true},
		}},
	}
}

func TestResourceKindsRenderSemanticColumnsAndSensitivityMarker(t *testing.T) {
	rendered := newResourceKindsModel(resourceKindsTestState()).view(120)
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t,
		[]string{"TYPE", "NAMESPACE", "ALIASES"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, ansi.Strip(lines[1]), "MachineStatus")
	assert.Contains(t, ansi.Strip(lines[2]), "⚠")
}

func TestResourceKindsFilterMatchesDisplayType(t *testing.T) {
	kinds := newResourceKindsModel(resourceKindsTestState())
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('S'), keyPress('e'), keyPress('c'),
		{Code: tea.KeyEnter},
	} {
		kinds = kinds.update(message)
	}

	rendered := kinds.view(120)

	assert.Contains(t, rendered, "SecretsBundle")
	assert.NotContains(t, rendered, "MachineStatus")
}

func resourceInstancesTestState() application.ResourceBrowserState {
	return application.ResourceBrowserState{
		InstancesStatus: application.Ready,
		SelectedKind:    "MachineStatus",
		Instances: domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{
			{Namespace: "runtime", Type: "MachineStatuses.runtime.talos.dev", ID: "machine", Phase: "running"},
		}},
	}
}

func TestResourceInstancesRenderSemanticColumns(t *testing.T) {
	rendered := newResourceInstancesModel(resourceInstancesTestState()).view(120)
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 2)
	assert.Equal(t,
		[]string{"NAMESPACE", "ID", "PHASE"},
		strings.Fields(ansi.Strip(lines[0])),
	)
}

func TestRenderResourceDetailShowsMetadataAndYAML(t *testing.T) {
	rendered := renderResourceDetailHeader(domain.ResourceInstanceSnapshot{
		Namespace: "runtime", Type: "MachineStatuses.runtime.talos.dev", ID: "machine", Version: "1", Phase: "running",
	}, false)

	assert.Contains(t, rendered, "NAMESPACE  runtime")
	assert.Contains(t, rendered, "ID         machine")
	assert.Contains(t, rendered, "PHASE      running")
}

func TestRenderResourceDetailHeaderMarksSensitiveKind(t *testing.T) {
	rendered := renderResourceDetailHeader(domain.ResourceInstanceSnapshot{ID: "machine"}, true)

	assert.Contains(t, rendered, "⚠")
}

func TestResourcesCommandDrillsFromKindsToInstancesToDetail(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{}, application.NewRunner(application.Dependencies{}))
	root.splash = false

	root = enterCommand(t, root, "resources")
	assert.Equal(t, viewResourceKinds, root.views.top().Kind)
}

func TestResourcesCommandWithArgumentSkipsToInstances(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{}, application.NewRunner(application.Dependencies{}))
	root.splash = false

	root = enterCommand(t, root, "resources MachineStatus")

	require.Equal(t, viewResourceInstances, root.views.top().Kind)
}
