package talos

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
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
	assert.Equal(t, "factory.talos.dev/metal-installer/abc123:v1.13.4", deriveSchematicInstallerImage("factory.talos.dev", "metal", "abc123", "v1.13.4"))
	assert.Equal(t, "factory.talos.dev/aws-installer/abc123:v1.13.4", deriveSchematicInstallerImage("factory.talos.dev", "aws", "abc123", "v1.13.4"))
	assert.Equal(t, "", deriveSchematicInstallerImage("factory.talos.dev", "", "abc123", "v1.13.4"))
}
func TestParseSchematicAuthor(t *testing.T) {
	flavor, factory := parseSchematicAuthor("metal (https://factory.example)")
	assert.Equal(t, "metal", flavor)
	assert.Equal(t, "https://factory.example", factory)
}
func TestSupportsLifecycleUpgradeAPI(t *testing.T) {
	assert.False(t, supportsLifecycleUpgradeAPI("v1.12.9"))
	assert.True(t, supportsLifecycleUpgradeAPI("v1.13.3"))
	assert.False(t, supportsLifecycleUpgradeAPI("v2.0.0"))
	assert.False(t, supportsLifecycleUpgradeAPI("not-a-version"))
}

type fakeLifecycleMaintenanceClient struct {
	*fakeNodeControlClient
	version         string
	lifecycleTarget string
	steps           []string
	lifecycleErr    error
	prepareErr      error
	maintenance     *fakeUpgradeMaintenance
}

func (c *fakeLifecycleMaintenanceClient) upgradeVersion(context.Context) (string, error) {
	return c.version, nil
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
	steps        *[]string
	drainErr     error
	rebootErr    error
	waitTalosErr error
	waitKubeErr  error
	uncordonErr  error
}

func (m *fakeUpgradeMaintenance) Cordon(context.Context) error {
	*m.steps = append(*m.steps, "cordon")

	return nil
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
	assert.Equal(t, "10.0.0.12", client.lifecycleTarget)
	assert.Equal(t, []string{"cordon", "drain", "reboot", "wait-talos", "wait-kubernetes", "uncordon"}, client.steps)
	assert.Equal(t, ports.UpgradeComplete, results[len(results)-1].Event.Phase)
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
