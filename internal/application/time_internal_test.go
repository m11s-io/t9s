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
				reader := (managedSession{time: underlying, ctx: sessionCtx}).Time()
				done := make(chan error, 1)
				go func() {
					if kind == "read" {
						_, err := reader.Read(callCtx, "cp-1")
						done <- err
					} else {
						_, err := reader.Check(callCtx, "cp-1", "ntp.example.org")
						done <- err
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
