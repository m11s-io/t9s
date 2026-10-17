package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimeRenderSampleAndUnknown(t *testing.T) {
	m := newTimeModel(application.TimeState{Status: application.Ready, Value: domain.TimeSnapshot{TimeSample: domain.TimeSample{Difference: 250 * time.Millisecond, DifferenceValid: true}}})
	rendered := m.viewSized(contentSize{Width: 100, Height: 20})
	assert.Contains(t, rendered, "+250ms")
	assert.Contains(t, rendered, "server - node")
	assert.Contains(t, rendered, "sampled difference")
	assert.Contains(t, rendered, "unknown")
}
func TestTimeServerValidation(t *testing.T) {
	for _, s := range []string{"ntp.example.org", "  ntp.example.org  ", "192.0.2.1", "2001:db8::1"} {
		_, err := application.ValidateTimeServer(s)
		assert.NoError(t, err, s)
	}
	for _, s := range []string{"", " ", "ntp host", "host\nname", "https://ntp.example.org", "host/path", "host:123", "[2001:db8::1]:123", "host?x=1"} {
		_, err := application.ValidateTimeServer(s)
		assert.Error(t, err, s)
	}
}

func TestTimeNodeKeyPromptFilterRefreshAndExit(t *testing.T) {
	root := readyRootModel()
	root.splash = false
	root, command := updateRoot(root, keyPress('t'))
	require.NotNil(t, command)
	assert.Equal(t, viewTime, root.views.top().Kind)
	assert.False(t, root.application.WritesEnabled)
	assert.NotEmpty(t, root.application.Time.Node)
	root, _ = updateRoot(root, keyPress('c'))
	require.NotNil(t, root.timePrompt)
	root.timePrompt.input.SetValue("https://invalid.example.org")
	root, command = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Nil(t, command)
	require.NotNil(t, root.timePrompt)
	assert.NotEmpty(t, root.timePrompt.err)
	root.timePrompt.input.SetValue(" 2001:db8::1 ")
	root, command = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	assert.Nil(t, root.timePrompt)
	assert.Equal(t, application.Loading, root.application.Time.CheckStatus)
	assert.Equal(t, "2001:db8::1", root.application.Time.Check.Server)
	root, _ = updateRoot(root, keyPress('c'))
	root, command = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Nil(t, command)
	assert.Nil(t, root.timePrompt)
	assert.Equal(t, viewTime, root.views.top().Kind)
	root, _ = updateRoot(root, keyPress('/'))
	root, _ = updateRoot(root, keyPress('c'))
	assert.Nil(t, root.timePrompt)
	assert.Equal(t, "c", root.time.filter)
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Equal(t, viewTime, root.views.top().Kind)
	root, command = updateRoot(root, keyPress('r'))
	require.NotNil(t, command)
	assert.Equal(t, "2001:db8::1", root.application.Time.Check.Server)
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Equal(t, viewNodes, root.views.top().Kind)
	assert.Equal(t, application.TimeState{}, root.application.Time)
}
func TestTimeContextPickerCancelKeepsStatusView(t *testing.T) {
	root := readyRootModel()
	root.splash = false
	root, _ = updateRoot(root, keyPress('t'))
	node := root.application.Time.Node
	root.palette, _ = root.palette.open()
	root.palette.input.SetValue("ctx")
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, root.contexts.active)
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Equal(t, node, root.application.Time.Node)
}
func TestTimeFilterAppliesToProblemsAndCheckPanel(t *testing.T) {
	m := newTimeModel(application.TimeState{Status: application.Partial, Value: domain.TimeSnapshot{TimeSample: domain.TimeSample{Problems: []string{"configured server measurement unavailable"}}}, CheckStatus: application.Failed, Check: domain.TimeCheckResult{TimeSample: domain.TimeSample{Server: "denied.example.org"}}, CheckErr: "time check permission denied"})
	m.filter = "permission"
	rendered := m.viewSized(contentSize{Width: 100, Height: 15})
	assert.Contains(t, rendered, "permission denied")
	assert.NotContains(t, rendered, "configured server")
	m.filter = "configured"
	rendered = m.viewSized(contentSize{Width: 100, Height: 15})
	assert.Contains(t, rendered, "configured server measurement unavailable")
}

