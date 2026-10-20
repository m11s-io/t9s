package domain

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/blang/semver/v4"
)

var ErrKubernetesUpgradeAssessment = errors.New("Kubernetes upgrade assessment failed")

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
	APIServerVersion         string
	ControllerManagerVersion string
	SchedulerVersion         string
	ProxyVersion             string
	KubeletVersion           string
	KubeletImage             string
	TargetKubeletImage       string
	ConfigFingerprint        string
	Ready                    bool
	Unschedulable            bool
}

type KubernetesUpgradeInventory struct {
	ClusterUID         string
	TalosClusterID     string
	Gateway            string
	CLI                TalosCLIIdentity
	Nodes              []KubernetesUpgradeNode
	Manifests          []string
	ManifestsConverged bool
	Permissions        map[string]bool
	Healthy            bool
	ObservedAt         time.Time
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
	KubernetesUpgradeDrain        KubernetesUpgradeStage = "drain"
	KubernetesUpgradeKubelet      KubernetesUpgradeStage = "kubelet"
	KubernetesUpgradeReadiness    KubernetesUpgradeStage = "readiness"
	KubernetesUpgradeUncordon     KubernetesUpgradeStage = "uncordon"
	KubernetesUpgradeVerify       KubernetesUpgradeStage = "verify"
)

type KubernetesUpgradeEvent struct {
	Stage   KubernetesUpgradeStage
	NodeUID string
	Status  string
}

type KubernetesUpgradeNodeResult struct {
	NodeUID      string
	Outcome      OperationOutcome
	LeftCordoned bool
	Message      string
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
		Manifests: append([]string(nil), inventory.Manifests...),
		Nodes:     make([]KubernetesUpgradeNodePlan, 0, len(inventory.Nodes)),
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
		for _, version := range []string{component.APIServerVersion, component.ControllerManagerVersion, component.SchedulerVersion, component.ProxyVersion} {
			parsed, err := parseStableKubernetesVersion(version)
			if err != nil {
				return fail("a control-plane or proxy version is unknown")
			}
			controlVersions = append(controlVersions, parsed)
		}
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
	for _, node := range plan.Nodes {
		version, _ := parseStableKubernetesVersion(node.SourceVersion)
		if version.String() != plan.SourceVersion && version.String() != to.String() {
			return fail("kubelet versions contain unrelated source or target drift")
		}
		if version.Minor > to.Minor || to.Minor-version.Minor > 3 {
			return fail("a kubelet is more than three minor versions behind the target")
		}
	}
	plan.SkipControlPlane = controlPlaneAtTarget(inventory.Nodes, to.String()) && inventory.ManifestsConverged
	if from.String() == to.String() && !plan.SkipControlPlane {
		return fail("the control plane is already at target but manifest convergence is unverified")
	}
	return plan, nil
}

func parseStableKubernetesVersion(value string) (semver.Version, error) {
	version, err := semver.ParseTolerant(value)
	if err != nil || len(version.Pre) != 0 || len(version.Build) != 0 || version.Major != 1 {
		return semver.Version{}, errors.New("unsupported Kubernetes version")
	}
	return version, nil
}

func controlPlaneAtTarget(nodes []KubernetesUpgradeNode, target string) bool {
	for _, node := range nodes {
		if node.Role == NodeRoleControl && (normalizeKubernetesVersion(node.APIServerVersion) != target || normalizeKubernetesVersion(node.ControllerManagerVersion) != target || normalizeKubernetesVersion(node.SchedulerVersion) != target || normalizeKubernetesVersion(node.ProxyVersion) != target) {
			return false
		}
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
