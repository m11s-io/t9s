package application

import (
	"context"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestTimeSessionAndCallCancellationReachBothRPCs(t *testing.T) {
	for _, kind := range []string{"read", "check"} {
		for _, parent := range []string{"session", "call"} {
			t.Run(kind+"/"+parent, func(t *testing.T) {
				sessionCtx, sessionCancel := context.WithCancel(t.Context())
				defer sessionCancel()
				callCtx, callCancel := context.WithCancel(t.Context())
				defer callCancel()
				started := make(chan struct{})
				underlying := &testkit.FakeTimeReader{
					ReadFunc: func(ctx context.Context, _ string) (domain.TimeSnapshot, error) {
						close(started)
						<-ctx.Done()
						return domain.TimeSnapshot{}, ctx.Err()
					},
					CheckFunc: func(ctx context.Context, _, _ string) (domain.TimeCheckResult, error) {
						close(started)
						<-ctx.Done()
						return domain.TimeCheckResult{}, ctx.Err()
					},
				}
				model := Model{Generation: 1, sessionRuntime: sessionRuntime{timeReader: underlying, requests: newRequestOwner(sessionCtx)}}
				model, effect := Update(model, OpenTime{Node: "cp-1"})
				if kind == "check" {
					_, effect = Update(model, CheckTime{Server: "ntp.example.org"})
				}
				done := make(chan error, 1)
				go func() {
					if kind == "read" {
						message := effect(callCtx, Dependencies{}).(TimeLoaded)
						done <- message.Err
					} else {
						message := effect(callCtx, Dependencies{}).(TimeChecked)
						done <- message.Err
					}
				}()
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("RPC did not start")
				}
				if parent == "session" {
					sessionCancel()
				} else {
					callCancel()
				}
				select {
				case err := <-done:
					require.ErrorIs(t, err, context.Canceled)
				case <-time.After(time.Second):
					t.Fatal("RPC did not cancel")
				}
			})
		}
	}
}
