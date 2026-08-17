package testkit

import (
	"context"
	"sync"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type FakeContextCatalog struct {
	ListFunc func(context.Context) ([]domain.ClusterContext, error)
}

func (f *FakeContextCatalog) List(ctx context.Context) ([]domain.ClusterContext, error) {
	return f.ListFunc(ctx)
}

type FakeNodeReader struct {
	ListFunc func(context.Context) (domain.NodeSet, error)
}

func (f *FakeNodeReader) List(ctx context.Context) (domain.NodeSet, error) {
	return f.ListFunc(ctx)
}

type FakeNodeController struct {
	RebootFunc              func(ctx context.Context, target string, mode ports.RebootMode) error
	ShutdownFunc            func(ctx context.Context, target string, force bool) error
	RollbackFunc            func(ctx context.Context, target string) error
	UpgradeFunc             func(ctx context.Context, target, image string) error
	CurrentInstallImageFunc func(ctx context.Context, target string) (string, error)
}

func (f *FakeNodeController) Reboot(ctx context.Context, target string, mode ports.RebootMode) error {
	return f.RebootFunc(ctx, target, mode)
}

func (f *FakeNodeController) Shutdown(ctx context.Context, target string, force bool) error {
	return f.ShutdownFunc(ctx, target, force)
}

func (f *FakeNodeController) Rollback(ctx context.Context, target string) error {
	return f.RollbackFunc(ctx, target)
}

func (f *FakeNodeController) Upgrade(ctx context.Context, target, image string) error {
	return f.UpgradeFunc(ctx, target, image)
}

func (f *FakeNodeController) CurrentInstallImage(ctx context.Context, target string) (string, error) {
	return f.CurrentInstallImageFunc(ctx, target)
}

type FakeServiceController struct {
	StartFunc   func(ctx context.Context, node, service string) error
	StopFunc    func(ctx context.Context, node, service string) error
	RestartFunc func(ctx context.Context, node, service string) error
}

func (f *FakeServiceController) Start(ctx context.Context, node, service string) error {
	return f.StartFunc(ctx, node, service)
}

func (f *FakeServiceController) Stop(ctx context.Context, node, service string) error {
	return f.StopFunc(ctx, node, service)
}

func (f *FakeServiceController) Restart(ctx context.Context, node, service string) error {
	return f.RestartFunc(ctx, node, service)
}

type FakeServiceReader struct {
	ListFunc func(context.Context) (domain.ServiceSet, error)
}

func (f *FakeServiceReader) List(ctx context.Context) (domain.ServiceSet, error) {
	return f.ListFunc(ctx)
}

type FakeEventReader struct {
	ListFunc func(context.Context) (domain.EventSet, error)
}

func (f *FakeEventReader) List(ctx context.Context) (domain.EventSet, error) {
	return f.ListFunc(ctx)
}

type FakeEtcdReader struct {
	ListFunc func(context.Context, []string) (domain.EtcdSet, error)
}

func (f *FakeEtcdReader) List(ctx context.Context, controlPlaneNodes []string) (domain.EtcdSet, error) {
	return f.ListFunc(ctx, controlPlaneNodes)
}

type FakeProcessReader struct {
	ListFunc func(context.Context, string) (domain.ProcessSet, error)
}

func (f *FakeProcessReader) List(ctx context.Context, node string) (domain.ProcessSet, error) {
	return f.ListFunc(ctx, node)
}

type FakeDiskReader struct {
	ListFunc func(context.Context, string) (domain.DiskSet, error)
}

func (f *FakeDiskReader) List(ctx context.Context, node string) (domain.DiskSet, error) {
	return f.ListFunc(ctx, node)
}

type FakeNetworkReader struct {
	ListFunc func(context.Context, string) (domain.NetworkSet, error)
}

func (f *FakeNetworkReader) List(ctx context.Context, node string) (domain.NetworkSet, error) {
	return f.ListFunc(ctx, node)
}

type FakeResourceKindReader struct {
	ListFunc func(context.Context) (domain.ResourceKindSet, error)
}

func (f *FakeResourceKindReader) List(ctx context.Context) (domain.ResourceKindSet, error) {
	return f.ListFunc(ctx)
}

type FakeResourceInstanceReader struct {
	ListFunc func(ctx context.Context, node, kindType string) (domain.ResourceInstanceSet, error)
	GetFunc  func(ctx context.Context, node, kindType, id string) (domain.ResourceInstanceSnapshot, error)
}

func (f *FakeResourceInstanceReader) List(ctx context.Context, node, kindType string) (domain.ResourceInstanceSet, error) {
	return f.ListFunc(ctx, node, kindType)
}

func (f *FakeResourceInstanceReader) Get(ctx context.Context, node, kindType, id string) (domain.ResourceInstanceSnapshot, error) {
	return f.GetFunc(ctx, node, kindType, id)
}

type FakeKubernetesNodeReader struct {
	ListFunc func(context.Context) (map[string]domain.KubernetesNodeSnapshot, error)
}

func (f *FakeKubernetesNodeReader) List(ctx context.Context) (map[string]domain.KubernetesNodeSnapshot, error) {
	return f.ListFunc(ctx)
}

type FakeKubernetesResolver struct {
	ResolveFunc func(ctx context.Context, talosContext string) (ports.KubernetesNodeReader, error)
}

func (f *FakeKubernetesResolver) Resolve(ctx context.Context, talosContext string) (ports.KubernetesNodeReader, error) {
	return f.ResolveFunc(ctx, talosContext)
}

type FakeSession struct {
	NodeReader             ports.NodeReader
	NodeController         ports.NodeController
	ServiceController      ports.ServiceController
	ServiceReader          ports.ServiceReader
	LogReader              ports.ServiceLogReader
	EventReader            ports.EventReader
	EtcdReader             ports.EtcdReader
	ProcessReader          ports.ProcessReader
	DiskReader             ports.DiskReader
	NetworkReader          ports.NetworkReader
	ResourceKindReader     ports.ResourceKindReader
	ResourceInstanceReader ports.ResourceInstanceReader
	CloseFunc              func() error

	mu         sync.Mutex
	closeCount int
}

func (f *FakeSession) Nodes() ports.NodeReader { return f.NodeReader }

func (f *FakeSession) NodeActions() ports.NodeController { return f.NodeController }

func (f *FakeSession) ServiceActions() ports.ServiceController { return f.ServiceController }

func (f *FakeSession) Services() ports.ServiceReader { return f.ServiceReader }

func (f *FakeSession) ServiceLogs() ports.ServiceLogReader { return f.LogReader }

func (f *FakeSession) Events() ports.EventReader { return f.EventReader }

func (f *FakeSession) Etcd() ports.EtcdReader { return f.EtcdReader }

func (f *FakeSession) Processes() ports.ProcessReader { return f.ProcessReader }

func (f *FakeSession) Disks() ports.DiskReader { return f.DiskReader }

func (f *FakeSession) Network() ports.NetworkReader { return f.NetworkReader }

func (f *FakeSession) ResourceKinds() ports.ResourceKindReader { return f.ResourceKindReader }

func (f *FakeSession) Resources() ports.ResourceInstanceReader { return f.ResourceInstanceReader }

func (f *FakeSession) Close() error {
	f.mu.Lock()
	f.closeCount++
	f.mu.Unlock()

	if f.CloseFunc != nil {
		return f.CloseFunc()
	}

	return nil
}

func (f *FakeSession) CloseCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.closeCount
}

type FakeSessionFactory struct {
	OpenFunc func(context.Context, string) (ports.Session, error)
}

func (f *FakeSessionFactory) Open(ctx context.Context, contextName string) (ports.Session, error) {
	return f.OpenFunc(ctx, contextName)
}
