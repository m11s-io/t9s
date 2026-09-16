package talos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func snapshotFixture() []byte {
	// A valid etcd snapshot payload is a multiple of 512 bytes followed by a
	// 32-byte sha256 trailer over that payload: 512 + 32 = 544.
	body := make([]byte, 512)
	for index := range body {
		body[index] = byte(index % 251)
	}
	sum := sha256.Sum256(body)

	return append(body, sum[:]...)
}

func TestEtcdOperationsSnapshotWritesPartThenRenamesAtomically(t *testing.T) {
	body := snapshotFixture()
	client := &fakeEtcdClient{snapshotBytes: map[string][]byte{"cp-1": body}}
	operations := newEtcdOperations(client)
	path := filepath.Join(t.TempDir(), "snap.db")

	result, err := operations.Snapshot(t.Context(), "cp-1", path)

	require.NoError(t, err)
	assert.Equal(t, "cp-1", result.Node)
	assert.Equal(t, path, result.Path)
	assert.Equal(t, int64(544), result.Size)
	expected := sha256.Sum256(body[:len(body)-etcdSnapshotChecksumSize])
	assert.Equal(t, hex.EncodeToString(expected[:]), result.SHA256)

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Len(t, written, 544)
	_, statErr := os.Stat(path + ".part")
	assert.True(t, os.IsNotExist(statErr), "the .part staging file must be renamed away")
	assert.Equal(t, []string{"cp-1"}, client.snapshotNodes)
}

func TestEtcdOperationsSnapshotRejectsBadChecksumTrailer(t *testing.T) {
	// 500 bytes cannot even hold a 32-byte trailer after a 512-byte body.
	client := &fakeEtcdClient{snapshotBytes: map[string][]byte{"cp-1": make([]byte, 500)}}
	operations := newEtcdOperations(client)
	path := filepath.Join(t.TempDir(), "snap.db")

	_, err := operations.Snapshot(t.Context(), "cp-1", path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum")
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "a rejected snapshot must not leave a final file")
	_, partErr := os.Stat(path + ".part")
	assert.True(t, os.IsNotExist(partErr), "a rejected snapshot must clean up its .part file")
}

func TestEtcdOperationsSnapshotRejectsWrongChecksumWithValidLength(t *testing.T) {
	// 512-byte body + 32-byte trailer = 544 bytes: the length congruence
	// passes, but the trailer is not sha256(body), so it must be rejected.
	body := make([]byte, 512)
	for index := range body {
		body[index] = byte(index % 251)
	}
	corrupt := append(append([]byte(nil), body...), make([]byte, 32)...)
	client := &fakeEtcdClient{snapshotBytes: map[string][]byte{"cp-1": corrupt}}
	operations := newEtcdOperations(client)
	path := filepath.Join(t.TempDir(), "snap.db")

	_, err := operations.Snapshot(t.Context(), "cp-1", path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum")
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "a checksum mismatch must not leave a final file")
	_, partErr := os.Stat(path + ".part")
	assert.True(t, os.IsNotExist(partErr), "a checksum mismatch must clean up its .part file")
}

func TestEtcdOperationsSnapshotRefusesToOverwriteExistingFile(t *testing.T) {
	client := &fakeEtcdClient{snapshotBytes: map[string][]byte{"cp-1": snapshotFixture()}}
	operations := newEtcdOperations(client)
	path := filepath.Join(t.TempDir(), "snap.db")
	require.NoError(t, os.WriteFile(path, []byte("precious"), 0o600))

	_, err := operations.Snapshot(t.Context(), "cp-1", path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "overwrite")
	contents, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "precious", string(contents), "an existing final path must never be clobbered")
}

type errorAfterReader struct {
	remaining []byte
	err       error
}

func (r *errorAfterReader) Read(p []byte) (int, error) {
	if len(r.remaining) > 0 {
		n := copy(p, r.remaining)
		r.remaining = r.remaining[n:]
		return n, nil
	}
	return 0, r.err
}

