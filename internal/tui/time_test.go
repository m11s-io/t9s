package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectTimeRenderSampleAndUnknown(t *testing.T) {
	model := newDirectTimeModel().setReadResult("node", domain.TimeSnapshot{TimeSample: domain.TimeSample{Difference: 250 * time.Millisecond, DifferenceValid: true}}, nil)
	rendered := model.viewSized(contentSize{Width: 100, Height: 20})
	assert.Contains(t, rendered, "+250ms")
	assert.Contains(t, rendered, "server - node")
	assert.Contains(t, rendered, "sampled difference")
	assert.Contains(t, rendered, "unknown")
}

func TestTimeServerValidation(t *testing.T) {
	for _, value := range []string{"ntp.example.org", "  ntp.example.org  ", "192.0.2.1", "2001:db8::1"} {
		_, err := application.ValidateTimeServer(value)
		assert.NoError(t, err, value)
	}
	for _, value := range []string{"", " ", "ntp host", "host\nname", "https://ntp.example.org", "host/path", "host:123", "[2001:db8::1]:123", "host?x=1"} {
		_, err := application.ValidateTimeServer(value)
		assert.Error(t, err, value)
	}
}

func TestDirectTimeKeyPromptFilterRefreshAndExit(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-id", Name: "cp-1"}})
	model.session = openCPUTimeSession(t)
	model.sessionCtx = context.Background()
	model, command := updateDirect(model, keyPress('t'))
	require.Equal(t, viewTime, model.views.top().Kind)
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.False(t, model.options.WritesEnabled)
	assert.Equal(t, "cp-1", model.time.node)

	model, _ = updateDirect(model, keyPress('c'))
	require.NotNil(t, model.timePrompt)
	model.timePrompt.input.SetValue("https://invalid.example.org")
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Nil(t, command)
	require.NotNil(t, model.timePrompt)
	assert.NotEmpty(t, model.timePrompt.err)
	model.timePrompt.input.SetValue(" 2001:db8::1 ")
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	assert.Nil(t, model.timePrompt)
	assert.Equal(t, directLoading, model.time.checkStatus, "the accepted server check is loading until its command completes")
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, "2001:db8::1", model.time.check.Server)

	model, _ = updateDirect(model, keyPress('c'))
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Nil(t, command)
	assert.Nil(t, model.timePrompt)
	assert.Equal(t, viewTime, model.views.top().Kind)
	model, _ = updateDirect(model, keyPress('/'))
	model, _ = updateDirect(model, keyPress('c'))
	assert.Nil(t, model.timePrompt)
	assert.Equal(t, "c", model.time.filter)
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, viewTime, model.views.top().Kind)
	model, command = updateDirect(model, keyPress('r'))
	require.NotNil(t, command)
	model, _ = updateDirect(model, command())
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, viewNodes, model.views.top().Kind)
}

func TestDirectTimeFilterAppliesToProblemsAndCheckPanel(t *testing.T) {
	model := newDirectTimeModel().setReadResult("node", domain.TimeSnapshot{Problems: []string{"configured server measurement unavailable"}}, nil)
	model = model.setCheckResult("node", "denied.example.org", domain.TimeCheckResult{TimeSample: domain.TimeSample{Server: "denied.example.org"}}, ports.ErrTimePermissionDenied)
	model.filter = "permission"
	rendered := model.viewSized(contentSize{Width: 100, Height: 15})
	assert.Contains(t, rendered, "permission denied")
	assert.NotContains(t, rendered, "configured server")
	model.filter = "configured"
	rendered = model.viewSized(contentSize{Width: 100, Height: 15})
	assert.Contains(t, rendered, "configured server measurement unavailable")
}

func TestDirectTimeHelpAndContextPickerPreserveVisibleResult(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-id", Name: "cp-1"}})
	model.cluster.contextName = "prod"
	model.cluster.contexts = []domain.ClusterContext{{Name: "prod"}, {Name: "stage"}}
	model.time = newDirectTimeModel().setReadResult("cp-1", domain.TimeSnapshot{SyncKnown: true, Synced: true}, nil)
	model.time = model.time.setCheckResult("cp-1", "ntp.example.org", domain.TimeCheckResult{TimeSample: domain.TimeSample{Difference: 250 * time.Millisecond, DifferenceValid: true}}, nil)
	model.views = model.views.push(viewFrame{Kind: viewTime, Label: "cp-1 > time", NodeID: "node-id", NodeTarget: "cp-1"})

	model, _ = updateDirect(model, keyPress('?'))
	require.Equal(t, viewHelp, model.views.top().Kind)
	assert.Equal(t, "ntp.example.org", model.time.check.Server, "Help keeps the underlying time result")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, viewTime, model.views.top().Kind)
	assert.Contains(t, model.View().Content, "+250ms")

	model, _ = runDirectPalette(t, model, "ctx")
	require.True(t, model.contexts.active)
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.False(t, model.contexts.active)
	assert.Equal(t, viewTime, model.views.top().Kind)
	assert.Contains(t, model.View().Content, "+250ms")

	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, viewNodes, model.views.top().Kind)
	assert.NotEqual(t, viewTime, model.views.top().Kind, "leaving Time hides its state")
}

func TestDirectTimeBottomScrollClampsFilteredRows(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-id", Name: "cp-1"}})
	model.time = newDirectTimeModel().setReadResult("cp-1", domain.TimeSnapshot{TimeSample: domain.TimeSample{Problems: []string{"first problem", "second problem", "third problem", "fourth problem", "fifth problem", "sixth problem", "seventh problem", "eighth problem"}}}, nil)
	model.time = model.time.setCheckResult("cp-1", "", domain.TimeCheckResult{}, assert.AnError)
	model.views = model.views.push(viewFrame{Kind: viewTime, Label: "cp-1 > time", NodeID: "node-id", NodeTarget: "cp-1"})
	model, _ = updateDirect(model, tea.WindowSizeMsg{Width: 80, Height: 16})
	model, _ = updateDirect(model, keyPress('G'))
	bottom := model.View().Content
	model, _ = updateDirect(model, keyPress('k'))
	assert.NotEqual(t, bottom, model.View().Content, "k after G moves immediately")
	for range 30 {
		model, _ = updateDirect(model, keyPress('j'))
	}
	bottom = model.View().Content
	model, _ = updateDirect(model, keyPress('k'))
	assert.NotEqual(t, bottom, model.View().Content, "repeated j does not accumulate an invisible offset")
	model, _ = updateDirect(model, keyPress('/'))
	for _, character := range "problem" {
		model, _ = updateDirect(model, keyPress(character))
	}
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model, _ = updateDirect(model, keyPress('G'))
	bottom = model.View().Content
	model, _ = updateDirect(model, keyPress('k'))
	assert.NotEqual(t, bottom, model.View().Content, "G clamps against filtered rows")
	model, _ = updateDirect(model, tea.WindowSizeMsg{Width: 80, Height: 40})
	model, _ = updateDirect(model, keyPress('G'))
	assert.Zero(t, model.time.offset, "when all filtered rows fit, the offset stays at the start")
}
