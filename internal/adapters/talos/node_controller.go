package talos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/blang/semver/v4"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/m11s-io/t9s/internal/ports"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
	k8sresource "github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	runtimeresource "github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	kubectldrain "k8s.io/kubectl/pkg/drain"
)

type lifecycleUpgradeClient interface {
	lifecycleUpgrade(context.Context, string, func(ports.UpgradeEvent)) error
}

type versionedUpgradeClient interface {
	upgradeVersion(context.Context) (string, error)
}

const upgradeCapabilityTimeout = 15 * time.Second

type upgradeMaintenanceClient interface {
	prepareUpgradeMaintenance(context.Context, string) (upgradeMaintenance, error)
}

type upgradeMaintenance interface {
	// Cordon reports whether this upgrade changed the node's scheduling state.
	// Only a cordon created by t9s may be undone during cleanup.
	Cordon(context.Context) (bool, error)
	Drain(context.Context, time.Duration, func(string)) error
	Reboot(context.Context) error
	WaitTalosReady(context.Context, time.Duration) error
	WaitKubernetesReady(context.Context, time.Duration) error
	Uncordon(context.Context) error
}

type nodeControlClient interface {
	Reboot(ctx context.Context, opts ...talosclient.RebootMode) error
	Shutdown(ctx context.Context, opts ...talosclient.ShutdownOption) error
	Rollback(ctx context.Context) error
	Upgrade(ctx context.Context, opts ...talosclient.UpgradeOption) error
	CurrentInstallImage(ctx context.Context) (string, error)
}

type imagePullStream interface {
	Recv() (*machineapi.ImageServicePullResponse, error)
}

type lifecycleInstallStream interface {
	Recv() (*machineapi.LifecycleServiceUpgradeResponse, error)
}

// lifecycleOperations isolates generated streaming clients for focused tests.
// It remains adapter-private; ports continue to expose only normalized events.
type lifecycleOperations interface {
	Pull(context.Context, string) (imagePullStream, error)
	Upgrade(context.Context, string) (lifecycleInstallStream, error)
}

type machineryLifecycleOperations struct{ client *talosclient.Client }

func (o machineryLifecycleOperations) Pull(ctx context.Context, image string) (imagePullStream, error) {
	return o.client.ImageClient.Pull(ctx, &machineapi.ImageServicePullRequest{Containerd: systemContainerdInstance(), ImageRef: image})
}

func (o machineryLifecycleOperations) Upgrade(ctx context.Context, image string) (lifecycleInstallStream, error) {
	return o.client.LifecycleClient.Upgrade(ctx, &machineapi.LifecycleServiceUpgradeRequest{Containerd: systemContainerdInstance(), Source: &machineapi.InstallArtifactsSource{ImageName: image}})
}

func systemContainerdInstance() *commonapi.ContainerdInstance {
	return &commonapi.ContainerdInstance{Driver: commonapi.ContainerDriver_CRI, Namespace: commonapi.ContainerdNamespace_NS_SYSTEM}
}

type machineryNodeControlClient struct {
	client    *talosclient.Client
	lifecycle lifecycleOperations
}

func (c machineryNodeControlClient) Reboot(ctx context.Context, opts ...talosclient.RebootMode) error {
	return c.client.Reboot(ctx, opts...)
}

func (c machineryNodeControlClient) Shutdown(ctx context.Context, opts ...talosclient.ShutdownOption) error {
	return c.client.Shutdown(ctx, opts...)
}

func (c machineryNodeControlClient) Rollback(ctx context.Context) error {
	return c.client.Rollback(ctx)
}

func (c machineryNodeControlClient) Upgrade(ctx context.Context, opts ...talosclient.UpgradeOption) error {
	_, err := c.client.UpgradeWithOptions(ctx, opts...)
	return err
}

