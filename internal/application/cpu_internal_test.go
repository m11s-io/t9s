package application

import (
	"context"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestCPUSessionAndCallCancellationReachSample(t *testing.T) {
	for _, parent := range []string{"session", "call"} {
		t.Run(parent, func(t *testing.T) {
			sessionCtx, sessionCancel := context.WithCancel(t.Context())
			defer sessionCancel()
			callCtx, callCancel := context.WithCancel(t.Context())
			defer callCancel()
			started := make(chan struct{})
			reader := (managedSession{cpu: &testkit.FakeCPUReader{SampleFunc: func(ctx context.Context, _ string) (domain.CPUSnapshot, error) {
				close(started)
				<-ctx.Done()
				return domain.CPUSnapshot{}, ctx.Err()
			}}, ctx: sessionCtx}).CPU()
			done := make(chan error, 1)
			go func() { _, err := reader.Sample(callCtx, "cp-1"); done <- err }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("sample did not start")
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
				t.Fatal("sample did not cancel")
			}
		})
	}
}
