package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openReadTestSession(t *testing.T, adapter ports.Session) *application.Session {
	t.Helper()
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{
		OpenFunc: func(context.Context, string) (ports.Session, error) { return adapter, nil },
	}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestDiagnosticsUseCallerAndSessionContext(t *testing.T) {
	t.Run("caller cancellation reaches the port", func(t *testing.T) {
		entered := make(chan struct{})
		adapter := &testkit.FakeSession{ProcessReader: &testkit.FakeProcessReader{ListFunc: func(ctx context.Context, node string) (domain.ProcessSet, error) {
			assert.Equal(t, "worker-7", node)
			close(entered)
			<-ctx.Done()
			return domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 7}}}, ctx.Err()
		}}}
		session := openReadTestSession(t, adapter)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := session.Processes(ctx, "worker-7"); done <- err }()
		<-entered
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
	})

	t.Run("session close cancels an active read", func(t *testing.T) {
		entered := make(chan struct{})
		adapter := &testkit.FakeSession{ProcessReader: &testkit.FakeProcessReader{ListFunc: func(ctx context.Context, node string) (domain.ProcessSet, error) {
			close(entered)
			<-ctx.Done()
			return domain.ProcessSet{}, ctx.Err()
		}}}
		session := openReadTestSession(t, adapter)
		done := make(chan error, 1)
		go func() { _, err := session.Processes(context.Background(), "worker-7"); done <- err }()
		<-entered
		require.NoError(t, session.Close())
		require.ErrorIs(t, <-done, context.Canceled)
	})
}

func TestDiagnosticsPreservePartialValues(t *testing.T) {
	partialCPU := domain.CPUSnapshot{Cores: []domain.CPUCoreSnapshot{{ID: 3, CPUUtilization: domain.CPUUtilization{Known: true, BusyPercent: 42}}}, RunningProcesses: 9, ProcessCountsKnown: true, Problems: []string{"second sample unavailable"}}
	partialTime := domain.TimeSnapshot{TimeSample: domain.TimeSample{Server: "node-a", Problems: []string{"clock status unavailable"}}, SyncKnown: true, Synced: true}
	readErr := errors.New("partial read")
	session := openReadTestSession(t, &testkit.FakeSession{
		CPUReader:  &testkit.FakeCPUReader{SampleFunc: func(context.Context, string) (domain.CPUSnapshot, error) { return partialCPU, readErr }},
		TimeReader: &testkit.FakeTimeReader{ReadFunc: func(context.Context, string) (domain.TimeSnapshot, error) { return partialTime, readErr }},
	})
	cpu, err := session.CPU(context.Background(), "node-a")
	require.ErrorIs(t, err, readErr)
	assert.Equal(t, partialCPU, cpu)
	clock, err := session.Time(context.Background(), "node-a")
	require.ErrorIs(t, err, readErr)
	assert.Equal(t, partialTime, clock)
}

func TestDiagnosticsRouteToRequestedNode(t *testing.T) {
	routed := map[string]string{}
	remember := func(name string) func(context.Context, string) error {
		return func(_ context.Context, node string) error { routed[name] = node; return nil }
	}
	adapter := &testkit.FakeSession{
		ProcessReader: &testkit.FakeProcessReader{ListFunc: func(ctx context.Context, node string) (domain.ProcessSet, error) {
			return domain.ProcessSet{}, remember("processes")(ctx, node)
		}},
		DiskReader: &testkit.FakeDiskReader{ListFunc: func(ctx context.Context, node string) (domain.DiskSet, error) {
			return domain.DiskSet{}, remember("disks")(ctx, node)
		}},
		NetworkReader: &testkit.FakeNetworkReader{ListFunc: func(ctx context.Context, node string) (domain.NetworkSet, error) {
			return domain.NetworkSet{}, remember("network")(ctx, node)
		}},
		NetstatReader: &testkit.FakeNetstatReader{ListFunc: func(ctx context.Context, node string) (domain.SocketSet, error) {
			return domain.SocketSet{}, remember("sockets")(ctx, node)
		}},
		MountReader: &testkit.FakeMountReader{ListFunc: func(ctx context.Context, node string) (domain.MountSet, error) {
			return domain.MountSet{}, remember("mounts")(ctx, node)
		}},
		MemoryReader: &testkit.FakeMemoryReader{ListFunc: func(ctx context.Context, node string) (domain.MemorySnapshot, error) {
			return domain.MemorySnapshot{}, remember("memory")(ctx, node)
		}},
		CPUReader: &testkit.FakeCPUReader{SampleFunc: func(ctx context.Context, node string) (domain.CPUSnapshot, error) {
			return domain.CPUSnapshot{}, remember("cpu")(ctx, node)
		}},
		TimeReader: &testkit.FakeTimeReader{
			ReadFunc: func(ctx context.Context, node string) (domain.TimeSnapshot, error) {
				return domain.TimeSnapshot{}, remember("time")(ctx, node)
			},
			CheckFunc: func(ctx context.Context, node, _ string) (domain.TimeCheckResult, error) {
				return domain.TimeCheckResult{}, remember("time-check")(ctx, node)
			},
		},
		NodeController: &testkit.FakeNodeController{CurrentInstallImageFunc: func(ctx context.Context, node string) (string, error) {
			return "", remember("install-image")(ctx, node)
		}},
	}
	session := openReadTestSession(t, adapter)
	cases := []struct {
		name string
		read func() error
	}{
		{"processes", func() error { _, err := session.Processes(context.Background(), "node-17"); return err }},
		{"disks", func() error { _, err := session.Disks(context.Background(), "node-17"); return err }},
		{"network", func() error { _, err := session.Network(context.Background(), "node-17"); return err }},
		{"sockets", func() error { _, err := session.Sockets(context.Background(), "node-17"); return err }},
		{"mounts", func() error { _, err := session.Mounts(context.Background(), "node-17"); return err }},
		{"memory", func() error { _, err := session.Memory(context.Background(), "node-17"); return err }},
		{"cpu", func() error { _, err := session.CPU(context.Background(), "node-17"); return err }},
		{"time", func() error { _, err := session.Time(context.Background(), "node-17"); return err }},
		{"time-check", func() error { _, err := session.CheckTime(context.Background(), "node-17", "ntp.example"); return err }},
		{"install-image", func() error { _, err := session.CurrentInstallImage(context.Background(), "node-17"); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, tc.read())
			assert.Equal(t, "node-17", routed[tc.name])
		})
	}
}

