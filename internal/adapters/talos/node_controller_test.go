package talos

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestDeriveUpgradeImageReplacesTagKeepingRepo(t *testing.T) {
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.2", deriveUpgradeImage("ghcr.io/siderolabs/installer:v1.13.0", "v1.13.2"))
}

func TestDeriveUpgradeImagePreservesRegistryPort(t *testing.T) {
	assert.Equal(t, "registry.internal:5000/talos/installer:v1.13.2", deriveUpgradeImage("registry.internal:5000/talos/installer:v1.13.0", "v1.13.2"))
}

func TestDeriveUpgradeImageAppendsTagWhenDeclaredImageHasNone(t *testing.T) {
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.2", deriveUpgradeImage("ghcr.io/siderolabs/installer", "v1.13.2"))
}

func TestDeriveUpgradeImageLeavesDigestReferencesUntouched(t *testing.T) {
	assert.Equal(t, "ghcr.io/siderolabs/installer@sha256:abcd", deriveUpgradeImage("ghcr.io/siderolabs/installer@sha256:abcd", "v1.13.2"))
}

func TestDeriveUpgradeImageHandlesEmptyInputs(t *testing.T) {
	assert.Equal(t, "", deriveUpgradeImage("", "v1.13.2"))
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.0", deriveUpgradeImage("ghcr.io/siderolabs/installer:v1.13.0", ""))
}

type fakeInstallImageLookup struct {
	declared     string
	declaredErr  error
	author       string
	schematic    string
	schematicErr error
	platformName string
	platformErr  error
	version      string
	versionErr   error
}

func (f fakeInstallImageLookup) declaredInstallImage(context.Context) (string, error) {
	return f.declared, f.declaredErr
}

func (f fakeInstallImageLookup) schematicMetadata(context.Context) (string, string, error) {
	return f.author, f.schematic, f.schematicErr
}

func (f fakeInstallImageLookup) platform(context.Context) (string, error) {
	return f.platformName, f.platformErr
}

func (f fakeInstallImageLookup) runningTalosVersion(context.Context) (string, error) {
	return f.version, f.versionErr
}

func TestCurrentInstallImageUsesCanonicalFactoryMetadata(t *testing.T) {
	const schematic = "75859b9f9a0bc974287be95a622cc7db6f642581a51435cb87eab7e07df8e673"
	image, err := currentInstallImage(t.Context(), fakeInstallImageLookup{
		declared:     "ghcr.io/siderolabs/installer:v1.13.0",
		author:       "Image Factory (https://factory.talos.dev/)",
		schematic:    schematic,
		platformName: "metal",
		version:      "v1.13.4",
	})

	require.NoError(t, err)
	assert.Equal(t, "factory.talos.dev/metal-installer/"+schematic+":v1.13.4", image)
}

func TestCurrentInstallImageFallsBackToDeclaredImageForInvalidFactoryMetadata(t *testing.T) {
	image, err := currentInstallImage(t.Context(), fakeInstallImageLookup{
		declared:     "ghcr.io/siderolabs/installer:v1.13.0",
		author:       "Image Factory (http://factory.talos.dev/)",
		schematic:    "live",
		platformName: "metal",
		version:      "v1.13.4",
	})

	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.4", image)
}

type fakeNodeControlClient struct {
	rebootReq       machineapi.RebootRequest
	rebootErr       error
	shutdownReq     machineapi.ShutdownRequest
	shutdownErr     error
	rollbackErr     error
	upgradeReq      talosclient.UpgradeOptions
	upgradeErr      error
	currentImage    string
	currentImageErr error
}

func (c *fakeNodeControlClient) Reboot(ctx context.Context, opts ...talosclient.RebootMode) error {
	for _, opt := range opts {
		opt(&c.rebootReq)
	}
	return c.rebootErr
}

func (c *fakeNodeControlClient) Shutdown(ctx context.Context, opts ...talosclient.ShutdownOption) error {
	for _, opt := range opts {
		opt(&c.shutdownReq)
	}
	return c.shutdownErr
}

