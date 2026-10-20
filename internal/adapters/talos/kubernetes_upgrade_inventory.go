package talos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blang/semver/v4"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/compatibility"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	configresource "github.com/siderolabs/talos/pkg/machinery/resources/config"
	k8sresource "github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsclient "k8s.io/client-go/kubernetes/typed/apps/v1"
	authorizationclient "k8s.io/client-go/kubernetes/typed/authorization/v1"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

type kubernetesUpgradeBackend struct {
	talos       *talosclient.Client
	talosConfig *clientconfig.Config
	contextName string
	nodes       ports.NodeReader
}

var _ ports.KubernetesUpgradeBackend = (*kubernetesUpgradeBackend)(nil)

func newKubernetesUpgradeBackend(client *talosclient.Client, config *clientconfig.Config, contextName string, nodes ports.NodeReader) ports.KubernetesUpgradeBackend {
	return &kubernetesUpgradeBackend{talos: client, talosConfig: config, contextName: contextName, nodes: nodes}
}

func (b *kubernetesUpgradeBackend) Inspect(ctx context.Context, target string) (domain.KubernetesUpgradeInventory, error) {
	var inventory domain.KubernetesUpgradeInventory
	if b == nil || b.talos == nil || b.talosConfig == nil || b.nodes == nil {
		return inventory, errors.New("Kubernetes upgrade capability is unavailable")
	}
	talosContext := b.talosConfig.Contexts[b.contextName]
	if talosContext == nil || len(talosContext.Endpoints) == 0 {
		return inventory, errors.New("Talos context identity is incomplete")
	}
	identity, err := inspectTalosCLI(ctx)
	if err != nil {
		return inventory, err
	}
	kubeBytes, err := b.talos.Kubeconfig(ctx)
	if err != nil {
		return inventory, errors.New("unable to obtain Kubernetes credentials from Talos")
	}
	if err = validateTalosKubeconfig(kubeBytes); err != nil {
		clear(kubeBytes)
		return inventory, errors.New("Talos returned an unsupported Kubernetes credential file")
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeBytes)
	clear(kubeBytes)
	if err != nil {
		return inventory, errors.New("unable to build the Talos Kubernetes connection")
	}
	core, err := coreclient.NewForConfig(restConfig)
	if err != nil {
		return inventory, errors.New("unable to connect to the Kubernetes cluster")
	}
	authorization, err := authorizationclient.NewForConfig(restConfig)
	if err != nil {
		return inventory, errors.New("unable to connect to Kubernetes authorization")
	}
	apps, err := appsclient.NewForConfig(restConfig)
	if err != nil {
		return inventory, errors.New("unable to connect to Kubernetes apps API")
	}

	ctxInfo, err := core.Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil || ctxInfo.UID == "" {
		return inventory, errors.New("unable to verify Kubernetes cluster identity")
	}
	nodeList, err := core.Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodeList.Items) == 0 {
		return inventory, errors.New("unable to discover Kubernetes nodes")
	}
	talosNodes, err := b.nodes.List(ctx)
	if err != nil {
		return inventory, errors.New("unable to discover Talos nodes")
	}
	pods, err := core.Pods("kube-system").List(ctx, metav1.ListOptions{})
	if err != nil {
		return inventory, errors.New("unable to inspect Kubernetes system components")
	}
	controlPlane := map[string]map[string]string{}
	proxyPodNodes := map[string]struct{}{}
	proxyPodVersions := make(map[string]string)
	proxyPods := make([]corev1.Pod, 0)
	proxyPodsFound := false
	for _, pod := range pods.Items {
		component := pod.Labels["k8s-app"]
		if component == "" {
			component = pod.Labels["component"]
		}
		if component == "kube-proxy" {
			proxyPodsFound = true
			if pod.DeletionTimestamp != nil {
				continue
			}
			if pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName == "" || !podReady(pod) {
				return inventory, fmt.Errorf("%w: a kube-proxy pod is not ready", domain.ErrKubernetesUpgradeNotReady)
			}
			if _, exists := proxyPodNodes[pod.Spec.NodeName]; exists {
				return inventory, errors.New("multiple ready kube-proxy pods are scheduled on a node")
			}
			foundContainer := false
			for _, container := range pod.Spec.Containers {
				if container.Name == component {
					version := imageVersion(container.Image)
					if version == "" {
						return inventory, errors.New("a kube-proxy image version is unknown")
					}
					proxyPodVersions[pod.Spec.NodeName] = version
					foundContainer = true
				}
			}
			if !foundContainer {
				return inventory, errors.New("a kube-proxy container version is unavailable")
			}
			proxyPodNodes[pod.Spec.NodeName] = struct{}{}
			proxyPods = append(proxyPods, *pod.DeepCopy())
			continue
		}
		if component != "kube-apiserver" && component != "kube-controller-manager" && component != "kube-scheduler" {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName == "" || !podReady(pod) {
			continue
		}
		for _, container := range pod.Spec.Containers {
			if container.Name == component {
				if controlPlane[pod.Spec.NodeName] == nil {
					controlPlane[pod.Spec.NodeName] = map[string]string{}
				}
				controlPlane[pod.Spec.NodeName][component] = imageVersion(container.Image)
			}
		}
	}
	var gateway string
	var proxyEnabled, proxyConfigKnown bool
	inventory.TargetProxyImage = fmt.Sprintf("%s:v%s", constants.KubeProxyImage, strings.TrimPrefix(target, "v"))
	for _, node := range nodeList.Items {
		role := domain.NodeRoleWorker
		if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
			role = domain.NodeRoleControl
		} else if _, ok := node.Labels["node-role.kubernetes.io/master"]; ok {
			role = domain.NodeRoleControl
		}
		talosNode, ok := matchTalosNode(talosNodes.Nodes, node)
		if !ok {
			return inventory, errors.New("Kubernetes node does not map uniquely to a Talos node")
		}
		if talosNode.Health != domain.HealthHealthy {
			return inventory, fmt.Errorf("%w: Talos node %s is not healthy", domain.ErrKubernetesUpgradeNotReady, node.Name)
		}
		nodeCtx := talosclient.WithNode(ctx, talosNode.Target())
		versionResponse, versionErr := b.talos.Version(nodeCtx)
		if versionErr != nil || len(versionResponse.Messages) != 1 || versionResponse.Messages[0].Version == nil {
			return inventory, errors.New("unable to read Talos version for every node")
		}
		kubeletSpec, specErr := safe.StateGet[*k8sresource.KubeletSpec](nodeCtx, b.talos.COSI, resource.NewMetadata(k8sresource.NamespaceName, k8sresource.KubeletSpecType, "kubelet", resource.VersionUndefined))
		if specErr != nil {
			return inventory, errors.New("unable to read kubelet image for every node")
		}
		activeConfig, configErr := safe.StateGetByID[*configresource.MachineConfig](nodeCtx, b.talos.COSI, configresource.ActiveID)
		if configErr != nil {
			return inventory, errors.New("unable to read active Talos configuration")
		}
		configBytes, configErr := activeConfig.Provider().EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
		if configErr != nil {
			return inventory, errors.New("unable to fingerprint active Talos configuration")
		}
		configHash := sha256.Sum256(configBytes)
		clear(configBytes)
		nodeProxyEnabled, nodeProxyConfigKnown := false, false
		nodeProxyImage := ""
		if role == domain.NodeRoleControl {
			proxyConfig := activeConfig.Provider().K8sProxyConfig()
			if proxyConfig == nil {
				return inventory, errors.New("Talos kube-proxy configuration is unavailable")
			}
			nodeProxyEnabled, nodeProxyConfigKnown = proxyConfig.Enabled(), true
			nodeProxyImage = proxyConfig.Image()
			if proxyConfigKnown && proxyEnabled != nodeProxyEnabled {
				return inventory, errors.New("Talos control-plane nodes disagree about kube-proxy configuration")
			}
			proxyEnabled, proxyConfigKnown = nodeProxyEnabled, true
		}
		components := controlPlane[node.Name]
		if role == domain.NodeRoleControl {
			var missing []string
			for _, component := range []string{"kube-apiserver", "kube-controller-manager", "kube-scheduler"} {
				if components[component] == "" {
					missing = append(missing, component)
				}
			}
			if len(missing) > 0 {
				return inventory, fmt.Errorf("%w: control-plane components are not ready on node %q (missing: %s)", domain.ErrKubernetesUpgradeNotReady, node.Name, strings.Join(missing, ", "))
			}
			if gateway == "" {
				gateway = talosNode.Target()
			}
		}
		talosVersion, parseErr := compatibility.ParseTalosVersion(versionResponse.Messages[0].Version)
		if parseErr != nil {
			return inventory, errors.New("Talos version is unsupported")
		}
		k8sVersion, parseErr := compatibility.ParseKubernetesVersion(target)
		if parseErr != nil || talosVersion == nil || k8sVersion == nil || k8sVersion.SupportedWith(talosVersion) != nil {
			return inventory, errors.New("target Kubernetes version is incompatible with a Talos node")
		}
		kubeletImage := kubeletSpec.TypedSpec().Image
		targetImage, imageErr := targetKubeletImage(target, kubeletImage)
		if imageErr != nil {
			return inventory, errors.New("current kubelet image form is unsupported")
		}
		nodePlan := domain.KubernetesUpgradeNode{
			Name: node.Name, UID: string(node.UID), TalosID: talosNode.ID, TalosAddress: talosNode.Target(), Role: role,
			ProxyEnabled: nodeProxyEnabled, ProxyConfigKnown: nodeProxyConfigKnown, ProxyConfigImage: nodeProxyImage,
			TalosVersion: talosVersion.String(), KubeletVersion: strings.TrimPrefix(node.Status.NodeInfo.KubeletVersion, "v"),
			KubeletImage: kubeletImage, TargetKubeletImage: targetImage,
			ConfigFingerprint: hex.EncodeToString(configHash[:]), Ready: nodeReady(node), Unschedulable: node.Spec.Unschedulable,
		}
		if role == domain.NodeRoleControl {
			nodePlan.APIServerVersion = components["kube-apiserver"]
			nodePlan.ControllerManagerVersion = components["kube-controller-manager"]
			nodePlan.SchedulerVersion = components["kube-scheduler"]
		}
		inventory.Nodes = append(inventory.Nodes, nodePlan)
	}
	if !proxyConfigKnown {
		return inventory, errors.New("Talos kube-proxy configuration is unknown")
	}
	proxyDaemonSet, proxyDaemonSetErr := apps.DaemonSets("kube-system").Get(ctx, "kube-proxy", metav1.GetOptions{})
	var proxyVersions []domain.KubernetesProxyNodeVersion
	if proxyEnabled {
		if proxyDaemonSetErr != nil {
			return inventory, errors.New("Talos kube-proxy is enabled but its DaemonSet is unavailable")
		}
		daemonSetVersion, daemonSetErr := validateKubeProxyDaemonSet(proxyDaemonSet, int32(len(nodeList.Items)))
		if daemonSetErr != nil {
			return inventory, daemonSetErr
		}
		inventory.ProxyRolloutComplete = proxyDaemonSet.Status.ObservedGeneration >= proxyDaemonSet.Generation && proxyDaemonSet.Status.UpdatedNumberScheduled == proxyDaemonSet.Status.DesiredNumberScheduled && proxyDaemonSet.Status.NumberReady == proxyDaemonSet.Status.DesiredNumberScheduled && proxyDaemonSet.Status.NumberUnavailable == 0
		if len(proxyPodNodes) != len(nodeList.Items) {
			return inventory, fmt.Errorf("%w: a ready kube-proxy pod is missing", domain.ErrKubernetesUpgradeNotReady)
		}
		for _, pod := range proxyPods {
			owner := metav1.GetControllerOf(&pod)
			if owner == nil || owner.Kind != "DaemonSet" || owner.Name != proxyDaemonSet.Name || owner.UID != proxyDaemonSet.UID {
				return inventory, errors.New("a kube-proxy pod is not owned by the Talos kube-proxy DaemonSet")
			}
		}
		for _, node := range nodeList.Items {
			if _, ok := proxyPodNodes[node.Name]; !ok {
				return inventory, fmt.Errorf("%w: a ready kube-proxy pod is missing", domain.ErrKubernetesUpgradeNotReady)
			}
			version := proxyPodVersions[node.Name]
			if version == "" {
				return inventory, errors.New("a kube-proxy pod version is unknown")
			}
			proxyVersions = append(proxyVersions, domain.KubernetesProxyNodeVersion{NodeUID: string(node.UID), Name: node.Name, Version: version})
		}
		if daemonSetVersion != strings.TrimPrefix(target, "v") {
			foundTemplateVersion := false
			for _, proxy := range proxyVersions {
				if proxy.Version == daemonSetVersion {
					foundTemplateVersion = true
					break
				}
			}
			if !foundTemplateVersion {
				return inventory, errors.New("kube-proxy DaemonSet template does not match the source or target rollout")
			}
		}
	} else {
		if proxyDaemonSetErr == nil || !apierrors.IsNotFound(proxyDaemonSetErr) {
			return inventory, errors.New("Talos kube-proxy is disabled but its DaemonSet could not be ruled out")
		}
		if proxyPodsFound {
			return inventory, errors.New("kube-proxy pods exist while Talos proxy configuration is disabled")
		}
		inventory.ProxyRolloutComplete = true
	}
	sort.Slice(proxyVersions, func(i, j int) bool { return proxyVersions[i].NodeUID < proxyVersions[j].NodeUID })
	inventory.ProxyEnabled = proxyEnabled
	inventory.ProxyConfigKnown = true
	inventory.ProxyVersions = proxyVersions
	if gateway == "" {
		return inventory, errors.New("cluster has no control-plane gateway")
	}
	controlTargets := make([]string, 0)
	for _, node := range inventory.Nodes {
		if node.Role == domain.NodeRoleControl {
			controlTargets = append(controlTargets, node.TalosAddress)
		}
	}
	etcd, err := newEtcdReader(machineryEtcdClient{client: b.talos}).List(ctx, controlTargets)
	if err != nil || len(etcd.Members) < len(controlTargets)/2+1 {
		return inventory, errors.New("etcd quorum is unavailable")
	}
	for _, member := range etcd.Members {
		if !member.StatusKnown || len(member.Alarms) != 0 {
			return inventory, errors.New("etcd status is unknown or unhealthy")
		}
	}
	permissions, err := inspectKubernetesPermissions(ctx, core, authorization)
	if err != nil {
		return inventory, errors.New("unable to verify required Kubernetes permissions")
	}
	for _, allowed := range permissions {
		if !allowed {
			return inventory, errors.New("Kubernetes credentials lack permissions required for upgrade")
		}
	}
	inventory.ClusterUID = string(ctxInfo.UID)
	inventory.TalosClusterID = talosContext.Cluster
	if inventory.TalosClusterID == "" {
		inventory.TalosClusterID = b.contextName
	}
	inventory.Gateway = gateway
	inventory.CLI = identity
	inventory.Permissions = permissions
	inventory.Healthy = len(inventory.Nodes) == len(nodeList.Items)
	for _, node := range inventory.Nodes {
		inventory.Healthy = inventory.Healthy && node.Ready
	}
	inventory.ObservedAt = time.Now()
	return inventory, nil
}

