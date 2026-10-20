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
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName == "" || !podReady(pod) {
			continue
		}
		for _, container := range pod.Spec.Containers {
			component := pod.Labels["component"]
			if component == "" && pod.Labels["k8s-app"] == "kube-proxy" {
				component = "kube-proxy"
			}
			if component == "kube-apiserver" || component == "kube-controller-manager" || component == "kube-scheduler" || component == "kube-proxy" {
				if controlPlane[pod.Spec.NodeName] == nil {
					controlPlane[pod.Spec.NodeName] = map[string]string{}
				}
				controlPlane[pod.Spec.NodeName][component] = imageVersion(container.Image)
			}
		}
	}
	var gateway string
	for _, node := range nodeList.Items {
		role := domain.NodeRoleWorker
		if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
			role = domain.NodeRoleControl
		} else if _, ok := node.Labels["node-role.kubernetes.io/master"]; ok {
			role = domain.NodeRoleControl
		}
		talosNode, ok := matchTalosNode(talosNodes.Nodes, node)
		if !ok || talosNode.Health != domain.HealthHealthy {
			return inventory, errors.New("Kubernetes node does not map uniquely to a Talos node")
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
		components := controlPlane[node.Name]
		if role == domain.NodeRoleControl {
			if components["kube-apiserver"] == "" || components["kube-controller-manager"] == "" || components["kube-scheduler"] == "" || components["kube-proxy"] == "" {
				return inventory, errors.New("control-plane component versions are incomplete")
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
			TalosVersion: talosVersion.String(), KubeletVersion: strings.TrimPrefix(node.Status.NodeInfo.KubeletVersion, "v"),
			KubeletImage: kubeletImage, TargetKubeletImage: targetImage,
			ConfigFingerprint: hex.EncodeToString(configHash[:]), Ready: nodeReady(node), Unschedulable: node.Spec.Unschedulable,
		}
		if role == domain.NodeRoleControl {
			nodePlan.APIServerVersion = components["kube-apiserver"]
			nodePlan.ControllerManagerVersion = components["kube-controller-manager"]
			nodePlan.SchedulerVersion = components["kube-scheduler"]
			nodePlan.ProxyVersion = components["kube-proxy"]
		}
		inventory.Nodes = append(inventory.Nodes, nodePlan)
	}
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
		if len(fields) == 2 && strings.TrimSuffix(fields[0], ":") == "Client" {
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
	checks := []struct{ verb, resource, subresource, namespace string }{
		{"get", "namespaces", "", ""}, {"list", "nodes", "", ""}, {"get", "nodes", "", ""},
		{"update", "nodes", "", ""}, {"list", "pods", "", ""}, {"create", "pods", "eviction", ""},
	}
	result := make(map[string]bool, len(checks))
	for _, check := range checks {
		request := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb: check.verb, Resource: check.resource, Subresource: check.subresource, Namespace: check.namespace,
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