// deriveUpgradeImage combines a declared install image's registry/repository
// prefix with the currently-running Talos version tag. Talos does not
// rewrite the declared machine config's install.image field after an
// out-of-band `talosctl upgrade`, so the declared value alone can be stale —
// prefilling the Upgrade prompt with it verbatim risks a silent downgrade if
// an operator accepts the prefill without editing it. Digest references
// (containing "@") are left untouched, since there is no tag to replace.
func deriveUpgradeImage(declaredImage, runningTag string) string {
	if declaredImage == "" || runningTag == "" || strings.Contains(declaredImage, "@") {
		return declaredImage
	}
	lastSlash := strings.LastIndex(declaredImage, "/")
	lastSegment := declaredImage[lastSlash+1:]
	if colon := strings.LastIndex(lastSegment, ":"); colon >= 0 {
		return declaredImage[:lastSlash+1+colon] + ":" + runningTag
	}
	return declaredImage + ":" + runningTag
}

// The v1.13.3 machinery module does not expose ImageFactorySchematic. Until
// t9s upgrades that SDK, ExtensionStatus's schematic metadata is the supported
// compatibility source for a live schematic-preserving installer suggestion.
type installImageLookup interface {
	declaredInstallImage(context.Context) (string, error)
	schematicMetadata(context.Context) (string, string, error)
	platform(context.Context) (string, error)
	runningTalosVersion(context.Context) (string, error)
}

type machineryInstallImageLookup struct{ client *talosclient.Client }

func (c machineryNodeControlClient) CurrentInstallImage(ctx context.Context) (string, error) {
	return currentInstallImage(ctx, machineryInstallImageLookup{client: c.client})
}

func currentInstallImage(ctx context.Context, lookup installImageLookup) (string, error) {
	// ExtensionStatus supplies the factory URL and live schematic ID, while
	// PlatformMetadata supplies the canonical Image Factory installer flavor.
	author, schematicID, _ := lookup.schematicMetadata(ctx)
	platform, _ := lookup.platform(ctx)
	declaredImage, declaredErr := lookup.declaredInstallImage(ctx)
	runningTag, versionErr := lookup.runningTalosVersion(ctx)
	if versionErr == nil {
		if image := deriveSchematicInstallerImage(parseSchematicFactoryURL(author), platform, schematicID, runningTag); image != "" {
			return image, nil
		}
		if declaredImage != "" {
			return deriveUpgradeImage(declaredImage, runningTag), nil
		}
	}
	if declaredImage != "" {
		return declaredImage, nil
	}
	if declaredErr != nil {
		return "", declaredErr
	}

	return "", nil
}

func (l machineryInstallImageLookup) declaredInstallImage(ctx context.Context) (string, error) {
	cfg, err := safe.StateGet[*talosconfig.MachineConfig](
		ctx, l.client.COSI,
		resource.NewMetadata(talosconfig.NamespaceName, talosconfig.MachineConfigType, talosconfig.ActiveID, resource.VersionUndefined),
	)
	if err != nil {
		return "", err
	}

	return cfg.Provider().Machine().Install().Image(), nil
}

func (l machineryInstallImageLookup) schematicMetadata(ctx context.Context) (string, string, error) {
	if extensions, listErr := safe.StateListAll[*runtimeresource.ExtensionStatus](ctx, l.client.COSI); listErr == nil {
		for extension := range extensions.All() {
			if extension.TypedSpec().Metadata.Name == "schematic" {
				return extension.TypedSpec().Metadata.Author, extension.TypedSpec().Metadata.Version, nil
			}
		}
	}

	return "", "", nil
}

func (l machineryInstallImageLookup) platform(ctx context.Context) (string, error) {
	metadata, err := safe.StateGet[*runtimeresource.PlatformMetadata](
		ctx, l.client.COSI,
		resource.NewMetadata(runtimeresource.NamespaceName, runtimeresource.PlatformMetadataType, runtimeresource.PlatformMetadataID, resource.VersionUndefined),
	)
	if err != nil {
		return "", err
	}

	return metadata.TypedSpec().Platform, nil
}

func (l machineryInstallImageLookup) runningTalosVersion(ctx context.Context) (string, error) {
	versionResponse, err := l.client.Version(ctx)
	if err != nil {
		return "", err
	}
	for _, message := range versionResponse.GetMessages() {
		if version := message.GetVersion(); version != nil {
			return version.GetTag(), nil
		}
	}

	return "", fmt.Errorf("Talos version response contained no version")
}

type nodeController struct{ client nodeControlClient }

