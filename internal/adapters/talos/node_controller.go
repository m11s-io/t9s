package talos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/blang/semver/v4"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/m11s-io/t9s/internal/ports"
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

type upgradeMaintenanceClient interface {
	prepareUpgradeMaintenance(context.Context, string) (upgradeMaintenance, error)
}

type upgradeMaintenance interface {
	Cordon(context.Context) error
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

type machineryNodeControlClient struct{ client *talosclient.Client }

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

func (c machineryNodeControlClient) CurrentInstallImage(ctx context.Context) (string, error) {
	cfg, err := safe.StateGet[*talosconfig.MachineConfig](
		ctx, c.client.COSI,
		resource.NewMetadata(talosconfig.NamespaceName, talosconfig.MachineConfigType, talosconfig.ActiveID, resource.VersionUndefined),
	)
	if err != nil {
		return "", err
	}
	declaredImage := cfg.Provider().Machine().Install().Image()
	var schematicFactory, schematicFlavor, schematicID, schematicAuthor string
	if extensions, listErr := safe.StateListAll[*runtimeresource.ExtensionStatus](ctx, c.client.COSI); listErr == nil {
		for extension := range extensions.All() {
			if extension.TypedSpec().Metadata.Name == "schematic" {
				schematicID = extension.TypedSpec().Metadata.Version
				schematicAuthor = extension.TypedSpec().Metadata.Author
				break
			}
		}
		if schematicID != "" {
			schematicFlavor, schematicFactory = parseSchematicAuthor(schematicAuthor)
		}
	}

	versionResponse, err := c.client.Version(ctx)
	if err != nil {
		// The declared image is still a valid, if possibly stale, prefill —
		// don't fail the whole prompt just because the live version query
		// failed.
		return declaredImage, nil
	}
	for _, message := range versionResponse.GetMessages() {
		if version := message.GetVersion(); version != nil {
			if image := deriveSchematicInstallerImage(schematicFactory, schematicFlavor, schematicID, version.GetTag()); image != "" {
				return image, nil
			}
			return deriveUpgradeImage(declaredImage, version.GetTag()), nil
		}
	}
	return declaredImage, nil
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
		stream.results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeChecking, Message: "checking Talos upgrade API"}}
		versioned, ok := c.client.(versionedUpgradeClient)
		if !ok {
			stream.results <- ports.UpgradeResult{Err: errors.New("Talos client cannot determine upgrade API version"), Done: true}
			return
		}
		version, versionErr := versioned.upgradeVersion(streamCtx)
		if versionErr != nil {
			stream.results <- ports.UpgradeResult{Err: fmt.Errorf("check Talos upgrade API version: %w", versionErr), Done: true}
			return
		}
		lifecycleSupported, versionErr := lifecycleUpgradeAPISupported(version)
		if versionErr != nil {
			stream.results <- ports.UpgradeResult{Err: versionErr, Done: true}
			return
		}
		if !lifecycleSupported {
			err := c.Upgrade(streamCtx, target, image)
			if err == nil {
				stream.results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeComplete, Message: "legacy upgrade accepted"}, Done: true}
			} else {
				stream.results <- ports.UpgradeResult{Err: err, Done: true}
			}
			return
		}
		lifecycle, ok := c.client.(lifecycleUpgradeClient)
		if !ok {
			stream.results <- ports.UpgradeResult{Err: errors.New("Talos client does not support lifecycle upgrades"), Done: true}
			return
		}
		err := lifecycle.lifecycleUpgrade(talosclient.WithNode(streamCtx, target), image, func(event ports.UpgradeEvent) { stream.results <- ports.UpgradeResult{Event: &event} })
		if err == nil {
			maintenanceClient, ok := c.client.(upgradeMaintenanceClient)
			if !ok {
				err = errors.New("Talos client does not support safe upgrade maintenance")
			} else {
				stream.results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeDraining, Message: "resolving Kubernetes node"}}
				maintenance, prepareErr := maintenanceClient.prepareUpgradeMaintenance(streamCtx, target)
				if prepareErr != nil {
					err = fmt.Errorf("prepare Kubernetes node maintenance: %w", prepareErr)
				} else {
					err = runSafeUpgradeMaintenance(streamCtx, maintenance, func(event ports.UpgradeEvent) { stream.results <- ports.UpgradeResult{Event: &event} })
				}
			}
		}
		if err == nil {
			stream.results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeComplete, Message: "upgrade complete"}, Done: true}
		} else {
			stream.results <- ports.UpgradeResult{Err: err, Done: true}
		}
	}()
	return stream
}

