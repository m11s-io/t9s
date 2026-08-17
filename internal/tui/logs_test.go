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

func TestServicesLogKeyOpensSelectedServiceLogs(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{ContextName: "prod", Services: application.ServiceState{
		Status: application.Ready,
		Value:  domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "etcd"}}},
	}}, application.NewRunner(application.Dependencies{}))
	root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})

	root, command := updateRoot(root, keyPress('l'))

	require.NotNil(t, command)
	assert.Equal(t, viewServiceLogs, root.views.top().Kind)
	assert.Contains(t, root.views.breadcrumb("prod"), "services > etcd@cp-1 > logs")
	assert.Contains(t, root.View().Content, "SERVICE LOGS")
}

func TestServiceLogsFilterFollowWrapAndClear(t *testing.T) {
	logs := newLogsModel(application.LogState{Status: application.Ready, Lines: []string{
		"starting etcd", "health check ok", strings.Repeat("wide ", 30),
	}})
	for _, message := range []tea.KeyPressMsg{keyPress('/'), keyPress('h'), keyPress('e'), keyPress('a'), {Code: tea.KeyEnter}} {
		logs = logs.update(message)
	}
	assert.Contains(t, logs.viewSized(contentSize{Width: 30, Height: 8}), "health check ok")
	assert.NotContains(t, logs.viewSized(contentSize{Width: 30, Height: 8}), "starting etcd")

	logs = logs.update(keyPress('s'))
	assert.False(t, logs.following)
	logs = logs.setState(application.LogState{Status: application.Ready, Following: true, Lines: []string{"new batch"}})
	assert.False(t, logs.following, "incoming batches preserve a user pause")
	logs = logs.update(keyPress('w'))
	assert.True(t, logs.wrap)
	logs = logs.update(tea.KeyPressMsg{Code: 'c', Text: "C", Mod: tea.ModShift})
	assert.True(t, logs.clearRequested)
}

func TestServiceLogsScrollUpPausesFollowingAndMovesTowardOlderLines(t *testing.T) {
	logs := newLogsModel(application.LogState{Status: application.Ready, Lines: []string{
		"line 1", "line 2", "line 3", "line 4", "line 5",
	}})
	assert.True(t, logs.following)

	logs = logs.update(tea.KeyPressMsg{Code: tea.KeyUp})

	assert.False(t, logs.following)
	view := logs.viewSized(contentSize{Width: 40, Height: 4})
	assert.NotContains(t, view, "line 5", "scrolling up must move away from the newest line")
}

func TestServiceLogsGotoBottomResumesFollowing(t *testing.T) {
	logs := newLogsModel(application.LogState{Status: application.Ready, Lines: []string{
		"line 1", "line 2", "line 3", "line 4", "line 5",
	}})
	logs = logs.update(tea.KeyPressMsg{Code: tea.KeyUp})
	assert.False(t, logs.following)

	logs = logs.update(keyPress('G'))

	assert.True(t, logs.following)
	view := logs.viewSized(contentSize{Width: 40, Height: 4})
	assert.Contains(t, view, "line 5")
}

func TestServiceLogsGotoTopMovesToOldestLineAndPausesFollowing(t *testing.T) {
	logs := newLogsModel(application.LogState{Status: application.Ready, Lines: []string{
		"line 1", "line 2", "line 3", "line 4", "line 5",
	}})

	logs = logs.update(keyPress('g'))

	assert.False(t, logs.following)
	view := logs.viewSized(contentSize{Width: 40, Height: 4})
	assert.Contains(t, view, "line 1")
}

func TestServiceLogsRenderIsBoundedAndShowsRetainedFailure(t *testing.T) {
	logs := newLogsModel(application.LogState{
		Status: application.Failed,
		Lines:  []string{"line one", "line two", "line three"},
		Err:    "log stream unavailable",
	})

	view := logs.viewSized(contentSize{Width: 20, Height: 3})
	assert.LessOrEqual(t, len(strings.Split(view, "\n")), 3)
	assert.Contains(t, view, "log stream")
}

func TestServiceLogsStripANSIBeforeFilteringAndRendering(t *testing.T) {
	logs := newLogsModel(application.LogState{Status: application.Ready, Lines: []string{
		"\x1b[31mcritical\x1b[0m service failure",
	}})
	logs.filter = "critical"

	lines := logs.visibleLines()
	require.Equal(t, []string{"critical service failure"}, lines)
	assert.NotContains(t, logs.viewSized(contentSize{Width: 40, Height: 3}), "\x1b[")
}

func TestServiceLogsSanitizeNeutralizesControlCharacters(t *testing.T) {
	logs := newLogsModel(application.LogState{Status: application.Ready, Lines: []string{
		"safe log\rEVIL",
		"abc\b\b\bXYZ",
		"hello\x0eworld",
		"a\tb",
	}})

	lines := logs.visibleLines()

	require.Equal(t, []string{"safe logEVIL", "abcXYZ", "helloworld", "a b"}, lines)
	for _, line := range lines {
		assert.NotContains(t, line, "\r")
		assert.NotContains(t, line, "\b")
		assert.NotContains(t, line, "\x0e")
		assert.NotContains(t, line, "\t")
		// With control characters neutralized, StringWidth is an accurate
		// predictor of what actually renders (no zero-width \t, no
		// cursor-repositioning \r/\b hiding in the count).
		assert.Equal(t, len(line), ansi.StringWidth(line))
	}
}

func TestServiceLogsSanitizesErrBeforeRendering(t *testing.T) {
	logs := newLogsModel(application.LogState{
		Status: application.Failed,
		Lines:  []string{"line one"},
		Err:    "\x1b[31mstream closed\x1b[0m\rEVIL",
	})

	view := logs.viewSized(contentSize{Width: 40, Height: 4})

	assert.NotContains(t, view, "\x1b[")
	assert.NotContains(t, view, "\r")
	assert.Contains(t, view, "stream closedEVIL")
}

func TestServiceLogsHardwrapANSIAndWideTextWithinDisplayWidth(t *testing.T) {
	logs := newLogsModel(application.LogState{Status: application.Ready, Lines: []string{
		"abcde\x1b[31mfghij\x1b[0m界界",
	}})
	logs.wrap = true

	rendered := logs.renderedLines(6)

	require.NotEmpty(t, rendered)
	for _, line := range rendered {
		assert.LessOrEqual(t, ansi.StringWidth(line), 6)
		assert.NotContains(t, line, "\x1b[")
	}
	assert.Equal(t, "abcdefghij界界", strings.Join(rendered, ""))
}
