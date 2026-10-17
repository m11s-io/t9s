package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type timeTestReader struct {
	read  func(context.Context, string) (domain.TimeSnapshot, error)
	check func(context.Context, string, string) (domain.TimeCheckResult, error)
}

func (r timeTestReader) Read(c context.Context, n string) (domain.TimeSnapshot, error) {
	return r.read(c, n)
}
func (r timeTestReader) Check(c context.Context, n, s string) (domain.TimeCheckResult, error) {
	return r.check(c, n, s)
}
func TestTimeCheckPermissionDeniedPreservesStatus(t *testing.T) {
	prior := domain.TimeSnapshot{SyncKnown: true, Synced: true}
	calls := 0
	reader := timeTestReader{check: func(_ context.Context, n, s string) (domain.TimeCheckResult, error) {
		calls++
		assert.Equal(t, "cp-1", n)
		assert.Equal(t, "ntp.example.org", s)
		return domain.TimeCheckResult{}, ports.ErrTimePermissionDenied
	}}
	m := application.Model{Generation: 1, Time: application.TimeState{Node: "cp-1", Status: application.Ready, Value: prior}}
	m, _ = application.Update(m, application.SessionOpened{Generation: 1, Time: reader})
	m, e := application.Update(m, application.CheckTime{Server: "ntp.example.org"})
	require.NotNil(t, e)
	m, next := application.Update(m, e(t.Context(), application.Dependencies{}))
	assert.Nil(t, next)
	assert.Equal(t, prior, m.Time.Value)
	assert.Equal(t, application.Ready, m.Time.Status)
	assert.Equal(t, application.Failed, m.Time.CheckStatus)
	assert.Equal(t, "time check permission denied", m.Time.CheckErr)
	assert.Equal(t, 1, calls)
}
func TestTimeLateNodeResultIgnored(t *testing.T) {
	reader := timeTestReader{read: func(_ context.Context, n string) (domain.TimeSnapshot, error) {
		return domain.TimeSnapshot{TimeSample: domain.TimeSample{Server: n}}, nil
	}}
	m := application.Model{Generation: 1}
	m, _ = application.Update(m, application.SessionOpened{Generation: 1, Time: reader})
	m, e1 := application.Update(m, application.OpenTime{Node: "cp-1"})
	require.NotNil(t, e1)
	m, e2 := application.Update(m, application.OpenTime{Node: "cp-2"})
	require.NotNil(t, e2)
	m, _ = application.Update(m, e2(t.Context(), application.Dependencies{}))
	m, _ = application.Update(m, e1(t.Context(), application.Dependencies{}))
	assert.Equal(t, "cp-2", m.Time.Value.Server)
}