func (c *fakeNodeControlClient) Rollback(ctx context.Context) error {
	return c.rollbackErr
}

func (c *fakeNodeControlClient) Upgrade(ctx context.Context, opts ...talosclient.UpgradeOption) error {
	for _, opt := range opts {
		opt(&c.upgradeReq)
	}
	return c.upgradeErr
}

func (c *fakeNodeControlClient) CurrentInstallImage(ctx context.Context) (string, error) {
	return c.currentImage, c.currentImageErr
}

func TestNodeControllerRebootDefaultModeSendsNoModeOverride(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Reboot(t.Context(), "cp-1", ports.RebootDefault)

	require.NoError(t, err)
	assert.Equal(t, machineapi.RebootRequest_DEFAULT, client.rebootReq.Mode)
}

func TestNodeControllerRebootPowercycleSetsMode(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Reboot(t.Context(), "cp-1", ports.RebootPowercycle)

	require.NoError(t, err)
	assert.Equal(t, machineapi.RebootRequest_POWERCYCLE, client.rebootReq.Mode)
}

func TestNodeControllerRebootWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{rebootErr: errors.New("unreachable")}
	controller := newNodeController(client)

	err := controller.Reboot(t.Context(), "cp-1", ports.RebootDefault)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
	assert.Contains(t, err.Error(), "unreachable")
}

func TestNodeControllerShutdownForceFalseLeavesForceUnset(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Shutdown(t.Context(), "cp-1", false)

	require.NoError(t, err)
	assert.False(t, client.shutdownReq.Force)
}

func TestNodeControllerShutdownForceTrueSetsForce(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Shutdown(t.Context(), "cp-1", true)

	require.NoError(t, err)
	assert.True(t, client.shutdownReq.Force)
}

func TestNodeControllerShutdownWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{shutdownErr: errors.New("unreachable")}
	controller := newNodeController(client)

	err := controller.Shutdown(t.Context(), "cp-1", false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
}

func TestNodeControllerRollbackWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{rollbackErr: errors.New("no previous version")}
	controller := newNodeController(client)

	err := controller.Rollback(t.Context(), "cp-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
	assert.Contains(t, err.Error(), "no previous version")
}

func TestNodeControllerRollbackSucceeds(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Rollback(t.Context(), "cp-1")

	require.NoError(t, err)
}

func TestNodeControllerUpgradeSendsImage(t *testing.T) {
	client := &fakeNodeControlClient{}
	controller := newNodeController(client)

	err := controller.Upgrade(t.Context(), "cp-1", "ghcr.io/siderolabs/installer:v1.13.3")

	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.3", client.upgradeReq.Request.Image)
}

func TestNodeControllerUpgradeWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{upgradeErr: errors.New("incompatible image")}
	controller := newNodeController(client)

	err := controller.Upgrade(t.Context(), "cp-1", "bad:image")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
	assert.Contains(t, err.Error(), "incompatible image")
}

func TestNodeControllerCurrentInstallImageReturnsImage(t *testing.T) {
	client := &fakeNodeControlClient{currentImage: "ghcr.io/siderolabs/installer:v1.13.2"}
	controller := newNodeController(client)

	image, err := controller.CurrentInstallImage(t.Context(), "cp-1")

	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/siderolabs/installer:v1.13.2", image)
}

func TestNodeControllerCurrentInstallImageWrapsError(t *testing.T) {
	client := &fakeNodeControlClient{currentImageErr: errors.New("resource not found")}
	controller := newNodeController(client)

	_, err := controller.CurrentInstallImage(t.Context(), "cp-1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cp-1")
}
func TestDeriveSchematicInstallerImage(t *testing.T) {
	tests := map[string]struct {
		factoryURL string
		platform   string
		want       string
	}{
		"metal":  {"https://factory.talos.dev/", "metal", "factory.talos.dev/metal-installer/live:v1.13.4"},
		"aws":    {"https://factory.talos.dev/", "aws", "factory.talos.dev/aws-installer/live:v1.13.4"},
		"custom": {"https://factory.example:5000/", "metal", "factory.example:5000/metal-installer/live:v1.13.4"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want, deriveSchematicInstallerImage(test.factoryURL, test.platform, "live", "v1.13.4"))
		})
	}
}

