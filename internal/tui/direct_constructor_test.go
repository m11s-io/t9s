package tui

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestNewWithCleanupOwnsSessionUntilQuitAndReturnsIdempotentCleanup(t *testing.T) {
	adapter := &testkit.FakeSession{}
	client := application.NewClient(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
			return adapter, nil
		}},
	})

	model, cleanup := NewWithCleanup(t.Context(), client, LaunchOptions{NodeFocus: "worker-1", WritesEnabled: true})
	require.NotNil(t, model)
	require.NotNil(t, cleanup)
	direct := modelFromTea(model)
	startup := direct.Init()().(tea.BatchMsg)
	updated, open := direct.Update(startup[0]())
	direct = modelFromTea(updated)
	require.NotNil(t, open)
	opened := open()
	updated, _ = direct.Update(opened)
	direct = modelFromTea(updated)
	require.NotNil(t, direct.session)

	updated, command := direct.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	require.NotNil(t, updated)
	if command != nil {
		_ = command()
	}
	_ = cleanup()
	_ = cleanup()
	require.Eventually(t, func() bool { return adapter.CloseCount() == 1 }, time.Second, time.Millisecond)
}

func TestRuntimeErrorFallbackCancelsEffectsAndClosesSession(t *testing.T) {
	readStarted := make(chan struct{})
	readCanceled := make(chan struct{})
	adapter := &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(ctx context.Context) (domain.NodeSet, error) {
		close(readStarted)
		<-ctx.Done()
		close(readCanceled)
		return domain.NodeSet{}, ctx.Err()
	}}}
	client := application.NewClient(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
			return adapter, nil
		}},
	})
	model, cleanup := NewWithCleanup(context.Background(), client, LaunchOptions{})
	program := tea.NewProgram(model, tea.WithInput(runtimeFailureReader{ready: readStarted}), tea.WithOutput(io.Discard), tea.WithWindowSize(120, 40), tea.WithoutSignalHandler())
	_, err := program.Run()
	require.Error(t, err, "Bubble Tea must return the injected runtime input error")
	// This is the same public fallback command the CLI runs after Program.Run.
	cleanup()
	require.Eventually(t, func() bool { return adapter.CloseCount() == 1 }, time.Second, time.Millisecond)
	select {
	case <-readCanceled:
	case <-time.After(time.Second):
		t.Fatal("fallback cleanup did not cancel the active blocked node read")
	}
}

type runtimeFailureReader struct{ ready <-chan struct{} }

func (r runtimeFailureReader) Read([]byte) (int, error) {
	<-r.ready
	return 0, errors.New("injected terminal input failure")
}
