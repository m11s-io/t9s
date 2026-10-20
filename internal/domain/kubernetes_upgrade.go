package domain

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/blang/semver/v4"
)

var ErrKubernetesUpgradeAssessment = errors.New("Kubernetes upgrade assessment failed")

// ErrKubernetesUpgradeNotReady marks an observation that may settle after a restart.
var ErrKubernetesUpgradeNotReady = errors.New("cluster readiness has not settled")

type TalosCLIIdentity struct {
	Path    string
	Version string
	SHA256  string
}

type KubernetesUpgradeNode struct {
	Name                     string
	UID                      string
	TalosID                  string
	TalosAddress             string
	Role                     NodeRole
	TalosVersion             string
	ProxyEnabled             bool
	ProxyConfigKnown         bool
	ProxyConfigImage         string
	APIServerVersion         string
	ControllerManagerVersion string
	SchedulerVersion         string
	KubeletVersion           string
	KubeletImage             string
	TargetKubeletImage       string
	ConfigFingerprint        string
	Ready                    bool
	Unschedulable            bool
}

type KubernetesUpgradeInventory struct {
	ClusterUID           string
	TalosClusterID       string
	Gateway              string
	CLI                  TalosCLIIdentity
	Nodes                []KubernetesUpgradeNode
	ProxyEnabled         bool
	ProxyConfigKnown     bool
	TargetProxyImage     string
	ProxyRolloutComplete bool
	// ProxyVersions are keyed by node identity so the assessment detects per-node drift.
	ProxyVersions      []KubernetesProxyNodeVersion
	Manifests          []string
	ManifestsConverged bool
	Permissions        map[string]bool
	Healthy            bool
	ObservedAt         time.Time
}

// KubernetesProxyNodeVersion records an observed kube-proxy pod by Kubernetes node.
type KubernetesProxyNodeVersion struct {
	NodeUID string
	Name    string
	Version string
}

type KubernetesUpgradeNodePlan struct {
	ClusterUID         string
	Name               string
	UID                string
	TalosID            string
	TalosAddress       string
	Role               NodeRole
	TalosVersion       string
	SourceVersion      string
	TargetVersion      string
	SourceKubeletImage string
	TargetKubeletImage string
	ConfigFingerprint  string
	OriginallyCordoned bool
	Skip               bool
}

type KubernetesUpgradePlan struct {
	ClusterUID       string
	TalosClusterID   string
	Gateway          string
	CLI              TalosCLIIdentity
	SourceVersion    string
	TargetVersion    string
	ProxyEnabled     bool
	TargetProxyImage string
	// ProxyVersions is ordered by node UID to keep the plan fingerprint stable.
	ProxyVersions    []KubernetesProxyNodeVersion
	Nodes            []KubernetesUpgradeNodePlan
	Manifests        []string
	SkipControlPlane bool
	Fingerprint      string
}

type KubernetesUpgradeStage string

const (
	KubernetesUpgradeSnapshot     KubernetesUpgradeStage = "snapshot"
	KubernetesUpgradeControlPlane KubernetesUpgradeStage = "control-plane"
	KubernetesUpgradeKubeProxy    KubernetesUpgradeStage = "kube-proxy"
	KubernetesUpgradeManifests    KubernetesUpgradeStage = "manifests"
	KubernetesUpgradePrePull      KubernetesUpgradeStage = "kubelet-pre-pull"
	KubernetesUpgradeKubelet      KubernetesUpgradeStage = "kubelet"
	KubernetesUpgradeReadiness    KubernetesUpgradeStage = "readiness"
	KubernetesUpgradeVerify       KubernetesUpgradeStage = "verify"
)

type KubernetesUpgradeEvent struct {
	Stage   KubernetesUpgradeStage
	NodeUID string
	Status  string
}

type KubernetesUpgradeNodeResult struct {
	NodeUID           string
	Outcome           OperationOutcome
	LeftCordoned      bool
	Message           string
	ConfigFingerprint string
}

type KubernetesUpgradeResult struct {
	Outcome  OperationOutcome
	Nodes    []KubernetesUpgradeNodeResult
	Snapshot *EtcdSnapshotResult
}

