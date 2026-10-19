package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resourceKindsTestState() resourceBrowserState {
	return resourceBrowserState{
		KindsStatus: directReady,
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

func resourceInstancesTestState() resourceBrowserState {
	return resourceBrowserState{
		InstancesStatus: directReady,
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

func TestResourceInstancePhaseColumnStaysNearID(t *testing.T) {
	header := ansi.Strip(strings.Split(newResourceInstancesModel(resourceInstancesTestState()).view(120), "\n")[0])
	id := strings.Index(header, "ID")
	phase := strings.Index(header, "PHASE")

	assert.GreaterOrEqual(t, id, 0)
	assert.Less(t, phase, 55, "the empty part of ID should not push PHASE to the far edge")
	assert.Less(t, phase-id, 30)
}

func TestRenderResourceDetailShowsMetadataAndYAML(t *testing.T) {
	rendered := renderResourceDetailHeader(domain.ResourceInstanceSnapshot{
		Namespace: "runtime", Type: "MachineStatuses.runtime.talos.dev", ID: "machine", Version: "1", Phase: "running",
	}, false)

	assert.Contains(t, rendered, "\x1b[38;2;0;255;255m")
	assert.Contains(t, rendered, "\x1b[1;38;2;255;255;255m FIELD", "detail column headers should use the table header color")
	plain := ansi.Strip(rendered)
	assert.Equal(t, " FIELD            VALUE", strings.Split(plain, "\n")[0])
	assert.True(t, strings.HasPrefix(strings.Split(plain, "\n")[1], " NAMESPACE"))
	assert.Contains(t, plain, "NAMESPACE        runtime")
	assert.Contains(t, plain, "ID               machine")
	assert.Contains(t, plain, "PHASE            running")
}

func TestRenderResourceDetailHeaderMarksSensitiveKind(t *testing.T) {
	rendered := renderResourceDetailHeader(domain.ResourceInstanceSnapshot{ID: "machine"}, true)

	assert.Contains(t, rendered, "⚠")
}

func TestResourceDetailYAMLUsesTheSamePaletteAndInset(t *testing.T) {
	model := newResourceDetailModel().setResult(domain.ResourceInstanceSnapshot{YAML: "key: value"}, directReady, "", false)
	model.bodyHeight = 1

	rendered := model.directViewSized(contentSize{})

	assert.Contains(t, rendered, "\x1b[38;2;0;255;255m key: value")
}

func TestResourceDetailYAMLSanitizesTerminalControlsAndKeepsLineBreaks(t *testing.T) {
	model := newResourceDetailModel().setResult(domain.ResourceInstanceSnapshot{
		YAML: "value: safe\x1b[2J\r\ninjected\nnext: line",
	}, directReady, "", false)
	model.bodyHeight = 3

	rendered := model.directViewSized(contentSize{})
	plain := ansi.Strip(rendered)

	assert.NotContains(t, rendered, "\x1b[2J")
	assert.NotContains(t, plain, "\r")
	assert.Contains(t, plain, "value: safe\n injected", "YAML newlines stay as separate rows after control characters are removed")
	assert.Contains(t, plain, "next: line")
	assert.Equal(t, 10, len(strings.Split(plain, "\n")), "metadata header/rows and YAML lines retain their row structure")
}