func TestDeriveSchematicInstallerImageRejectsUnsafeMetadata(t *testing.T) {
	for name, input := range map[string][2]string{
		"http":        {"http://factory.talos.dev/", "metal"},
		"credentials": {"https://user:pass@factory.talos.dev/", "metal"},
		"path":        {"https://factory.talos.dev/api", "metal"},
		"query":       {"https://factory.talos.dev/?x=1", "metal"},
		"fragment":    {"https://factory.talos.dev/#x", "metal"},
		"uppercase":   {"https://factory.talos.dev/", "Metal"},
		"dot":         {"https://factory.talos.dev/", "metal.bad"},
		"unsafe":      {"https://factory.talos.dev/", "metal/evil"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, deriveSchematicInstallerImage(input[0], input[1], "live", "v1.13.4"))
		})
	}
	assert.Empty(t, deriveSchematicInstallerImage("https://factory.talos.dev/", "metal", "", "v1.13.4"))
	assert.Empty(t, deriveSchematicInstallerImage("https://factory.talos.dev/", "metal", "live", ""))
}

func TestSupportsLifecycleUpgradeAPI(t *testing.T) {
	assert.False(t, supportsLifecycleUpgradeAPI("v1.12.9"))
	assert.True(t, supportsLifecycleUpgradeAPI("v1.13.3"))
	assert.False(t, supportsLifecycleUpgradeAPI("v2.0.0"))
	assert.False(t, supportsLifecycleUpgradeAPI("not-a-version"))
}

type fakeLifecycleOperations struct {
	order      []string
	pull       imagePullStream
	pullErr    error
	install    lifecycleInstallStream
	installErr error
}

func (f *fakeLifecycleOperations) Pull(context.Context, string) (imagePullStream, error) {
	f.order = append(f.order, "pull")
	return f.pull, f.pullErr
}

func (f *fakeLifecycleOperations) Upgrade(context.Context, string) (lifecycleInstallStream, error) {
	f.order = append(f.order, "install")
	return f.install, f.installErr
}

type fakeImagePullStream struct {
	responses []*machineapi.ImageServicePullResponse
}

func (s *fakeImagePullStream) Recv() (*machineapi.ImageServicePullResponse, error) {
	if len(s.responses) == 0 {
		return nil, io.EOF
	}
	response := s.responses[0]
	s.responses = s.responses[1:]
	return response, nil
}

type fakeLifecycleInstallStream struct {
	responses []*machineapi.LifecycleServiceUpgradeResponse
}

func (s *fakeLifecycleInstallStream) Recv() (*machineapi.LifecycleServiceUpgradeResponse, error) {
	if len(s.responses) == 0 {
		return nil, io.EOF
	}
	response := s.responses[0]
	s.responses = s.responses[1:]
	return response, nil
}

func lifecycleExitCode(code int32) *machineapi.LifecycleServiceUpgradeResponse {
	return &machineapi.LifecycleServiceUpgradeResponse{Progress: &machineapi.LifecycleServiceInstallProgress{
		Response: &machineapi.LifecycleServiceInstallProgress_ExitCode{ExitCode: code},
	}}
}

func TestMachineryLifecycleUpgradePullsBeforeInstalling(t *testing.T) {
	ops := &fakeLifecycleOperations{
		pull:    &fakeImagePullStream{},
		install: &fakeLifecycleInstallStream{responses: []*machineapi.LifecycleServiceUpgradeResponse{lifecycleExitCode(0)}},
	}
	client := machineryNodeControlClient{lifecycle: ops}
	var events []ports.UpgradeEvent

	err := client.lifecycleUpgrade(t.Context(), "image:v1.13.4", func(event ports.UpgradeEvent) { events = append(events, event) })

	require.NoError(t, err)
	assert.Equal(t, []string{"pull", "install"}, ops.order)
	assert.Equal(t, ports.UpgradeInstalling, events[len(events)-1].Phase)
}

