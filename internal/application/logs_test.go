package application_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceLogLifecycleReadsOneBatchAtATimeAndCloses(t *testing.T) {
	stream := &fakeApplicationLogStream{batches: []domain.LogBatch{{Lines: []string{"one", "two"}}, {EOF: true}}}
	reader := &fakeApplicationLogReader{stream: stream}
	model := application.Model{Generation: 7}
	model, _ = application.Update(model, application.SessionOpened{
		Generation: 7,
		Nodes:      &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) { return domain.NodeSet{}, nil }},
		Logs:       reader,
	})

	request := domain.LogRequest{Node: "cp-1", Service: "etcd"}
	model, effect := application.Update(model, application.OpenServiceLogs{Request: request})
	assert.Equal(t, application.Loading, model.Logs.Status)
	require.NotNil(t, effect)
	assert.Zero(t, stream.nextCount())

	runner := application.NewRunner(application.Dependencies{})
	opened := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, opened)
	require.NotNil(t, effect)
	assert.Zero(t, stream.nextCount())

	batch := runner.Run(context.Background(), effect)
	assert.Equal(t, 1, stream.nextCount())
	model, effect = application.Update(model, batch)
	assert.Equal(t, []string{"one", "two"}, model.Logs.Lines)
	require.NotNil(t, effect, "the next receive is armed only after the prior batch is reduced")

	eof := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, eof)
	assert.Nil(t, effect)
	assert.True(t, model.Logs.EOF)

	model, effect = application.Update(model, application.CloseServiceLogs{})
	require.NotNil(t, effect)
	assert.False(t, stream.closed)
	_ = runner.Run(context.Background(), effect)
	assert.True(t, stream.closed)
}

func TestServiceLogsRetainBoundedLinesAndExistingLinesOnFailure(t *testing.T) {
	lines := make([]string, 2100)
	for index := range lines {
		lines[index] = fmt.Sprintf("line-%04d", index)
	}
	model := application.Model{Generation: 3, Logs: application.LogState{Status: application.Ready, Lines: []string{"old"}}}

	model, _ = application.Update(model, application.ServiceLogBatchLoaded{
		Generation: 3, StreamGeneration: 1, Batch: domain.LogBatch{Lines: lines},
	})
	assert.Len(t, model.Logs.Lines, 2000)
	assert.Equal(t, "line-0100", model.Logs.Lines[0])

	model, effect := application.Update(model, application.ServiceLogBatchLoaded{
		Generation: 3, StreamGeneration: 1, Err: errors.New("token=top-secret receive failed"),
	})
	assert.Nil(t, effect)
	assert.Len(t, model.Logs.Lines, 2000)
	assert.Equal(t, "log stream unavailable", model.Logs.Err)
	assert.NotContains(t, model.Logs.Err, "top-secret")
}

func TestStaleServiceLogBatchIsIgnored(t *testing.T) {
	model := application.Model{Generation: 5, Logs: application.LogState{Status: application.Ready, Lines: []string{"current"}}}

	updated, effect := application.Update(model, application.ServiceLogBatchLoaded{
		Generation: 4, StreamGeneration: 1, Batch: domain.LogBatch{Lines: []string{"stale"}},
	})

	assert.Nil(t, effect)
	assert.Equal(t, model, updated)
}

type fakeApplicationLogReader struct {
	stream ports.ServiceLogStream
}

func (f *fakeApplicationLogReader) Open(context.Context, domain.LogRequest) (ports.ServiceLogStream, error) {
	return f.stream, nil
}

type fakeApplicationLogStream struct {
	mu      sync.Mutex
	batches []domain.LogBatch
	index   int
	closed  bool
}

func (f *fakeApplicationLogStream) Next(context.Context) (domain.LogBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.index >= len(f.batches) {
		return domain.LogBatch{EOF: true}, nil
	}
	batch := f.batches[f.index]
	f.index++
	return batch, nil
}

func (f *fakeApplicationLogStream) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeApplicationLogStream) nextCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.index
}