const (
	upgradeDrainTimeout   = 5 * time.Minute
	upgradeReadyTimeout   = 5 * time.Minute
	upgradeCleanupTimeout = time.Minute
)

type upgradeStream struct {
	results chan ports.UpgradeResult
	cancel  context.CancelFunc
}

func (s *upgradeStream) Results() <-chan ports.UpgradeResult { return s.results }
func (s *upgradeStream) Cancel()                             { s.cancel() }

func newNodeController(client nodeControlClient) ports.NodeController {
	return &nodeController{client: client}
}

func (c *nodeController) Reboot(ctx context.Context, target string, mode ports.RebootMode) error {
	var opts []talosclient.RebootMode
	if mode == ports.RebootPowercycle {
		opts = append(opts, talosclient.WithPowerCycle)
	}
	if err := c.client.Reboot(talosclient.WithNode(ctx, target), opts...); err != nil {
		return fmt.Errorf("reboot %s: %w", target, err)
	}
	return nil
}

func (c *nodeController) Shutdown(ctx context.Context, target string, force bool) error {
	var opts []talosclient.ShutdownOption
	if force {
		opts = append(opts, talosclient.WithShutdownForce(true))
	}
	if err := c.client.Shutdown(talosclient.WithNode(ctx, target), opts...); err != nil {
		return fmt.Errorf("shutdown %s: %w", target, err)
	}
	return nil
}

func (c *nodeController) Rollback(ctx context.Context, target string) error {
	if err := c.client.Rollback(talosclient.WithNode(ctx, target)); err != nil {
		return fmt.Errorf("rollback %s: %w", target, err)
	}
	return nil
}

func (c *nodeController) Upgrade(ctx context.Context, target, image string) error {
	if err := c.client.Upgrade(talosclient.WithNode(ctx, target), talosclient.WithUpgradeImage(image)); err != nil {
		return fmt.Errorf("upgrade %s: %w", target, err)
	}
	return nil
}

func (c *nodeController) UpgradeStream(ctx context.Context, target, image string) ports.UpgradeStream {
	streamCtx, cancel := context.WithCancel(ctx)
	stream := &upgradeStream{results: make(chan ports.UpgradeResult, 1), cancel: cancel}
	go func() {
		defer close(stream.results)
		defer cancel()
		emit := func(result ports.UpgradeResult) bool { return sendUpgradeResult(streamCtx, stream.results, result) }
		if !emit(ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeChecking, Message: "checking Talos upgrade API"}}) {
			return
		}
		versioned, ok := c.client.(versionedUpgradeClient)
		if !ok {
			emit(ports.UpgradeResult{Err: errors.New("Talos client cannot determine upgrade API version"), Done: true})
			return
		}
		capabilityCtx, capabilityCancel := context.WithTimeout(streamCtx, upgradeCapabilityTimeout)
		version, versionErr := versioned.upgradeVersion(talosclient.WithNode(capabilityCtx, target))
		capabilityCancel()
		if versionErr != nil {
			emit(ports.UpgradeResult{Err: fmt.Errorf("check Talos upgrade API version: %w", versionErr), Done: true})
			return
		}
		lifecycleSupported, versionErr := lifecycleUpgradeAPISupported(version)
		if versionErr != nil {
			emit(ports.UpgradeResult{Err: versionErr, Done: true})
			return
		}
		if !lifecycleSupported {
			err := c.Upgrade(streamCtx, target, image)
			if err == nil && streamCtx.Err() != nil {
				err = streamCtx.Err()
			}
			if err == nil {
				emit(ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeComplete, Message: "legacy upgrade accepted"}, Done: true})
			} else {
				emit(ports.UpgradeResult{Err: err, Done: true})
			}
			return
		}
		lifecycle, ok := c.client.(lifecycleUpgradeClient)
		if !ok {
			emit(ports.UpgradeResult{Err: errors.New("Talos client does not support lifecycle upgrades"), Done: true})
			return
		}
		err := lifecycle.lifecycleUpgrade(talosclient.WithNode(streamCtx, target), image, func(event ports.UpgradeEvent) { emit(ports.UpgradeResult{Event: &event}) })
		if err == nil && streamCtx.Err() != nil {
			err = streamCtx.Err()
		}
		if err == nil {
			maintenanceClient, ok := c.client.(upgradeMaintenanceClient)
			if !ok {
				err = errors.New("Talos client does not support safe upgrade maintenance")
			} else {
				emit(ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeDraining, Message: "resolving Kubernetes node"}})
				maintenance, prepareErr := maintenanceClient.prepareUpgradeMaintenance(streamCtx, target)
				if prepareErr != nil {
					err = fmt.Errorf("prepare Kubernetes node maintenance: %w", prepareErr)
				} else {
					err = runSafeUpgradeMaintenance(streamCtx, maintenance, func(event ports.UpgradeEvent) { emit(ports.UpgradeResult{Event: &event}) })
				}
			}
		}
		if err == nil {
			emit(ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeComplete, Message: "upgrade complete"}, Done: true})
		} else {
			emit(ports.UpgradeResult{Err: err, Done: true})
		}
	}()
	return stream
}