func TestMachineryLifecycleUpgradeRejectsNonzeroExitCode(t *testing.T) {
	client := machineryNodeControlClient{lifecycle: &fakeLifecycleOperations{
		pull:    &fakeImagePullStream{},
		install: &fakeLifecycleInstallStream{responses: []*machineapi.LifecycleServiceUpgradeResponse{lifecycleExitCode(17)}},
	}}

	err := client.lifecycleUpgrade(t.Context(), "image:v1.13.4", func(ports.UpgradeEvent) {})

	require.ErrorContains(t, err, "exit code 17")
}

func TestMachineryLifecycleUpgradeRejectsEOFBeforeExitCode(t *testing.T) {
	client := machineryNodeControlClient{lifecycle: &fakeLifecycleOperations{
		pull:    &fakeImagePullStream{},
		install: &fakeLifecycleInstallStream{},
	}}

	err := client.lifecycleUpgrade(t.Context(), "image:v1.13.4", func(ports.UpgradeEvent) {})

	require.ErrorContains(t, err, "ended without exit code")
}

type fakeLifecycleMaintenanceClient struct {
	*fakeNodeControlClient
	version         string
	versionErr      error
	versionTarget   string
	lifecycleTarget string
	steps           []string
	lifecycleErr    error
	prepareErr      error
	maintenance     *fakeUpgradeMaintenance
}

func (c *fakeLifecycleMaintenanceClient) upgradeVersion(ctx context.Context) (string, error) {
	if outgoing, ok := metadata.FromOutgoingContext(ctx); ok {
		nodes := outgoing.Get("node")
		if len(nodes) > 0 {
			c.versionTarget = nodes[0]
		}
	}
	return c.version, c.versionErr
}

func (c *fakeLifecycleMaintenanceClient) lifecycleUpgrade(ctx context.Context, _ string, progress func(ports.UpgradeEvent)) error {
	if outgoing, ok := metadata.FromOutgoingContext(ctx); ok {
		nodes := outgoing.Get("node")
		if len(nodes) > 0 {
			c.lifecycleTarget = nodes[0]
		}
	}
	if c.lifecycleTarget == "" {
		return errors.New("lifecycle upgrade was not targeted to the selected node")
	}
	c.steps = append(c.steps, "lifecycle")
	progress(ports.UpgradeEvent{Phase: ports.UpgradePulling, Message: "pulled"})
	progress(ports.UpgradeEvent{Phase: ports.UpgradeInstalling, Message: "installed"})

	return c.lifecycleErr
}

func (c *fakeLifecycleMaintenanceClient) prepareUpgradeMaintenance(context.Context, string) (upgradeMaintenance, error) {
	if c.prepareErr != nil {
		return nil, c.prepareErr
	}

	return c.maintenance, nil
}

type fakeUpgradeMaintenance struct {
	steps           *[]string
	alreadyCordoned bool
	drainErr        error
	rebootErr       error
	waitTalosErr    error
	waitKubeErr     error
	uncordonErr     error
}

func (m *fakeUpgradeMaintenance) Cordon(context.Context) (bool, error) {
	*m.steps = append(*m.steps, "cordon")

	return !m.alreadyCordoned, nil
}

func (m *fakeUpgradeMaintenance) Drain(context.Context, time.Duration, func(string)) error {
	*m.steps = append(*m.steps, "drain")

	return m.drainErr
}

func (m *fakeUpgradeMaintenance) Reboot(context.Context) error {
	*m.steps = append(*m.steps, "reboot")

	return m.rebootErr
}

func (m *fakeUpgradeMaintenance) WaitTalosReady(context.Context, time.Duration) error {
	*m.steps = append(*m.steps, "wait-talos")

	return m.waitTalosErr
}

func (m *fakeUpgradeMaintenance) WaitKubernetesReady(context.Context, time.Duration) error {
	*m.steps = append(*m.steps, "wait-kubernetes")

	return m.waitKubeErr
}

