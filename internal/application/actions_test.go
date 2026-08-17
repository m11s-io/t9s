package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildActionEffectsCallsControllerPerTargetIndependently(t *testing.T) {
	var calls []string
	controller := &testkit.FakeNodeController{
		RebootFunc: func(_ context.Context, target string, _ ports.RebootMode) error {
			calls = append(calls, target)
			if target == "cp-2" {
				return errors.New("unreachable")
			}
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: controller})
	pending := application.PendingAction{Kind: application.ActionReboot, Targets: []string{"cp-1", "cp-2"}}

	effects := application.BuildActionEffects(model, pending)

	require.Len(t, effects, 2)
	firstResult := effects[0](t.Context(), application.Dependencies{})
	secondResult := effects[1](t.Context(), application.Dependencies{})
	assert.Equal(t, application.ActionSucceeded{Generation: model.Generation, Target: "cp-1"}, firstResult)
	assert.Equal(t, application.ActionFailed{Generation: model.Generation, Target: "cp-2", Err: errors.New("unreachable")}, secondResult)
	assert.ElementsMatch(t, []string{"cp-1", "cp-2"}, calls)
}

func TestBuildActionEffectsUsesShutdownForShutdownKind(t *testing.T) {
	var shutdownCalled bool
	controller := &testkit.FakeNodeController{
		ShutdownFunc: func(context.Context, string, bool) error {
			shutdownCalled = true
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: controller})
	pending := application.PendingAction{Kind: application.ActionShutdown, Targets: []string{"cp-1"}}

	effects := application.BuildActionEffects(model, pending)

	require.Len(t, effects, 1)
	effects[0](t.Context(), application.Dependencies{})
	assert.True(t, shutdownCalled)
}

func TestBuildActionEffectsUsesRollbackForRollbackKind(t *testing.T) {
	var rollbackCalled bool
	controller := &testkit.FakeNodeController{
		RollbackFunc: func(context.Context, string) error {
			rollbackCalled = true
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: controller})
	pending := application.PendingAction{Kind: application.ActionRollback, Targets: []string{"cp-1"}}

	effects := application.BuildActionEffects(model, pending)

	require.Len(t, effects, 1)
	effects[0](t.Context(), application.Dependencies{})
	assert.True(t, rollbackCalled)
}

func TestBuildActionEffectsUsesUpgradeWithImageForUpgradeKind(t *testing.T) {
	var gotTarget, gotImage string
	controller := &testkit.FakeNodeController{
		UpgradeFunc: func(_ context.Context, target, image string) error {
			gotTarget, gotImage = target, image
			return nil
		},
	}
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: controller})
	pending := application.PendingAction{Kind: application.ActionUpgrade, Targets: []string{"cp-1"}, Image: "ghcr.io/siderolabs/installer:v1.13.3"}

	effects := application.BuildActionEffects(model, pending)

	require.Len(t, effects, 1)
	effects[0](t.Context(), application.Dependencies{})
	assert.Equal(t, "cp-1", gotTarget)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.3", gotImage)
}