func sendUpgradeResult(ctx context.Context, results chan<- ports.UpgradeResult, result ports.UpgradeResult) bool {
	select {
	case results <- result:
		return true
	case <-ctx.Done():
		return false
	}
}

func (c *nodeController) CurrentInstallImage(ctx context.Context, target string) (string, error) {
	image, err := c.client.CurrentInstallImage(talosclient.WithNode(ctx, target))
	if err != nil {
		return "", fmt.Errorf("current install image %s: %w", target, err)
	}
	return image, nil
}
func parseSchematicFactoryURL(author string) string {
	idx := strings.LastIndex(author, " (")
	if idx < 0 || !strings.HasSuffix(author, ")") {
		return ""
	}

	return author[idx+2 : len(author)-1]
}

func deriveSchematicInstallerImage(factoryURL, platform, schematic, tag string) string {
	factory, err := url.Parse(factoryURL)
	if err != nil || factory.Scheme != "https" || factory.Host == "" || factory.User != nil ||
		factory.RawQuery != "" || factory.Fragment != "" || factory.Opaque != "" ||
		(factory.Path != "" && factory.Path != "/") || factory.RawPath != "" ||
		!validInstallerPlatform(platform) || !validInstallerPathComponent(schematic) ||
		!validInstallerPathComponent(tag) {
		return ""
	}

	return factory.Host + "/" + platform + "-installer/" + schematic + ":" + tag
}

func validInstallerPlatform(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && character != '-' {
			return false
		}
	}

	return true
}

func validInstallerPathComponent(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && character != '-' && character != '.' {
			return false
		}
	}

	return true
}

func supportsLifecycleUpgradeAPI(raw string) bool {
	version, err := semver.Parse(strings.TrimPrefix(raw, "v"))
	if err != nil {
		return false
	}
	min, _ := semver.Parse("1.13.0-alpha.2")
	max, _ := semver.Parse("2.0.0")
	return version.GT(min) && version.LT(max)
}

func lifecycleUpgradeAPISupported(raw string) (bool, error) {
	version, err := semver.Parse(strings.TrimPrefix(raw, "v"))
	if err != nil {
		return false, fmt.Errorf("parse Talos version %q: %w", raw, err)
	}
	min, _ := semver.Parse("1.13.0-alpha.2")
	max, _ := semver.Parse("2.0.0")

	return version.GT(min) && version.LT(max), nil
}

