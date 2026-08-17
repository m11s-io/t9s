package talos

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeServiceControlClient struct {
	startID    string
	startErr   error
	stopID     string
	stopErr    error
	restartID  string
	restartErr error
}

func (c *fakeServiceControlClient) Start(ctx context.Context, id string) error {
	c.startID = id
	return c.startErr
}

func (c *fakeServiceControlClient) Stop(ctx context.Context, id string) error {
	c.stopID = id
	return c.stopErr
}

func (c *fakeServiceControlClient) Restart(ctx context.Context, id string) error {
	c.restartID = id
	return c.restartErr
}

func TestServiceControllerStartSendsServiceID(t *testing.T) {
	client := &fakeServiceControlClient{}
	controller := newServiceController(client)

	err := controller.Start(t.Context(), "cp-1", "etcd")

	require.NoError(t, err)
	assert.Equal(t, "etcd", client.startID)
}

func TestServiceControllerStartWrapsError(t *testing.T) {
	client := &fakeServiceControlClient{startErr: errors.New("unreachable")}
	controller := newServiceController(client)

	err := controller.Start(t.Context(), "cp-1", "etcd")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "etcd")
	assert.Contains(t, err.Error(), "cp-1")
}

func TestServiceControllerStopSendsServiceID(t *testing.T) {
	client := &fakeServiceControlClient{}
	controller := newServiceController(client)

	err := controller.Stop(t.Context(), "cp-1", "etcd")

	require.NoError(t, err)
	assert.Equal(t, "etcd", client.stopID)
}

func TestServiceControllerRestartSendsServiceID(t *testing.T) {
	client := &fakeServiceControlClient{}
	controller := newServiceController(client)

	err := controller.Restart(t.Context(), "cp-1", "etcd")

	require.NoError(t, err)
	assert.Equal(t, "etcd", client.restartID)
}

func TestServiceControllerRestartWrapsError(t *testing.T) {
	client := &fakeServiceControlClient{restartErr: errors.New("service not found")}
	controller := newServiceController(client)

	err := controller.Restart(t.Context(), "cp-1", "unknown")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
	assert.Contains(t, err.Error(), "cp-1")
}