// The older result is completed before refresh, but delivered afterwards. This
// catches stale delivery even when cancellation cannot prevent RPC completion.
func TestTimeRefreshRejectsOlderSameNodeResult(t *testing.T) {
	reader := timeTestReader{read: func(context.Context, string) (domain.TimeSnapshot, error) {
		return domain.TimeSnapshot{TimeSample: domain.TimeSample{Server: "old.example.org"}}, nil
	}}
	m := application.Model{Generation: 7}
	m, _ = application.Update(m, application.SessionOpened{Generation: 7, Time: reader})
	m, first := application.Update(m, application.OpenTime{Node: "cp-1"})
	old := first(t.Context(), application.Dependencies{})
	m, second := application.Update(m, application.RefreshTime{})
	fresh := second(t.Context(), application.Dependencies{}).(application.TimeLoaded)
	fresh.Value.Server = "new.example.org"
	m, _ = application.Update(m, old)
	assert.Equal(t, application.Loading, m.Time.Status)
	wrongContext := fresh
	wrongContext.Generation = 6
	m, _ = application.Update(m, wrongContext)
	assert.Equal(t, application.Loading, m.Time.Status)
	m, _ = application.Update(m, fresh)
	assert.Equal(t, "new.example.org", m.Time.Value.Server)
}
func TestTimeCheckAndStatusHaveSeparateRequestIdentities(t *testing.T) {
	reader := timeTestReader{
		read: func(context.Context, string) (domain.TimeSnapshot, error) {
			return domain.TimeSnapshot{SyncKnown: true, Synced: true}, nil
		},
		check: func(context.Context, string, string) (domain.TimeCheckResult, error) {
			return domain.TimeCheckResult{TimeSample: domain.TimeSample{DifferenceValid: true}}, nil
		},
	}
	m := application.Model{Generation: 1}
	m, _ = application.Update(m, application.SessionOpened{Generation: 1, Time: reader})
	m, read := application.Update(m, application.OpenTime{Node: "cp-1"})
	m, check1 := application.Update(m, application.CheckTime{Server: "ntp.example.org"})
	old := check1(t.Context(), application.Dependencies{})
	m, check2 := application.Update(m, application.CheckTime{Server: "ntp.example.org"})
	m, _ = application.Update(m, read(t.Context(), application.Dependencies{}))
	assert.True(t, m.Time.Value.Synced)
	m, _ = application.Update(m, old)
	assert.Equal(t, application.Loading, m.Time.CheckStatus)
	assert.Equal(t, "ntp.example.org", m.Time.Check.Server)
	m, _ = application.Update(m, check2(t.Context(), application.Dependencies{}))
	assert.Equal(t, application.Ready, m.Time.CheckStatus)
	m, refresh := application.Update(m, application.RefreshTime{})
	require.NotNil(t, refresh)
	assert.Equal(t, application.Ready, m.Time.CheckStatus)
	m, _ = application.Update(m, refresh(t.Context(), application.Dependencies{}))
	assert.Equal(t, "ntp.example.org", m.Time.Check.Server)
}
func TestTimeRequestsCancelOnCloseAndContextChange(t *testing.T) {
	for _, closeMessage := range []application.Message{application.CloseTime{}, application.SelectContext{Name: "other"}} {
		readStarted, checkStarted := make(chan struct{}), make(chan struct{})
		reader := timeTestReader{
			read: func(ctx context.Context, _ string) (domain.TimeSnapshot, error) {
				close(readStarted)
				<-ctx.Done()
				return domain.TimeSnapshot{}, ctx.Err()
			},
			check: func(ctx context.Context, _, _ string) (domain.TimeCheckResult, error) {
				close(checkStarted)
				<-ctx.Done()
				return domain.TimeCheckResult{}, ctx.Err()
			},
		}
		m := application.Model{Generation: 1}
		m, _ = application.Update(m, application.SessionOpened{Generation: 1, Time: reader})
		m, read := application.Update(m, application.OpenTime{Node: "cp-1"})
		m, check := application.Update(m, application.CheckTime{Server: "ntp.example.org"})
		results := make(chan application.Message, 2)
		go func() { results <- read(t.Context(), application.Dependencies{}) }()
		go func() { results <- check(t.Context(), application.Dependencies{}) }()
		<-readStarted
		<-checkStarted
		m, _ = application.Update(m, closeMessage)
		for range 2 {
			select {
			case msg := <-results:
				m, _ = application.Update(m, msg)
			case <-time.After(time.Second):
				t.Fatal("time request did not cancel")
			}
		}
		assert.Equal(t, application.TimeState{}, m.Time)
	}
}
func TestTimePartialAndCanceledResultsAreNotReady(t *testing.T) {
	for _, readErr := range []error{nil, context.Canceled} {
		reader := timeTestReader{read: func(context.Context, string) (domain.TimeSnapshot, error) {
			return domain.TimeSnapshot{SyncKnown: true, Synced: true, TimeSample: domain.TimeSample{Problems: []string{"measurement unavailable"}}}, readErr
		}}
		m := application.Model{Generation: 1}
		m, _ = application.Update(m, application.SessionOpened{Generation: 1, Time: reader})
		m, e := application.Update(m, application.OpenTime{Node: "cp-1"})
		m, _ = application.Update(m, e(t.Context(), application.Dependencies{}))
		assert.NotEqual(t, application.Ready, m.Time.Status)
	}
}