func runSafeUpgradeMaintenance(ctx context.Context, maintenance upgradeMaintenance, progress func(ports.UpgradeEvent)) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	progress(ports.UpgradeEvent{Phase: ports.UpgradeDraining, Message: "cordoning Kubernetes node"})
	cordonedByT9S, err := maintenance.Cordon(ctx)
	if err != nil {
		return fmt.Errorf("cordon Kubernetes node: %w", err)
	}
	if cordonedByT9S {
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), upgradeCleanupTimeout)
			defer cancel()
			progress(ports.UpgradeEvent{Phase: ports.UpgradeUncordon, Message: "uncordoning Kubernetes node"})
			if err := maintenance.Uncordon(cleanupCtx); err != nil {
				progress(ports.UpgradeEvent{Phase: ports.UpgradeUncordon, Message: "uncordon failed; upgrade completed; node may remain cordoned"})
				if retErr != nil {
					retErr = errors.Join(retErr, fmt.Errorf("uncordon Kubernetes node: %w", err))
				}
			}
		}()
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	progress(ports.UpgradeEvent{Phase: ports.UpgradeDraining, Message: "draining Kubernetes node"})
	if err := maintenance.Drain(ctx, upgradeDrainTimeout, func(message string) {
		progress(ports.UpgradeEvent{Phase: ports.UpgradeDraining, Message: message})
	}); err != nil {
		return fmt.Errorf("drain Kubernetes node: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	progress(ports.UpgradeEvent{Phase: ports.UpgradeRebooting, Message: "rebooting Talos node"})
	if err := maintenance.Reboot(ctx); err != nil {
		return fmt.Errorf("reboot Talos node: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	progress(ports.UpgradeEvent{Phase: ports.UpgradeWaiting, Message: "waiting for Talos API"})
	if err := maintenance.WaitTalosReady(ctx, upgradeReadyTimeout); err != nil {
		return fmt.Errorf("wait for Talos API: %w", err)
	}
	progress(ports.UpgradeEvent{Phase: ports.UpgradeWaiting, Message: "waiting for Kubernetes node readiness"})
	if err := maintenance.WaitKubernetesReady(ctx, upgradeReadyTimeout); err != nil {
		return fmt.Errorf("wait for Kubernetes node readiness: %w", err)
	}

	return nil
}

type machineryUpgradeMaintenance struct {
	client    *talosclient.Client
	target    string
	nodeName  string
	clientset kubernetes.Interface
}

func (c machineryNodeControlClient) prepareUpgradeMaintenance(ctx context.Context, target string) (upgradeMaintenance, error) {
	clientset, err := kubernetesClientFromTalos(ctx, c.client)
	if err != nil {
		return nil, fmt.Errorf("fetch Kubernetes client from Talos: %w", err)
	}
	nodename, err := safe.StateGetByID[*k8sresource.Nodename](talosclient.WithNode(ctx, target), c.client.COSI, k8sresource.NodenameID)
	if err != nil {
		return nil, fmt.Errorf("resolve Kubernetes node name: %w", err)
	}

	return machineryUpgradeMaintenance{
		client:    c.client,
		target:    target,
		nodeName:  nodename.TypedSpec().Nodename,
		clientset: clientset,
	}, nil
}

func kubernetesClientFromTalos(ctx context.Context, client *talosclient.Client) (kubernetes.Interface, error) {
	kubeconfig, err := client.Kubeconfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch kubeconfig from Talos API: %w", err)
	}
	config, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parse Talos kubeconfig: %w", err)
	}
	restConfig, err := config.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build Kubernetes REST config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes clientset: %w", err)
	}

	return clientset, nil
}

func (m machineryUpgradeMaintenance) Cordon(ctx context.Context) (bool, error) {
	node, err := m.clientset.CoreV1().Nodes().Get(ctx, m.nodeName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get Kubernetes node %q: %w", m.nodeName, err)
	}
	if node.Spec.Unschedulable {
		return false, nil
	}
	if err := kubectldrain.RunCordonOrUncordon(&kubectldrain.Helper{Ctx: ctx, Client: m.clientset}, node, true); err != nil {
		return false, fmt.Errorf("cordon Kubernetes node %q: %w", m.nodeName, err)
	}

	return true, nil
}

func (m machineryUpgradeMaintenance) Drain(ctx context.Context, timeout time.Duration, progress func(string)) error {
	drainCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	helper := newDrainHelper(drainCtx, m.clientset, timeout)
	if progress != nil {
		helper.OnPodDeletionOrEvictionStarted = func(pod *corev1.Pod, usingEviction bool) {
			verb := "deleting"
			if usingEviction {
				verb = "evicting"
			}
			progress(fmt.Sprintf("%s pod %s/%s", verb, pod.Namespace, pod.Name))
		}
	}
	if err := kubectldrain.RunNodeDrain(helper, m.nodeName); err != nil {
		return fmt.Errorf("drain Kubernetes node %q: %w", m.nodeName, err)
	}

	return nil
}

