package cli_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunReturnsTwoForUnknownFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := cli.Run(t.Context(), []string{"--unknown"}, strings.NewReader(""), &stdout, &stderr)

	assert.Equal(t, 2, exitCode)
	assert.Contains(t, stderr.String(), "unknown flag")
}

func TestRunRendersMissingTalosconfigAndQuitsNormally(t *testing.T) {
	const failure = "load talosconfig"
	stdout := newSignalWriter(failure)
	stdin := &signalReader{signal: stdout.matched, contents: []byte("q")}
	var stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	exitCode := cli.Run(ctx, []string{
		"--talosconfig", "/path/that/does/not/exist/talosconfig",
		"--talosconfig", "/path/that/does/not/exist/second",
		"--context", "prod",
		"--node", "10.0.0.2",
	}, stdin, stdout, &stderr)

	require.NoError(t, ctx.Err())
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), failure)
	assert.NotContains(t, stderr.String(), failure)
}

func TestRunCallerCancellationQuitsNormally(t *testing.T) {
	const failure = "load talosconfig"
	stdout := newSignalWriter(failure)
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan int, 1)
	go func() {
		result <- cli.Run(ctx, []string{
			"--talosconfig", "/path/that/does/not/exist/talosconfig",
		}, nil, stdout, &stderr)
	}()

	select {
	case <-stdout.matched:
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for rendered configuration failure")
	}
	cancel()

	select {
	case exitCode := <-result:
		assert.Equal(t, 0, exitCode)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for caller cancellation shutdown")
	}
}

type signalWriter struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	needle  string
	matched chan struct{}
	once    sync.Once
}

func newSignalWriter(needle string) *signalWriter {
	return &signalWriter{needle: needle, matched: make(chan struct{})}
}

func (w *signalWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	n, err := w.buffer.Write(data)
	matched := strings.Contains(w.buffer.String(), w.needle)
	w.mu.Unlock()
	if matched {
		w.once.Do(func() { close(w.matched) })
	}

	return n, err
}

func (w *signalWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buffer.String()
}

type signalReader struct {
	signal   <-chan struct{}
	contents []byte
	sent     bool
}

func (r *signalReader) Read(destination []byte) (int, error) {
	if r.sent {
		return 0, io.EOF
	}
	select {
	case <-r.signal:
		r.sent = true
		return copy(destination, r.contents), nil
	case <-time.After(4 * time.Second):
		return 0, context.DeadlineExceeded
	}
}