func TestEtcdOperationsSnapshotCleansUpPartOnStreamError(t *testing.T) {
	client := &fakeEtcdClient{snapshotStream: func(string) (io.ReadCloser, error) {
		return io.NopCloser(&errorAfterReader{remaining: make([]byte, 512), err: errors.New("stream broke")}), nil
	}}
	operations := newEtcdOperations(client)
	path := filepath.Join(t.TempDir(), "snap.db")

	_, err := operations.Snapshot(t.Context(), "cp-1", path)

	require.Error(t, err)
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
	_, partErr := os.Stat(path + ".part")
	assert.True(t, os.IsNotExist(partErr), "a failed copy must remove its .part file")
}

type cancelAfterReader struct {
	cancel    context.CancelFunc
	remaining []byte
	fired     bool
}

func (r *cancelAfterReader) Read(p []byte) (int, error) {
	if !r.fired {
		r.fired = true
		n := copy(p, r.remaining)
		r.remaining = r.remaining[n:]
		r.cancel()

		return n, nil
	}

	return 0, io.EOF
}

func TestEtcdOperationsSnapshotCancelsMidCopyAndCleansUp(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	client := &fakeEtcdClient{snapshotStream: func(string) (io.ReadCloser, error) {
		return io.NopCloser(&cancelAfterReader{cancel: cancel, remaining: make([]byte, 4096)}), nil
	}}
	operations := newEtcdOperations(client)
	path := filepath.Join(t.TempDir(), "snap.db")

	_, err := operations.Snapshot(ctx, "cp-1", path)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
	_, partErr := os.Stat(path + ".part")
	assert.True(t, os.IsNotExist(partErr), "a cancelled copy must remove its .part file")
}

func TestEtcdOperationsSnapshotRefusesPreexistingStagingFile(t *testing.T) {
	client := &fakeEtcdClient{snapshotBytes: map[string][]byte{"cp-1": snapshotFixture()}}
	operations := newEtcdOperations(client)
	path := filepath.Join(t.TempDir(), "snap.db")
	require.NoError(t, os.WriteFile(path+".part", []byte("stale"), 0o600))

	_, err := operations.Snapshot(t.Context(), "cp-1", path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "staging")
	contents, readErr := os.ReadFile(path + ".part")
	require.NoError(t, readErr)
	assert.Equal(t, "stale", string(contents), "an existing staging file must never be clobbered")
}

func TestEtcdOperationsSnapshotIsSingleNode(t *testing.T) {
	client := &fakeEtcdClient{snapshotBytes: map[string][]byte{"cp-2": snapshotFixture()}}
	operations := newEtcdOperations(client)

	_, err := operations.Snapshot(t.Context(), "cp-2", filepath.Join(t.TempDir(), "snap.db"))

	require.NoError(t, err)
	assert.Equal(t, []string{"cp-2"}, client.snapshotNodes, "snapshot must be addressed to exactly one node")
}

func TestEtcdOperationsDefragmentCallsClient(t *testing.T) {
	client := &fakeEtcdClient{}
	operations := newEtcdOperations(client)

	require.NoError(t, operations.Defragment(t.Context(), "cp-1"))
	assert.Equal(t, []string{"cp-1"}, client.defragNodes)
}

func TestEtcdOperationsDefragmentWrapsError(t *testing.T) {
	client := &fakeEtcdClient{defragErr: errors.New("resource exhausted")}
	operations := newEtcdOperations(client)

	err := operations.Defragment(t.Context(), "cp-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
	assert.Contains(t, err.Error(), "resource exhausted")
}

func TestEtcdOperationsDisarmAlarmsCallsClient(t *testing.T) {
	client := &fakeEtcdClient{}
	operations := newEtcdOperations(client)

	require.NoError(t, operations.DisarmAlarms(t.Context(), "cp-1"))
	assert.Equal(t, []string{"cp-1"}, client.disarmNodes)
}