func newDrainHelper(ctx context.Context, clientset kubernetes.Interface, timeout time.Duration) *kubectldrain.Helper {
	return &kubectldrain.Helper{Ctx: ctx, Client: clientset, Force: true, GracePeriodSeconds: -1, IgnoreAllDaemonSets: true, DeleteEmptyDirData: true, Timeout: timeout, Out: io.Discard, ErrOut: io.Discard}
}

func (m machineryUpgradeMaintenance) Reboot(ctx context.Context) error {
	return m.client.Reboot(talosclient.WithNode(ctx, m.target))
}

func (m machineryUpgradeMaintenance) WaitTalosReady(ctx context.Context, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for {
		if _, err := m.client.Version(talosclient.WithNode(waitCtx, m.target)); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("Talos API did not become ready: %w", errors.Join(waitCtx.Err(), lastErr))
		case <-time.After(time.Second):
		}
	}
}

func (m machineryUpgradeMaintenance) WaitKubernetesReady(ctx context.Context, timeout time.Duration) error {
	return k8swait.PollUntilContextTimeout(ctx, 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		node, err := m.clientset.CoreV1().Nodes().Get(ctx, m.nodeName, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("get Kubernetes node %q: %w", m.nodeName, err)
		}
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady {
				return condition.Status == corev1.ConditionTrue, nil
			}
		}

		return false, nil
	})
}

func (m machineryUpgradeMaintenance) Uncordon(ctx context.Context) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		node, err := m.clientset.CoreV1().Nodes().Get(ctx, m.nodeName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			lastErr = fmt.Errorf("get Kubernetes node %q: %w", m.nodeName, err)
		} else if !node.Spec.Unschedulable {
			return nil
		} else if err := kubectldrain.RunCordonOrUncordon(&kubectldrain.Helper{Ctx: ctx, Client: m.clientset}, node, false); err == nil {
			return nil
		} else {
			lastErr = fmt.Errorf("uncordon Kubernetes node %q: %w", m.nodeName, err)
		}
		if attempt < 2 {
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return lastErr
}
func (c machineryNodeControlClient) lifecycleUpgrade(ctx context.Context, image string, progress func(ports.UpgradeEvent)) error {
	lifecycle := c.lifecycle
	if lifecycle == nil {
		lifecycle = machineryLifecycleOperations{client: c.client}
	}
	pull, err := lifecycle.Pull(ctx, image)
	if err != nil {
		return fmt.Errorf("pull upgrade image: %w", err)
	}
	for {
		response, recvErr := pull.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return fmt.Errorf("pull upgrade image: %w", recvErr)
		}
		if progressResponse := response.GetPullProgress(); progressResponse != nil && progressResponse.GetProgress() != nil {
			layer := progressResponse.GetProgress()
			progress(ports.UpgradeEvent{Phase: ports.UpgradePulling, Message: "pulling upgrade image", Current: layer.GetOffset(), Total: layer.GetTotal()})
		}
	}
	stream, err := lifecycle.Upgrade(ctx, image)
	if err != nil {
		return err
	}
	progress(ports.UpgradeEvent{Phase: ports.UpgradeInstalling, Message: "installing Talos image"})
	for {
		response, recvErr := stream.Recv()
		if recvErr == io.EOF {
			return fmt.Errorf("lifecycle upgrade ended without exit code")
		}
		if recvErr != nil {
			return recvErr
		}
		if response.GetProgress() == nil {
			continue
		}
		switch value := response.GetProgress().GetResponse().(type) {
		case *machineapi.LifecycleServiceInstallProgress_Message:
			progress(ports.UpgradeEvent{Phase: ports.UpgradeInstalling, Message: value.Message})
		case *machineapi.LifecycleServiceInstallProgress_ExitCode:
			if value.ExitCode != 0 {
				return fmt.Errorf("lifecycle upgrade failed with exit code %d", value.ExitCode)
			}
			return nil
		}
	}
}
func (c machineryNodeControlClient) upgradeVersion(ctx context.Context) (string, error) {
	response, err := c.client.Version(ctx)
	if err != nil {
		return "", err
	}
	for _, message := range response.GetMessages() {
		if version := message.GetVersion(); version != nil {
			return version.GetTag(), nil
		}
	}
	return "", fmt.Errorf("Talos version response contained no version")
}