func (m *fakeUpgradeMaintenance) Uncordon(context.Context) error {
	*m.steps = append(*m.steps, "uncordon")

	return m.uncordonErr
}

func upgradeResults(stream ports.UpgradeStream) []ports.UpgradeResult {
	var results []ports.UpgradeResult
	for result := range stream.Results() {
		results = append(results, result)
	}

	return results
}

func TestNodeControllerUpgradeStreamTargetsLifecycleThenSafelyMaintainsNode(t *testing.T) {
	steps := []string{}
	client := &fakeLifecycleMaintenanceClient{
		fakeNodeControlClient: &fakeNodeControlClient{},
		version:               "v1.13.3",
		steps:                 steps,
	}
	client.maintenance = &fakeUpgradeMaintenance{steps: &client.steps}

	results := upgradeResults(newNodeController(client).UpgradeStream(t.Context(), "10.0.0.12", "factory.talos.dev/metal-installer/abc:v1.13.4"))

	require.NotEmpty(t, results)
	require.NoError(t, results[len(results)-1].Err)
	assert.True(t, results[len(results)-1].Done)
	assert.Equal(t, "10.0.0.12", client.versionTarget)
	assert.Equal(t, "10.0.0.12", client.lifecycleTarget)
	assert.Equal(t, []string{"lifecycle", "cordon", "drain", "reboot", "wait-talos", "wait-kubernetes", "uncordon"}, client.steps)
	assert.Equal(t, ports.UpgradeComplete, results[len(results)-1].Event.Phase)
}

func TestSafeUpgradeMaintenanceLeavesPreexistingCordonInPlace(t *testing.T) {
	steps := []string{}
	maintenance := &fakeUpgradeMaintenance{steps: &steps, alreadyCordoned: true}

	err := runSafeUpgradeMaintenance(t.Context(), maintenance, func(ports.UpgradeEvent) {})

	require.NoError(t, err)
	assert.Equal(t, []string{"cordon", "drain", "reboot", "wait-talos", "wait-kubernetes"}, steps)
}

func TestNewDrainHelperInitializesOutputWriters(t *testing.T) {
	helper := newDrainHelper(t.Context(), k8sfake.NewSimpleClientset(), time.Minute)
	assert.NotNil(t, helper.Out)
	assert.NotNil(t, helper.ErrOut)
}

func TestMachineryCordonPreservesPreexistingUnschedulableNode(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
	})
	maintenance := machineryUpgradeMaintenance{clientset: clientset, nodeName: "worker-1"}

	changed, err := maintenance.Cordon(t.Context())

	require.NoError(t, err)
	assert.False(t, changed)
	node, err := clientset.CoreV1().Nodes().Get(t.Context(), "worker-1", metav1.GetOptions{})
	require.NoError(t, err)
	assert.True(t, node.Spec.Unschedulable)
}