func validateKubeProxyDaemonSet(daemonSet *appsv1.DaemonSet, nodeCount int32) (string, error) {
	if daemonSet == nil || daemonSet.UID == "" || daemonSet.Name != "kube-proxy" || daemonSet.Namespace != "kube-system" || daemonSet.Spec.Selector == nil {
		return "", errors.New("kube-proxy DaemonSet identity is incomplete")
	}
	if len(daemonSet.Spec.Selector.MatchLabels) != 2 || len(daemonSet.Spec.Selector.MatchExpressions) != 0 || daemonSet.Spec.Selector.MatchLabels["k8s-app"] != "kube-proxy" || daemonSet.Spec.Selector.MatchLabels["tier"] != "node" || daemonSet.Spec.Template.Labels["k8s-app"] != "kube-proxy" || daemonSet.Spec.Template.Labels["tier"] != "node" {
		return "", errors.New("kube-proxy DaemonSet selector does not match Talos component labels")
	}
	desired := daemonSet.Status.DesiredNumberScheduled
	if nodeCount <= 0 || desired != nodeCount || daemonSet.Status.ObservedGeneration < daemonSet.Generation || daemonSet.Status.UpdatedNumberScheduled > desired || daemonSet.Status.NumberReady != desired || daemonSet.Status.NumberUnavailable != 0 {
		return "", fmt.Errorf("%w: kube-proxy DaemonSet is not ready on every node", domain.ErrKubernetesUpgradeNotReady)
	}
	for _, container := range daemonSet.Spec.Template.Spec.Containers {
		if container.Name == "kube-proxy" {
			version := imageVersion(container.Image)
			if version != "" {
				return version, nil
			}
		}
	}
	return "", errors.New("kube-proxy DaemonSet image version is unknown")
}

