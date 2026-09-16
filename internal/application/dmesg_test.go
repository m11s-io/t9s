package application_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenDmesgStartsStreamingAndLoadsBatches(t *testing.T) {
	stream := &testkit.FakeDmesgStream{Batches: []domain.DmesgBatch{{Lines: []string{"one", "two"}}, {EOF: true}}}
	reader := &testkit.FakeDmesgReader{OpenFunc: func(context.Context, domain.DmesgRequest) (ports.DmesgStream, error) {
		return stream, nil
	}}
	model := application.Model{Generation: 7}
	model = withDmesgReader(model, reader)

	model, effect := application.Update(model, application.OpenDmesg{Request: domain.DmesgRequest{Node: "cp-1", Follow: true, Tail: true}})
	assert.Equal(t, application.Loading, model.Dmesg.Status)
	assert.Equal(t, "cp-1", model.Dmesg.Request.Node)
	assert.True(t, model.Dmesg.Following)
	require.NotNil(t, effect)

	runner := application.NewRunner(application.Dependencies{})
	opened := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, opened)
	require.NotNil(t, effect, "opening the stream must arm the first receive")

	batch := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, batch)
	assert.Equal(t, []string{"one", "two"}, model.Dmesg.Lines)
	require.NotNil(t, effect, "the next receive is armed only after the prior batch is reduced")

	eof := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, eof)
	assert.Nil(t, effect)
	assert.True(t, model.Dmesg.EOF)
}

func TestDmesgBatchLoadedAppendsAndTrimsTo2000Lines(t *testing.T) {
	lines := make([]string, 2200)
	for index := range lines {
		lines[index] = fmt.Sprintf("line-%04d", index)
	}
	model := application.Model{Generation: 3}

	model, _ = application.Update(model, application.DmesgBatchLoaded{
		Generation: 3, StreamGeneration: 1, Batch: domain.DmesgBatch{Lines: lines},
	})

	require.Len(t, model.Dmesg.Lines, 2000)
	assert.Equal(t, "line-0200", model.Dmesg.Lines[0])
}

func TestCloseDmesgClearsStateAndClosesStream(t *testing.T) {
	stream := &testkit.FakeDmesgStream{}
	reader := &testkit.FakeDmesgReader{OpenFunc: func(context.Context, domain.DmesgRequest) (ports.DmesgStream, error) {
		return stream, nil
	}}
	model := application.Model{Generation: 7}
	model = withDmesgReader(model, reader)
	runner := application.NewRunner(application.Dependencies{})

	model, effect := application.Update(model, application.OpenDmesg{Request: domain.DmesgRequest{Node: "cp-1", Follow: true, Tail: true}})
	opened := runner.Run(context.Background(), effect)
	model, _ = application.Update(model, opened)

	model, effect = application.Update(model, application.CloseDmesg{})
	require.NotNil(t, effect)
	assert.Equal(t, application.DmesgState{}, model.Dmesg)
	assert.False(t, stream.Closed())

	_ = runner.Run(context.Background(), effect)
	assert.True(t, stream.Closed())
}

func TestSelectContextResetsDmesg(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", Dmesg: application.DmesgState{
		Status:  application.Ready,
		Request: domain.DmesgRequest{Node: "cp-1", Follow: true, Tail: true},
		Lines:   []string{"x"},
	}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, application.DmesgState{}, model.Dmesg)
}

// withDmesgReader seeds a Model's unexported dmesgReader field via the
// SessionOpened message, since application.Model has no exported setter and
// this test file is package application_test (no access to unexported fields).
func withDmesgReader(model application.Model, reader ports.DmesgReader) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Dmesg: reader})
	return model
}
