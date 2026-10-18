package application

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestOwnerReplacementCancelsPriorRequest(t *testing.T) {
	owner := newRequestOwner(t.Context())
	defer owner.close()
	first, finishFirst, err := owner.begin(t.Context(), "nodes", 1, time.Second)
	require.NoError(t, err)
	second, finishSecond, err := owner.begin(t.Context(), "nodes", 2, time.Second)
	require.NoError(t, err)
	defer finishSecond()
	require.ErrorIs(t, first.Err(), context.Canceled)
	finishFirst()
	require.NoError(t, second.Err(), "completion of the replaced request must not cancel its successor")
	_, _, err = owner.begin(t.Context(), "nodes", 1, time.Second)
	require.Error(t, err)
	_, _, err = owner.begin(t.Context(), "nodes", 2, time.Second)
	require.Error(t, err)
}

func TestRequestOwnerRetainsCompletedRequestID(t *testing.T) {
	owner := newRequestOwner(t.Context())
	defer owner.close()
	ctx, finish, err := owner.begin(t.Context(), "nodes", 7, time.Second)
	require.NoError(t, err)
	finish()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	_, _, err = owner.begin(t.Context(), "nodes", 7, time.Second)
	require.Error(t, err)
}

func TestRequestOwnerCancellationAndClose(t *testing.T) {
	owner := newRequestOwner(t.Context())
	first, _, err := owner.begin(t.Context(), "logs", 1, 0)
	require.NoError(t, err)
	_, deadline := first.Deadline()
	require.False(t, deadline, "stream lifetime must have no added deadline")
	second, _, err := owner.begin(t.Context(), "nodes", 2, time.Second)
	require.NoError(t, err)
	owner.cancelThrough("logs", 1)
	require.ErrorIs(t, first.Err(), context.Canceled)
	require.NoError(t, second.Err())
	owner.close()
	owner.close()
	require.ErrorIs(t, second.Err(), context.Canceled)
	_, _, err = owner.begin(t.Context(), "other", 3, time.Second)
	require.Error(t, err)
}

func TestRequestOwnerRejectsCanceledContextsBeforeDispatch(t *testing.T) {
	session, cancelSession := context.WithCancel(t.Context())
	owner := newRequestOwner(session)
	defer owner.close()
	caller, cancelCaller := context.WithCancel(t.Context())
	cancelCaller()
	_, _, err := owner.begin(caller, "nodes", 1, time.Second)
	require.ErrorIs(t, err, context.Canceled)
	cancelSession()
	_, _, err = owner.begin(t.Context(), "nodes", 2, time.Second)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRequestOwnerPropagatesCancellationAndDeadline(t *testing.T) {
	session, cancelSession := context.WithCancel(t.Context())
	owner := newRequestOwner(session)
	defer owner.close()
	ctx, finish, err := owner.begin(t.Context(), "nodes", 1, 15*time.Second)
	require.NoError(t, err)
	defer finish()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(15*time.Second), deadline, time.Second)
	cancelSession()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("session cancellation did not reach request")
	}
	caller, cancelCaller := context.WithCancel(t.Context())
	otherOwner := newRequestOwner(t.Context())
	defer otherOwner.close()
	other, finishOther, err := otherOwner.begin(caller, "nodes", 1, 0)
	require.NoError(t, err)
	defer finishOther()
	cancelCaller()
	require.ErrorIs(t, other.Err(), context.Canceled)
}
