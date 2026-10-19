package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCommandResolutionIsClosedOverKnownNamesAndArguments(t *testing.T) {
	for _, test := range []struct {
		value string
		want  command
	}{
		{"nodes", commandNodes}, {"no", commandNodes},
		{"contexts", commandContexts}, {"ctx", commandContexts},
		{"help", commandHelp}, {"?", commandHelp},
		{"resources", commandResources}, {"res", commandResources},
		{"resources MachineStatus", commandResources},
		{" resources MachineStatus", commandUnknown},
		{"healthcheck", commandClusterHealth}, {"hc", commandClusterHealth},
		{"healthcheck 5m", commandClusterHealth},
		{" nodes", commandUnknown}, {"rm -rf anything", commandUnknown},
		{" healthcheck", commandUnknown}, {"healthcheck nonsense", commandUnknown},
	} {
		assert.Equal(t, test.want, resolveCommand(test.value), "resolve %q", test.value)
	}
	argument, ok := resourcesCommandArgument("resources MachineStatus")
	assert.True(t, ok)
	assert.Equal(t, "MachineStatus", argument)
	_, ok = resourcesCommandArgument("nodes")
	assert.False(t, ok)
	timeout, ok := healthcheckCommandArgument("healthcheck 5m")
	assert.True(t, ok)
	assert.Equal(t, 5*time.Minute, timeout)
	_, ok = healthcheckCommandArgument("nodes")
	assert.False(t, ok)
}

func TestDirectNodesPaletteClosesAndShowsNodes(t *testing.T) {
	model := newModel(t.Context(), nil, LaunchOptions{})
	model.splash = false
	model, command := runDirectPalette(t, model, "nodes")
	assert.NotNil(t, command, "Enter schedules the direct command message")
	assert.Equal(t, viewNodes, model.views.top().Kind)
	assert.False(t, model.palette.active)
	assert.NotContains(t, model.View().Content, "COMMAND :")
}
