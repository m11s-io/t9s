package ports

import (
	"context"
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubEtcdOperations struct{}

func (stubEtcdOperations) Snapshot(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
	return domain.EtcdSnapshotResult{Node: node, Path: path}, nil
}

func (stubEtcdOperations) Defragment(_ context.Context, _ string) error { return nil }

func (stubEtcdOperations) DisarmAlarms(_ context.Context, _ string) error { return nil }

func (stubEtcdOperations) RemoveMemberByID(_ context.Context, _ string, _ uint64) error { return nil }

func (stubEtcdOperations) LeaveCluster(_ context.Context, _ string) error { return nil }

var _ EtcdOperations = stubEtcdOperations{}

func TestEtcdOperationsSnapshotReturnsResult(t *testing.T) {
	var operations EtcdOperations = stubEtcdOperations{}

	result, err := operations.Snapshot(t.Context(), "cp-1", "/tmp/etcd.db")

	require.NoError(t, err)
	assert.Equal(t, "cp-1", result.Node)
	assert.Equal(t, "/tmp/etcd.db", result.Path)
}

func TestEtcdOperationsMembershipSurface(t *testing.T) {
	var operations EtcdOperations = stubEtcdOperations{}

	require.NoError(t, operations.RemoveMemberByID(t.Context(), "cp-2", 7))
	require.NoError(t, operations.LeaveCluster(t.Context(), "cp-1"))
}

// sessionStub implements every Session method so the compile-time assertion
// below fails until EtcdOperations() is added to the Session interface.
type sessionStub struct{}

func (sessionStub) Nodes() NodeReader                 { return nil }
func (sessionStub) NodeActions() NodeController       { return nil }
func (sessionStub) ServiceActions() ServiceController { return nil }
func (sessionStub) Services() ServiceReader           { return nil }
func (sessionStub) ServiceLogs() ServiceLogReader     { return nil }
func (sessionStub) Events() EventReader               { return nil }
func (sessionStub) Etcd() EtcdReader                  { return nil }
func (sessionStub) Processes() ProcessReader          { return nil }
func (sessionStub) Disks() DiskReader                 { return nil }
func (sessionStub) Network() NetworkReader            { return nil }
func (sessionStub) Dmesg() DmesgReader                { return nil }
func (sessionStub) Netstat() NetstatReader            { return nil }
func (sessionStub) Mounts() MountReader               { return nil }
func (sessionStub) Memory() MemoryReader              { return nil }
func (sessionStub) ResourceKinds() ResourceKindReader { return nil }
func (sessionStub) Resources() ResourceInstanceReader { return nil }
func (sessionStub) EtcdOperations() EtcdOperations    { return stubEtcdOperations{} }
func (sessionStub) Close() error                      { return nil }

var _ Session = sessionStub{}

func TestSessionExposesEtcdOperations(t *testing.T) {
	var session Session = sessionStub{}

	assert.NotNil(t, session.EtcdOperations())
}
