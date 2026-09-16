package talos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// etcdSnapshotChecksumSize is sha256.Size: the etcd snapshot wire format is a
// payload that is a multiple of 512 bytes followed by a 32-byte sha256 trailer
// over that payload.
const etcdSnapshotChecksumSize = sha256.Size

type etcdOperations struct{ client etcdClient }

func newEtcdOperations(client etcdClient) ports.EtcdOperations {
	return &etcdOperations{client: client}
}

// Snapshot streams a snapshot from a single node to path. It writes to
// path+".part" with O_EXCL so it can never clobber an existing staging file,
// refuses to overwrite an existing final file, verifies the sha256 checksum
// trailer actually matches the payload, and only then commits the staging
// file with a hard link. Any failure leaves no final file and no .part file
// behind. A completed snapshot is never deleted automatically.
func (o *etcdOperations) Snapshot(ctx context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		if err == nil {
			return domain.EtcdSnapshotResult{}, fmt.Errorf("refusing to overwrite existing snapshot %q", path)
		}
		return domain.EtcdSnapshotResult{}, fmt.Errorf("check snapshot destination %s: %w", path, err)
	}

	part := path + ".part"
	file, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return domain.EtcdSnapshotResult{}, fmt.Errorf("create snapshot staging file %s: %w", part, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = file.Close()
			_ = os.Remove(part)
		}
	}()

	stream, err := o.client.EtcdSnapshot(ctx, node, &machineapi.EtcdSnapshotRequest{})
	if err != nil {
		return domain.EtcdSnapshotResult{}, fmt.Errorf("snapshot etcd on %s: %w", node, err)
	}
	defer stream.Close()

	hasher := newSnapshotHasher()
	size, err := io.Copy(io.MultiWriter(file, hasher), contextReader{ctx: ctx, reader: stream})
	if err != nil {
		return domain.EtcdSnapshotResult{}, fmt.Errorf("stream snapshot from %s: %w", node, err)
	}
	if len(hasher.trailer) != etcdSnapshotChecksumSize {
		return domain.EtcdSnapshotResult{}, fmt.Errorf("snapshot payload %d bytes is too short to contain a %d-byte checksum trailer", size, etcdSnapshotChecksumSize)
	}
	if !bytes.Equal(hasher.digest(), hasher.trailer) {
		return domain.EtcdSnapshotResult{}, fmt.Errorf("snapshot checksum mismatch: payload digest does not match its %d-byte trailer", etcdSnapshotChecksumSize)
	}
	if err := file.Sync(); err != nil {
		return domain.EtcdSnapshotResult{}, fmt.Errorf("sync snapshot staging file %s: %w", part, err)
	}
	if err := file.Close(); err != nil {
		return domain.EtcdSnapshotResult{}, fmt.Errorf("close snapshot staging file %s: %w", part, err)
	}
	// Link (not rename) so the no-overwrite guarantee is atomic: Link fails
	// with EEXIST if a file appeared at path after the Stat above, instead of
	// silently replacing it.
	if err := os.Link(part, path); err != nil {
		return domain.EtcdSnapshotResult{}, fmt.Errorf("finalize snapshot %s: %w", path, err)
	}
	committed = true
	_ = os.Remove(part)
	syncDir(filepath.Dir(path))

	return domain.EtcdSnapshotResult{
		Node:   node,
		Path:   path,
		Size:   size,
		SHA256: hex.EncodeToString(hasher.trailer),
	}, nil
}

// snapshotHasher computes sha256 over a stream while withholding its final
// etcdSnapshotChecksumSize bytes, so the digest can be compared against the
// trailing checksum without buffering the whole snapshot.
type snapshotHasher struct {
	hash    hash.Hash
	trailer []byte
}

func newSnapshotHasher() *snapshotHasher { return &snapshotHasher{hash: sha256.New()} }

func (h *snapshotHasher) Write(p []byte) (int, error) {
	h.trailer = append(h.trailer, p...)
	if excess := len(h.trailer) - etcdSnapshotChecksumSize; excess > 0 {
		if _, err := h.hash.Write(h.trailer[:excess]); err != nil {
			return 0, err
		}
		h.trailer = append(h.trailer[:0], h.trailer[excess:]...)
	}

	return len(p), nil
}

func (h *snapshotHasher) digest() []byte { return h.hash.Sum(nil) }

// syncDir best-effort flushes the directory entry after a successful commit so
// a crash immediately after reporting success does not lose the new file.
func syncDir(dir string) {
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
}

// RemoveMemberByID forcibly removes a member by numeric ID. It is irreversible
// membership surgery, so callers must snapshot the target first.
func (o *etcdOperations) RemoveMemberByID(ctx context.Context, node string, memberID uint64) error {
	if err := o.client.EtcdRemoveMemberByID(ctx, node, &machineapi.EtcdRemoveMemberByIDRequest{MemberId: memberID}); err != nil {
		return fmt.Errorf("remove etcd member %d via %s: %w", memberID, node, err)
	}

	return nil
}

// LeaveCluster makes the member reached at node leave its cluster gracefully.
// The RPC is executed by the member itself, so node must be that member's own
// node.
func (o *etcdOperations) LeaveCluster(ctx context.Context, node string) error {
	if err := o.client.EtcdLeaveCluster(ctx, node, &machineapi.EtcdLeaveClusterRequest{}); err != nil {
		return fmt.Errorf("etcd leave cluster on %s: %w", node, err)
	}

	return nil
}

func (o *etcdOperations) Defragment(ctx context.Context, node string) error {
	if _, err := o.client.EtcdDefragment(ctx, node); err != nil {
		return fmt.Errorf("defragment etcd on %s: %w", node, err)
	}
	return nil
}

func (o *etcdOperations) DisarmAlarms(ctx context.Context, node string) error {
	if _, err := o.client.EtcdAlarmDisarm(ctx, node); err != nil {
		return fmt.Errorf("disarm etcd alarms on %s: %w", node, err)
	}
	return nil
}

// contextReader aborts an in-flight copy the moment ctx is canceled, so a
// session teardown cancels a multi-gigabyte snapshot promptly and the defer
// above removes the .part file.
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