// AssessKubernetesUpgrade validates the supported source-to-target path and
// creates a deterministic plan from a complete live inventory.
func AssessKubernetesUpgrade(inventory KubernetesUpgradeInventory, target string) (KubernetesUpgradePlan, error) {
	fail := func(reason string) (KubernetesUpgradePlan, error) {
		return KubernetesUpgradePlan{}, fmt.Errorf("%w: %s", ErrKubernetesUpgradeAssessment, reason)
	}
	if inventory.ClusterUID == "" || inventory.TalosClusterID == "" || inventory.Gateway == "" || inventory.CLI.Version != "v1.14.4" || inventory.CLI.Path == "" || inventory.CLI.SHA256 == "" {
		return fail("cluster identity or supported talosctl identity is incomplete")
	}
	if !inventory.Healthy || len(inventory.Nodes) == 0 {
		return fail("cluster health or node inventory is incomplete")
	}
	to, err := parseStableKubernetesVersion(target)
	if err != nil {
		return fail("target must be a stable Kubernetes version")
	}
	plan := KubernetesUpgradePlan{
		ClusterUID: inventory.ClusterUID, TalosClusterID: inventory.TalosClusterID,
		Gateway: inventory.Gateway, CLI: inventory.CLI, TargetVersion: to.String(),
		ProxyEnabled: inventory.ProxyEnabled, TargetProxyImage: inventory.TargetProxyImage,
		ProxyVersions: append([]KubernetesProxyNodeVersion(nil), inventory.ProxyVersions...),
		Manifests:     append([]string(nil), inventory.Manifests...),
		Nodes:         make([]KubernetesUpgradeNodePlan, 0, len(inventory.Nodes)),
	}
	sort.Slice(plan.ProxyVersions, func(i, j int) bool { return plan.ProxyVersions[i].NodeUID < plan.ProxyVersions[j].NodeUID })
	if !inventory.ProxyConfigKnown {
		return fail("Talos kube-proxy configuration is unknown")
	}
	if inventory.TargetProxyImage == "" {
		return fail("target Talos kube-proxy image is unknown")
	}
	seenUID, seenName := map[string]struct{}{}, map[string]struct{}{}
	for _, node := range inventory.Nodes {
		if node.Name == "" || node.UID == "" || node.TalosID == "" || node.TalosAddress == "" || node.Role == NodeRoleUnknown || node.TalosVersion == "" || node.KubeletVersion == "" {
			return fail("node identity, role, or version is unknown")
		}
		if _, ok := seenUID[node.UID]; ok {
			return fail("duplicate Kubernetes node UID")
		}
		if _, ok := seenName[node.Name]; ok {
			return fail("ambiguous Kubernetes node name")
		}
		seenUID[node.UID], seenName[node.Name] = struct{}{}, struct{}{}
		if !node.Ready {
			return fail("a Kubernetes node is not ready")
		}
		kubelet, parseErr := parseStableKubernetesVersion(node.KubeletVersion)
		if parseErr != nil {
			return fail("a kubelet version is unknown or unstable")
		}
		if node.TargetKubeletImage == "" || node.ConfigFingerprint == "" {
			return fail("a kubelet image or configuration fingerprint is unavailable")
		}
		plan.Nodes = append(plan.Nodes, KubernetesUpgradeNodePlan{
			ClusterUID: inventory.ClusterUID, Name: node.Name, UID: node.UID, TalosID: node.TalosID, TalosAddress: node.TalosAddress,
			Role: node.Role, TalosVersion: node.TalosVersion, SourceVersion: kubelet.String(), TargetVersion: to.String(),
			SourceKubeletImage: node.KubeletImage, TargetKubeletImage: node.TargetKubeletImage,
			ConfigFingerprint: node.ConfigFingerprint, OriginallyCordoned: node.Unschedulable,
			Skip: kubelet.String() == to.String(),
		})
	}
	sort.Slice(plan.Nodes, func(i, j int) bool {
		if plan.Nodes[i].Role != plan.Nodes[j].Role {
			return plan.Nodes[i].Role == NodeRoleControl
		}
		return plan.Nodes[i].Name < plan.Nodes[j].Name
	})
	controlVersions := make([]semver.Version, 0, len(inventory.Nodes)*4)
	for _, component := range inventory.Nodes {
		if component.Role != NodeRoleControl {
			continue
		}
		if !component.ProxyConfigKnown || component.ProxyEnabled != inventory.ProxyEnabled {
			return fail("Talos nodes disagree about whether kube-proxy is enabled")
		}
		if inventory.ProxyEnabled && component.ProxyConfigImage == "" {
			return fail("Talos kube-proxy image configuration is unknown")
		}
		for _, version := range []string{component.APIServerVersion, component.ControllerManagerVersion, component.SchedulerVersion} {
			parsed, err := parseStableKubernetesVersion(version)
			if err != nil {
				return fail("a control-plane component version is unknown")
			}
			controlVersions = append(controlVersions, parsed)
		}
	}
	if inventory.ProxyEnabled {
		if len(inventory.ProxyVersions) == 0 {
			return fail("kube-proxy is enabled but no ready proxy versions were found")
		}
		seenProxyNodes := make(map[string]struct{}, len(inventory.ProxyVersions))
		knownNodes := make(map[string]string, len(inventory.Nodes))
		for _, node := range inventory.Nodes {
			knownNodes[node.UID] = node.Name
		}
		if len(inventory.ProxyVersions) != len(inventory.Nodes) {
			return fail("kube-proxy is enabled but proxy inventory does not cover every node")
		}
		for _, proxy := range inventory.ProxyVersions {
			if proxy.NodeUID == "" || proxy.Name == "" {
				return fail("a kube-proxy node identity is unknown")
			}
			if knownNodes[proxy.NodeUID] != proxy.Name {
				return fail("a kube-proxy pod is attached to an unknown node")
			}
			if _, ok := seenProxyNodes[proxy.NodeUID]; ok {
				return fail("multiple kube-proxy pods are attached to one node")
			}
			seenProxyNodes[proxy.NodeUID] = struct{}{}
			parsed, err := parseStableKubernetesVersion(proxy.Version)
			if err != nil {
				return fail("a kube-proxy version is unknown")
			}
			controlVersions = append(controlVersions, parsed)
		}
	} else if len(inventory.ProxyVersions) != 0 {
		return fail("kube-proxy pods exist while Talos proxy configuration is disabled")
	}
	if len(controlVersions) == 0 {
		return fail("control-plane version inventory is empty")
	}
	min := controlVersions[0]
	for _, version := range controlVersions[1:] {
		if version.LT(min) {
			min = version
		}
	}
	plan.SourceVersion = min.String()
	from, _ := parseStableKubernetesVersion(plan.SourceVersion)
	if to.Major != from.Major || to.Minor < from.Minor || to.Minor > from.Minor+1 || (to.Minor == from.Minor && to.Patch < from.Patch) {
		return fail("only patch upgrades or one forward minor step are supported")
	}
	for _, version := range controlVersions {
		if version.String() != plan.SourceVersion && version.String() != to.String() {
			return fail("control-plane components contain unrelated version drift")
		}
		if version.Minor > to.Minor || to.Minor-version.Minor > 1 {
			return fail("control-plane component version skew is unsupported")
		}
	}
	plan.SkipControlPlane = controlPlaneAtTarget(inventory, to.String()) && inventory.ManifestsConverged
	// A fresh session must reconcile managed manifests through Talos again when
	// it has no evidence of the earlier CLI stage, even if components are at target.
	kubeletSource := plan.SourceVersion
	if from.String() == to.String() {
		var ok bool
		kubeletSource, ok = coherentKubeletSource(plan.Nodes, to.String())
		if !ok {
			return fail("kubelet versions contain unrelated source or target drift")
		}
	}
	for _, node := range plan.Nodes {
		version, _ := parseStableKubernetesVersion(node.SourceVersion)
		if version.GT(to) {
			return fail("a kubelet version is newer than the target")
		}
		if version.String() != kubeletSource && version.String() != to.String() {
			return fail("kubelet versions contain unrelated source or target drift")
		}
		if version.Minor > to.Minor || to.Minor-version.Minor > 3 {
			return fail("a kubelet is more than three minor versions behind the target")
		}
	}
	return plan, nil
}

