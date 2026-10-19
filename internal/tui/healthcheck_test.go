package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
)

func TestDirectHealthcheckRendersNormalizedFailureWithoutTransportDetails(t *testing.T) {
	health := &testkit.FakeClusterHealthReader{OpenFunc: func(context.Context, domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
		return nil, errors.New("rpc error: code = DeadlineExceeded desc = context deadline exceeded")
	}}
	session, _ := directStreamSession(t, nil, health)
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	model, command := runDirectPaletteCommand(t, model, "healthcheck 1m")
	for command != nil {
		model, command = runStreamCommand(t, model, command)
	}

	rendered := model.View().Content
	assert.Contains(t, rendered, "Cluster health check is unavailable")
	assert.NotContains(t, rendered, "rpc error")
	assert.NotContains(t, rendered, "DeadlineExceeded")
}
