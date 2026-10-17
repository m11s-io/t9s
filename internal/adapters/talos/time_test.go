package talos

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/ports"
	timeapi "github.com/siderolabs/talos/pkg/machinery/api/time"
	timeresource "github.com/siderolabs/talos/pkg/machinery/resources/time"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeTimeClient struct {
	response                     *timeapi.TimeResponse
	readErr, statusErr, checkErr error
	reads, checks, statuses      int
}

func (f *fakeTimeClient) Time(_ context.Context, node string) (*timeapi.TimeResponse, error) {
	f.reads++
	if node != "cp-1" {
		panic("wrong node")
	}
	return f.response, f.readErr
}
func (f *fakeTimeClient) TimeCheck(_ context.Context, node, server string) (*timeapi.TimeResponse, error) {
	f.checks++
	if node != "cp-1" || server != "ntp.example.org" {
		panic("wrong target")
	}
	return f.response, f.checkErr
}
func (f *fakeTimeClient) TimeStatus(_ context.Context, node string) (*timeresource.Status, error) {
	f.statuses++
	if node != "cp-1" {
		panic("wrong node")
	}
	s := timeresource.NewStatus()
	s.TypedSpec().Synced = true
	return s, f.statusErr
}
func TestTimeReadPreservesStatusWhenConfiguredServerFails(t *testing.T) {
	f := &fakeTimeClient{readErr: errors.New("configured server unavailable")}
	got, err := newTimeReader(f).Read(t.Context(), "cp-1")
	require.NoError(t, err)
	assert.True(t, got.SyncKnown)
	assert.True(t, got.Synced)
	assert.False(t, got.DifferenceValid)
	assert.NotEmpty(t, got.Problems)
	assert.Equal(t, 0, f.checks)
}
func TestTimeSampleDifference(t *testing.T) {
	local := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	f := &fakeTimeClient{response: &timeapi.TimeResponse{Messages: []*timeapi.Time{{Server: "ntp.example.org", Localtime: timestamppb.New(local), Remotetime: timestamppb.New(local.Add(250 * time.Millisecond))}}}}
	got, err := newTimeReader(f).Read(t.Context(), "cp-1")
	require.NoError(t, err)
	assert.True(t, got.DifferenceValid)
	assert.Equal(t, 250*time.Millisecond, got.Difference)
	assert.Equal(t, local, got.LocalTime)
	assert.True(t, got.Synced)
	check, err := newTimeReader(f).Check(t.Context(), "cp-1", "ntp.example.org")
	require.NoError(t, err)
	assert.Equal(t, 250*time.Millisecond, check.Difference)
	assert.Equal(t, 1, f.checks)
}
func TestTimeRejectsInvalidTimestamps(t *testing.T) {
	for _, ts := range []*timestamppb.Timestamp{nil, {Seconds: 253402300800}, {Nanos: -1}} {
		f := &fakeTimeClient{response: &timeapi.TimeResponse{Messages: []*timeapi.Time{{Localtime: ts, Remotetime: timestamppb.Now()}}}}
		got, err := newTimeReader(f).Read(t.Context(), "cp-1")
		require.NoError(t, err)
		assert.False(t, got.LocalTimeValid)
		assert.True(t, got.RemoteTimeValid)
		assert.False(t, got.DifferenceValid)
		assert.NotEmpty(t, got.Problems)
	}
}
func TestTimeCheckNormalizesPermissionDenied(t *testing.T) {
	f := &fakeTimeClient{checkErr: status.Error(codes.PermissionDenied, "sensitive server detail")}
	_, err := newTimeReader(f).Check(t.Context(), "cp-1", "ntp.example.org")
	require.ErrorIs(t, err, ports.ErrTimePermissionDenied)
	assert.NotContains(t, err.Error(), "sensitive")
	assert.Equal(t, 1, f.checks)
	assert.Zero(t, f.reads)
	assert.Zero(t, f.statuses)
}

func TestTimeStatusFailureDoesNotDiscardConfiguredServerSample(t *testing.T) {
	local := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	f := &fakeTimeClient{statusErr: errors.New("COSI unavailable"), response: &timeapi.TimeResponse{Messages: []*timeapi.Time{{Server: "ntp.example.org", Localtime: timestamppb.New(local), Remotetime: timestamppb.New(local.Add(-250 * time.Millisecond))}}}}
	got, err := newTimeReader(f).Read(t.Context(), "cp-1")
	require.NoError(t, err)
	assert.False(t, got.SyncKnown)
	assert.True(t, got.DifferenceValid)
	assert.Equal(t, -250*time.Millisecond, got.Difference)
	assert.NotEmpty(t, got.Problems)
}
func TestTimeMissingAndFailedMessagesRemainUnknown(t *testing.T) {
	for _, response := range []*timeapi.TimeResponse{nil, {}, {Messages: []*timeapi.Time{nil}}, {Messages: []*timeapi.Time{{}}}} {
		f := &fakeTimeClient{response: response}
		got, err := newTimeReader(f).Read(t.Context(), "cp-1")
		require.NoError(t, err)
		assert.True(t, got.SyncKnown)
		assert.False(t, got.LocalTimeValid)
		assert.False(t, got.RemoteTimeValid)
		assert.False(t, got.DifferenceValid)
		assert.NotEmpty(t, got.Problems)
	}
}
func TestTimeCanceledReadNeverReturnsHealthyStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := newTimeReader(&fakeTimeClient{}).Read(ctx, "cp-1")
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, got.SyncKnown)
}
func TestTimeTimestampRangeDoesNotProduceSaturatedDifference(t *testing.T) {
	f := &fakeTimeClient{response: &timeapi.TimeResponse{Messages: []*timeapi.Time{{Localtime: &timestamppb.Timestamp{Seconds: -62135596800}, Remotetime: &timestamppb.Timestamp{Seconds: 253402300799}}}}}
	got, err := newTimeReader(f).Read(t.Context(), "cp-1")
	require.NoError(t, err)
	assert.True(t, got.LocalTimeValid)
	assert.True(t, got.RemoteTimeValid)
	assert.False(t, got.DifferenceValid)
	assert.NotEmpty(t, got.Problems)
}
