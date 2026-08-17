package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildServiceActionEffectCallsRestart(t *testing.T) {
	var gotNode, gotService string
	controller := &testkit.FakeServiceController{
		RestartFunc: func(_ context.Context, node, service string) error {
			gotNode, gotService = node, service
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, ServiceController: controller})
	pending := application.PendingServiceAction{Kind: application.ServiceActionRestart, Node: "cp-1", Service: "etcd"}

	effect := application.BuildServiceActionEffect(model, pending)
	result := effect(t.Context(), application.Dependencies{})

	assert.Equal(t, "cp-1", gotNode)
	assert.Equal(t, "etcd", gotService)
	assert.Equal(t, application.ActionSucceeded{Generation: model.Generation, Target: "etcd@cp-1"}, result)
}

func TestBuildServiceActionEffectUsesStartAndStop(t *testing.T) {
	var startCalled, stopCalled bool
	controller := &testkit.FakeServiceController{
		StartFunc: func(context.Context, string, string) error {
			startCalled = true
			return nil
		},
		StopFunc: func(context.Context, string, string) error {
			stopCalled = true
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, ServiceController: controller})

	application.BuildServiceActionEffect(model, application.PendingServiceAction{Kind: application.ServiceActionStart, Node: "cp-1", Service: "kubelet"})(t.Context(), application.Dependencies{})
	application.BuildServiceActionEffect(model, application.PendingServiceAction{Kind: application.ServiceActionStop, Node: "cp-1", Service: "kubelet"})(t.Context(), application.Dependencies{})

	assert.True(t, startCalled)
	assert.True(t, stopCalled)
}

func TestBuildServiceActionEffectReturnsActionFailedOnError(t *testing.T) {
	controller := &testkit.FakeServiceController{
		RestartFunc: func(context.Context, string, string) error {
			return errors.New("unreachable")
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, ServiceController: controller})
	pending := application.PendingServiceAction{Kind: application.ServiceActionRestart, Node: "cp-1", Service: "etcd"}

	result := application.BuildServiceActionEffect(model, pending)(t.Context(), application.Dependencies{})

	require.IsType(t, application.ActionFailed{}, result)
	assert.Equal(t, "etcd@cp-1", result.(application.ActionFailed).Target)
}