func TestNodeControllerUpgradeStreamCancellationDoesNotDrainOrBlockOnEvents(t *testing.T) {
	steps := []string{}
	entered := make(chan struct{})
	client := &fakeLifecycleMaintenanceClient{
		fakeNodeControlClient: &fakeNodeControlClient{},
		version:               "v1.13.3",
		steps:                 steps,
	}
	client.maintenance = &fakeUpgradeMaintenance{steps: &client.steps}
	client.lifecycleErr = nil

	stream := newNodeController(client).UpgradeStream(t.Context(), "10.0.0.12", "image:v1.13.4")
	go func() {
		<-stream.Results() // checking fills the buffer, leaving lifecycle progress blocked until cancellation-aware.
		close(entered)
	}()
	require.Eventually(t, func() bool {
		select {
		case <-entered:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	stream.Cancel()

	require.Eventually(t, func() bool {
		_, ok := <-stream.Results()
		return !ok
	}, time.Second, time.Millisecond)
	assert.NotContains(t, client.steps, "cordon")
	assert.NotContains(t, client.steps, "drain")
}

func TestLifecycleUpgradeAPIRangeBoundaries(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{version: "1.13.0-alpha.2", want: false},
		{version: "1.13.0-alpha.3", want: true},
		{version: "1.13.3", want: true},
		{version: "2.0.0", want: false},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			got, err := lifecycleUpgradeAPISupported(test.version)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
	_, err := lifecycleUpgradeAPISupported("not-a-version")
	assert.Error(t, err)
}

func TestNodeControllerUpgradeStreamUsesLegacyAtLifecycleLowerBoundary(t *testing.T) {
	client := &fakeLifecycleMaintenanceClient{fakeNodeControlClient: &fakeNodeControlClient{}, version: "1.13.0-alpha.2"}

	results := upgradeResults(newNodeController(client).UpgradeStream(t.Context(), "cp-1", "image:v1.13.0"))

	require.NoError(t, results[len(results)-1].Err)
	assert.Equal(t, "image:v1.13.0", client.upgradeReq.Request.Image)
	assert.Empty(t, client.steps)
}

func TestNodeControllerUpgradeStreamRejectsInvalidVersionWithoutLegacyUpgrade(t *testing.T) {
	client := &fakeLifecycleMaintenanceClient{fakeNodeControlClient: &fakeNodeControlClient{}, version: "not-a-version"}

	results := upgradeResults(newNodeController(client).UpgradeStream(t.Context(), "cp-1", "image:v1.13.0"))

	require.Error(t, results[len(results)-1].Err)
	assert.Empty(t, client.upgradeReq.Request.Image)
}

func TestNodeControllerUpgradeStreamDoesNotMaintainAfterLifecycleFailure(t *testing.T) {
	steps := []string{}
	client := &fakeLifecycleMaintenanceClient{
		fakeNodeControlClient: &fakeNodeControlClient{},
		version:               "v1.13.3",
		steps:                 steps,
		lifecycleErr:          errors.New("install failed"),
	}
	client.maintenance = &fakeUpgradeMaintenance{steps: &client.steps}

	results := upgradeResults(newNodeController(client).UpgradeStream(t.Context(), "cp-1", "image:v1.13.4"))

	require.Error(t, results[len(results)-1].Err)
	assert.Equal(t, []string{"lifecycle"}, client.steps)
}

func TestSafeUpgradeMaintenanceUncordonsAfterTalosReadinessFailure(t *testing.T) {
	readyErr := errors.New("Talos API unavailable")
	steps := []string{}
	maintenance := &fakeUpgradeMaintenance{steps: &steps, waitTalosErr: readyErr}

	err := runSafeUpgradeMaintenance(t.Context(), maintenance, func(ports.UpgradeEvent) {})

	require.ErrorIs(t, err, readyErr)
	assert.Equal(t, []string{"cordon", "drain", "reboot", "wait-talos", "uncordon"}, steps)
}

func TestSafeUpgradeMaintenanceUncordonsAndJoinsCleanupFailureAfterRebootFailure(t *testing.T) {
	primary := errors.New("reboot unavailable")
	cleanup := errors.New("uncordon unavailable")
	steps := []string{}
	maintenance := &fakeUpgradeMaintenance{
		steps:       &steps,
		rebootErr:   primary,
		uncordonErr: cleanup,
	}

	err := runSafeUpgradeMaintenance(t.Context(), maintenance, func(ports.UpgradeEvent) {})

	require.Error(t, err)
	assert.ErrorIs(t, err, primary)
	assert.ErrorIs(t, err, cleanup)
	assert.Equal(t, []string{"cordon", "drain", "reboot", "uncordon"}, steps)
}

func TestSafeUpgradeMaintenanceUncordonsAfterDrainFailure(t *testing.T) {
	drainErr := errors.New("pod disruption budget blocks eviction")
	steps := []string{}
	maintenance := &fakeUpgradeMaintenance{
		steps:    &steps,
		drainErr: drainErr,
	}

	err := runSafeUpgradeMaintenance(t.Context(), maintenance, func(ports.UpgradeEvent) {})

	require.ErrorIs(t, err, drainErr)
	assert.Equal(t, []string{"cordon", "drain", "uncordon"}, steps)
}
