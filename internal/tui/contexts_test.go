package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextsColumnsFitNarrowAndWideViews(t *testing.T) {
	contexts := []domain.ClusterContext{{
		Name: "prod", Cluster: "prod", Endpoints: []string{"10.0.0.1"}, Nodes: []string{"cp-1", "worker-1"},
	}}
	model := newContextsModel(contexts, "prod")

	narrowLines := strings.Split(ansi.Strip(model.viewSized(contentSize{Width: 40, Height: 2})), "\n")
	require.Len(t, narrowLines, 2)
	assert.Equal(t, []string{"NAME", "CLUSTER", "ENDPOINTS", "NODES"}, strings.Fields(narrowLines[0]))
	assert.Contains(t, narrowLines[1], "10.0.0.1")
	assert.Contains(t, narrowLines[1], "2")
	for _, line := range narrowLines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 40)
	}

	wideLines := strings.Split(ansi.Strip(model.viewSized(contentSize{Width: 100, Height: 2})), "\n")
	nodesHeader := strings.Index(wideLines[0], "NODES")
	nodesValue := strings.LastIndex(wideLines[1], "2")
	assert.GreaterOrEqual(t, nodesHeader, 85, "the final column should use the available width")
	assert.Equal(t, nodesHeader, nodesValue, "header and row columns should align")
}