func coherentKubeletSource(nodes []KubernetesUpgradeNodePlan, target string) (string, bool) {
	source := target
	for _, node := range nodes {
		version, err := parseStableKubernetesVersion(node.SourceVersion)
		if err != nil {
			return "", false
		}
		if version.String() == target {
			continue
		}
		if source != target && source != version.String() {
			return "", false
		}
		source = version.String()
	}
	return source, true
}

func parseStableKubernetesVersion(value string) (semver.Version, error) {
	version, err := semver.ParseTolerant(value)
	if err != nil || len(version.Pre) != 0 || len(version.Build) != 0 || version.Major != 1 {
		return semver.Version{}, errors.New("unsupported Kubernetes version")
	}
	return version, nil
}

func controlPlaneAtTarget(inventory KubernetesUpgradeInventory, target string) bool {
	for _, node := range inventory.Nodes {
		if node.Role == NodeRoleControl && (normalizeKubernetesVersion(node.APIServerVersion) != target || normalizeKubernetesVersion(node.ControllerManagerVersion) != target || normalizeKubernetesVersion(node.SchedulerVersion) != target || node.ProxyConfigImage != inventory.TargetProxyImage) {
			return false
		}
	}
	if inventory.ProxyEnabled {
		if !inventory.ProxyRolloutComplete {
			return false
		}
		if len(inventory.ProxyVersions) == 0 {
			return false
		}
		if len(inventory.ProxyVersions) != len(inventory.Nodes) {
			return false
		}
		for _, proxy := range inventory.ProxyVersions {
			if normalizeKubernetesVersion(proxy.Version) != target {
				return false
			}
		}
	} else if len(inventory.ProxyVersions) != 0 {
		return false
	}
	return true
}

func normalizeKubernetesVersion(value string) string {
	version, err := parseStableKubernetesVersion(value)
	if err != nil {
		return ""
	}
	return version.String()
}