func TestMissingDiagnosticCapabilityIsExplicit(t *testing.T) {
	session := openReadTestSession(t, &testkit.FakeSession{})
	cases := []struct {
		name string
		read func() error
	}{
		{"processes", func() error { _, err := session.Processes(context.Background(), "node"); return err }},
		{"disks", func() error { _, err := session.Disks(context.Background(), "node"); return err }},
		{"network", func() error { _, err := session.Network(context.Background(), "node"); return err }},
		{"sockets", func() error { _, err := session.Sockets(context.Background(), "node"); return err }},
		{"mounts", func() error { _, err := session.Mounts(context.Background(), "node"); return err }},
		{"memory", func() error { _, err := session.Memory(context.Background(), "node"); return err }},
		{"cpu", func() error { _, err := session.CPU(context.Background(), "node"); return err }},
		{"time", func() error { _, err := session.Time(context.Background(), "node"); return err }},
		{"time check", func() error { _, err := session.CheckTime(context.Background(), "node", "time.example"); return err }},
		{"install image", func() error { _, err := session.CurrentInstallImage(context.Background(), "node"); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { require.ErrorIs(t, tc.read(), application.ErrCapabilityUnavailable) })
	}
}

func TestCheckTimeValidatesBeforeRPC(t *testing.T) {
	calls := 0
	denied := ports.ErrTimePermissionDenied
	session := openReadTestSession(t, &testkit.FakeSession{TimeReader: &testkit.FakeTimeReader{
		CheckFunc: func(_ context.Context, node, server string) (domain.TimeCheckResult, error) {
			calls++
			assert.Equal(t, "node-a", node)
			assert.Equal(t, "ntp.example", server)
			return domain.TimeCheckResult{TimeSample: domain.TimeSample{Server: server, Problems: []string{"denied"}}}, denied
		},
	}})
	for _, input := range []string{"https://ntp.example", "ntp.example:123", "bad host"} {
		_, err := session.CheckTime(context.Background(), "node-a", input)
		require.Error(t, err)
		assert.Zero(t, calls, "invalid server must be rejected before the port is called")
	}
	value, err := session.CheckTime(context.Background(), "node-a", " ntp.example ")
	require.ErrorIs(t, err, ports.ErrTimePermissionDenied)
	assert.Equal(t, "ntp.example", value.Server)
	assert.Equal(t, []string{"denied"}, value.Problems)
	assert.Equal(t, 1, calls)
}

func TestCurrentInstallImageUsesSessionController(t *testing.T) {
	var gotNode string
	controller := &testkit.FakeNodeController{CurrentInstallImageFunc: func(_ context.Context, node string) (string, error) {
		gotNode = node
		return "factory/image:v1.2.3", nil
	}}
	session := openReadTestSession(t, &testkit.FakeSession{NodeController: controller})
	image, err := session.CurrentInstallImage(context.Background(), "worker-9")
	require.NoError(t, err)
	assert.Equal(t, "factory/image:v1.2.3", image)
	assert.Equal(t, "worker-9", gotNode)
}