func validateTalosKubeconfig(data []byte) error {
	parsed, err := clientcmd.Load(data)
	if err != nil || parsed.CurrentContext == "" {
		return errors.New("invalid kubeconfig")
	}
	ctx := parsed.Contexts[parsed.CurrentContext]
	if ctx == nil || parsed.Clusters[ctx.Cluster] == nil || parsed.AuthInfos[ctx.AuthInfo] == nil {
		return errors.New("incomplete kubeconfig")
	}
	cluster := parsed.Clusters[ctx.Cluster]
	user := parsed.AuthInfos[ctx.AuthInfo]
	if !strings.HasPrefix(cluster.Server, "https://") || cluster.InsecureSkipTLSVerify || user.Exec != nil || user.AuthProvider != nil {
		return errors.New("unsupported kubeconfig authentication")
	}
	if len(user.ClientCertificateData) == 0 || len(user.ClientKeyData) == 0 || len(cluster.CertificateAuthorityData) == 0 {
		return errors.New("Talos kubeconfig must contain inline client and cluster certificates")
	}
	return nil
}

func inspectTalosCLI(ctx context.Context) (domain.TalosCLIIdentity, error) {
	var identity domain.TalosCLIIdentity
	path, err := exec.LookPath("talosctl")
	if err != nil {
		return identity, errors.New("talosctl v1.14.4 must be installed and available on PATH")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return identity, errors.New("unable to resolve talosctl path")
	}
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	versionCommand := exec.CommandContext(versionCtx, path, "version", "--client")
	var output boundedVersionOutput
	versionCommand.Stdout = &output
	versionCommand.Stderr = io.Discard
	if err := versionCommand.Run(); err != nil || !clientVersionMatches(output.String(), "v1.14.4") {
		return identity, errors.New("talosctl v1.14.4 is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return identity, errors.New("unable to inspect talosctl executable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 512<<20 {
		return identity, errors.New("talosctl executable has an invalid file type or size")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, info.Size()+1)); err != nil {
		return identity, errors.New("unable to fingerprint talosctl executable")
	}
	return domain.TalosCLIIdentity{Path: path, Version: "v1.14.4", SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

type boundedVersionOutput struct{ bytes.Buffer }

func (b *boundedVersionOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > 32*1024 {
		return 0, io.ErrShortWrite
	}
	return b.Buffer.Write(data)
}

func clientVersionMatches(output, expected string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 && (strings.TrimSuffix(fields[0], ":") == "Tag" || strings.TrimSuffix(fields[0], ":") == "Client") {
			return fields[1] == expected
		}
	}
	return false
}

func matchTalosNode(nodes []domain.NodeSnapshot, kubeNode corev1.Node) (domain.NodeSnapshot, bool) {
	var match domain.NodeSnapshot
	count := 0
	addresses := map[string]struct{}{}
	for _, address := range kubeNode.Status.Addresses {
		addresses[address.Address] = struct{}{}
	}
	for _, node := range nodes {
		matches := node.Name == kubeNode.Name
		for _, address := range node.Addresses {
			if _, ok := addresses[address]; ok {
				matches = true
			}
		}
		if matches {
			match, count = node, count+1
		}
	}
	return match, count == 1
}

func nodeReady(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func podReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func imageVersion(image string) string {
	if at := strings.IndexByte(image, '@'); at >= 0 {
		image = image[:at]
	}
	separator := strings.LastIndexByte(image, ':')
	if separator < strings.LastIndexByte(image, '/') || separator < 0 {
		return ""
	}
	return strings.TrimPrefix(image[separator+1:], "v")
}

func targetKubeletImage(target, current string) (string, error) {
	if !strings.HasPrefix(current, constants.KubeletImage+":") || strings.Contains(current, "@") {
		return "", errors.New("unsupported kubelet image reference")
	}
	version := imageVersion(current)
	suffix := ""
	for _, candidate := range []string{"-fat", "-slim"} {
		if strings.HasSuffix(version, candidate) {
			suffix = candidate
			break
		}
	}
	baseVersion := strings.TrimSuffix(version, suffix)
	parsed, err := semver.ParseTolerant(baseVersion)
	if err != nil || parsed.Major == 0 {
		return "", errors.New("unsupported kubelet image tag")
	}
	return fmt.Sprintf("%s:v%s%s", constants.KubeletImage, strings.TrimPrefix(target, "v"), suffix), nil
}

func inspectKubernetesPermissions(ctx context.Context, core coreclient.CoreV1Interface, authorization authorizationclient.AuthorizationV1Interface) (map[string]bool, error) {
	checks := []struct{ verb, resource, subresource, namespace, group string }{
		{"get", "namespaces", "", "", ""}, {"list", "nodes", "", "", ""}, {"get", "nodes", "", "", ""},
		{"list", "pods", "", "", ""},
		{"get", "daemonsets", "", "kube-system", "apps"},
	}
	result := make(map[string]bool, len(checks))
	for _, check := range checks {
		request := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb: check.verb, Resource: check.resource, Subresource: check.subresource, Namespace: check.namespace, Group: check.group,
			}},
		}
		response, err := authorization.SelfSubjectAccessReviews().Create(ctx, request, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		result[check.verb+":"+check.resource+":"+check.subresource] = response.Status.Allowed
	}
	return result, nil
}