func TestTimeHelpOverlayPreservesResultsUntilTimeIsRemoved(t *testing.T) {
	for _, exit := range []string{"back", "command", "context"} {
		t.Run(exit, func(t *testing.T) {
			root := readyRootModel()
			root.splash = false
			root, _ = updateRoot(root, keyPress('t'))
			prior := application.TimeState{Node: "cp-1", Status: application.Ready, Value: domain.TimeSnapshot{SyncKnown: true, Synced: true}, CheckStatus: application.Ready, Check: domain.TimeCheckResult{TimeSample: domain.TimeSample{Server: "ntp.example.org", DifferenceValid: true, Difference: 250 * time.Millisecond}}}
			root.application.Time = prior
			root.time = root.time.setState(prior)
			root, _ = updateRoot(root, keyPress('?'))
			require.Equal(t, viewHelp, root.views.top().Kind)
			assert.Equal(t, prior, root.application.Time, "Help must keep the underlying Time state alive")
			if exit == "context" {
				root, _ = updateRoot(root, applicationMessage{message: application.SelectContext{Name: "other"}})
			} else if exit == "command" {
				root, _ = updateRoot(root, keyPress(':'))
				root.palette.input.SetValue("nodes")
				root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})
			} else {
				root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
				require.Equal(t, viewTime, root.views.top().Kind)
				assert.Equal(t, prior, root.application.Time)
				assert.Contains(t, root.View().Content, "+250ms")
				root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
			}
			assert.Equal(t, application.TimeState{}, root.application.Time, "Removing Time or switching context must clear its state")
		})
	}
}

func TestTimeBottomScrollMovesUpImmediatelyAndClampsFilteredRows(t *testing.T) {
	root := readyRootModel()
	root.splash = false
	root, _ = updateRoot(root, keyPress('t'))
	state := application.TimeState{Status: application.Ready, Node: "cp-1", Value: domain.TimeSnapshot{TimeSample: domain.TimeSample{Problems: []string{"first problem", "second problem", "third problem", "fourth problem", "fifth problem", "sixth problem", "seventh problem", "eighth problem"}}}, CheckStatus: application.Failed, CheckErr: "denied"}
	root.application.Time = state
	root.time = root.time.setState(state)
	root, _ = updateRoot(root, tea.WindowSizeMsg{Width: 80, Height: 16})
	root, _ = updateRoot(root, keyPress('G'))
	bottom := root.View().Content
	root, _ = updateRoot(root, keyPress('k'))
	assert.NotEqual(t, bottom, root.View().Content, "k after G must move immediately")
	for range 30 {
		root, _ = updateRoot(root, keyPress('j'))
	}
	bottom = root.View().Content
	root, _ = updateRoot(root, keyPress('k'))
	assert.NotEqual(t, bottom, root.View().Content, "repeated j must not accumulate an invisible offset")
	root, _ = updateRoot(root, keyPress('/'))
	for _, r := range "problem" {
		root, _ = updateRoot(root, keyPress(r))
	}
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	root, _ = updateRoot(root, keyPress('G'))
	bottom = root.View().Content
	root, _ = updateRoot(root, keyPress('k'))
	assert.NotEqual(t, bottom, root.View().Content, "G must clamp against filtered rows")
	// A larger viewport makes every filtered row visible, so both scroll keys
	// must keep the stored position at the start instead of hiding future moves.
	root, _ = updateRoot(root, tea.WindowSizeMsg{Width: 80, Height: 40})
	root, _ = updateRoot(root, keyPress('G'))
	assert.Zero(t, root.time.offset)
}