func (c *nodeController) CurrentInstallImage(ctx context.Context, target string) (string, error) {
	image, err := c.client.CurrentInstallImage(talosclient.WithNode(ctx, target))
	if err != nil {
		return "", fmt.Errorf("current install image %s: %w", target, err)
	}
	return image, nil
}
func deriveSchematicInstallerImage(factory, flavor, schematic, tag string) string {
	if factory == "" || flavor == "" || schematic == "" || tag == "" {
		return ""
	}
	return factory + "/" + flavor + "-installer/" + schematic + ":" + tag
}
func parseSchematicAuthor(author string) (flavor, factory string) {
	idx := strings.LastIndex(author, " (")
	if idx < 0 {
		return author, "factory.talos.dev"
	}
	return author[:idx], strings.TrimSuffix(author[idx+2:], ")")
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
	progress(ports.UpgradeEvent{Phase: ports.UpgradeDraining, Message: "cordoning Kubernetes node"})
	if err := maintenance.Cordon(ctx); err != nil {
		return fmt.Errorf("cordon Kubernetes node: %w", err)
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), upgradeCleanupTimeout)
		defer cancel()
		progress(ports.UpgradeEvent{Phase: ports.UpgradeUncordon, Message: "uncordoning Kubernetes node"})
		if err := maintenance.Uncordon(cleanupCtx); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("uncordon Kubernetes node: %w", err))
		}
	}()

	progress(ports.UpgradeEvent{Phase: ports.UpgradeDraining, Message: "draining Kubernetes node"})
	if err := maintenance.Drain(ctx, upgradeDrainTimeout, func(message string) {
		progress(ports.UpgradeEvent{Phase: ports.UpgradeDraining, Message: message})
	}); err != nil {
		return fmt.Errorf("drain Kubernetes node: %w", err)
	}
	progress(ports.UpgradeEvent{Phase: ports.UpgradeRebooting, Message: "rebooting Talos node"})
	if err := maintenance.Reboot(ctx); err != nil {
		return fmt.Errorf("reboot Talos node: %w", err)
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

func (m machineryUpgradeMaintenance) Cordon(ctx context.Context) error {
	node, err := m.clientset.CoreV1().Nodes().Get(ctx, m.nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get Kubernetes node %q: %w", m.nodeName, err)
	}
	if err := kubectldrain.RunCordonOrUncordon(&kubectldrain.Helper{Ctx: ctx, Client: m.clientset}, node, true); err != nil {
		return fmt.Errorf("cordon Kubernetes node %q: %w", m.nodeName, err)
	}

	return nil
}

func (m machineryUpgradeMaintenance) Drain(ctx context.Context, timeout time.Duration, progress func(string)) error {
	drainCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	helper := &kubectldrain.Helper{
		Ctx:                 drainCtx,
		Client:              m.clientset,
		Force:               true,
		GracePeriodSeconds:  -1,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		Timeout:             timeout,
	}
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
	node, err := m.clientset.CoreV1().Nodes().Get(ctx, m.nodeName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get Kubernetes node %q: %w", m.nodeName, err)
	}
	if err := kubectldrain.RunCordonOrUncordon(&kubectldrain.Helper{Ctx: ctx, Client: m.clientset}, node, false); err != nil {
		return fmt.Errorf("uncordon Kubernetes node %q: %w", m.nodeName, err)
	}

	return nil
}
func (c machineryNodeControlClient) lifecycleUpgrade(ctx context.Context, image string, progress func(ports.UpgradeEvent)) error {
	pull, err := c.client.ImageClient.Pull(ctx, &machineapi.ImageServicePullRequest{ImageRef: image})
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
	stream, err := c.client.LifecycleClient.Upgrade(ctx, &machineapi.LifecycleServiceUpgradeRequest{Source: &machineapi.InstallArtifactsSource{ImageName: image}})
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
