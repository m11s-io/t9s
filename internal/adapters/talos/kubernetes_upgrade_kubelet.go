package talos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/m11s-io/t9s/internal/adapters/kubernetes"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/stdpatches"
	configresource "github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/v1alpha1"
	"k8s.io/client-go/tools/clientcmd"
)

func (b *kubernetesUpgradeBackend) UpgradeKubelet(ctx context.Context, plan domain.KubernetesUpgradeNodePlan, progress func(domain.KubernetesUpgradeEvent)) (domain.KubernetesUpgradeNodeResult, error) {
	result := domain.KubernetesUpgradeNodeResult{NodeUID: plan.UID}
	if plan.Skip {
		result.Outcome = domain.OperationCompleted
		result.ConfigFingerprint = plan.ConfigFingerprint
		return result, nil
	}
	emit := func(stage domain.KubernetesUpgradeStage) {
		if progress != nil {
			progress(domain.KubernetesUpgradeEvent{Stage: stage, NodeUID: plan.UID, Status: "running"})
		}
	}
	if b == nil || b.talos == nil || plan.TargetKubeletImage == "" || plan.UID == "" {
		return result, errors.New("kubelet upgrade plan is incomplete")
	}
	result.LeftCordoned = plan.OriginallyCordoned
	nodeCtx := talosclient.WithNode(ctx, plan.TalosAddress)
	_, initialFingerprint, err := b.readActiveConfig(ctx, plan.TalosAddress)
	if err != nil || initialFingerprint != plan.ConfigFingerprint {
		return result, errors.New("node configuration changed after upgrade planning")
	}
	emit(domain.KubernetesUpgradePrePull)
	if err := b.talos.ImagePull(nodeCtx, common.ContainerdNamespace_NS_SYSTEM, plan.TargetKubeletImage); err != nil {
		return result, errors.New("unable to pre-pull target kubelet image")
	}
	kubeBytes, err := b.talos.Kubeconfig(ctx)
	if err != nil {
		return result, errors.New("unable to obtain Kubernetes credentials from Talos")
	}
	if err := validateTalosKubeconfig(kubeBytes); err != nil {
		clear(kubeBytes)
		return result, errors.New("Talos returned unsupported Kubernetes credentials")
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeBytes)
	clear(kubeBytes)
	if err != nil {
		return result, errors.New("unable to build Kubernetes connection")
	}
	access, err := kubernetes.NewUpgradeAccess(restConfig)
	if err != nil {
		return result, err
	}
	clusterUID, err := access.ClusterUID(ctx)
	if err != nil || clusterUID != plan.ClusterUID {
		return result, errors.New("Kubernetes credentials identify a different cluster")
	}
	wasUnschedulable, err := access.SchedulingState(ctx, plan.Name, plan.UID)
	if err != nil {
		return result, err
	}
	if wasUnschedulable != plan.OriginallyCordoned {
		return result, errors.New("node scheduling state changed after preview")
	}
	current, fingerprint, err := b.readActiveConfig(ctx, plan.TalosAddress)
	if err != nil || fingerprint != plan.ConfigFingerprint {
		return result, errors.New("node configuration changed before kubelet patch")
	}
	provider := current.Provider()
	patch, err := stdpatches.PreparePatch(stdpatches.WithKubeletImage(stdpatches.GuessVersionContractKubelet(provider), plan.TargetKubeletImage))
	if err != nil {
		return result, errors.New("unable to prepare kubelet image update")
	}
	patched, err := configpatcher.Apply(configpatcher.WithConfig(provider), []configpatcher.Patch{patch})
	if err != nil {
		return result, errors.New("unable to patch kubelet image in node configuration")
	}
	newConfig, err := patched.Config()
	if err != nil {
		return result, errors.New("unable to encode kubelet configuration")
	}
	if newConfig.K8sKubeletConfig() == nil || newConfig.K8sKubeletConfig().Image() != plan.TargetKubeletImage {
		return result, errors.New("kubelet configuration patch did not produce the target image")
	}
	configBytes, err := newConfig.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return result, errors.New("unable to serialize kubelet configuration")
	}
	defer clear(configBytes)
	_, latestFingerprint, err := b.readActiveConfig(ctx, plan.TalosAddress)
	if err != nil || latestFingerprint != fingerprint {
		return result, errors.New("node configuration changed while preparing the kubelet update")
	}
	emit(domain.KubernetesUpgradeKubelet)
	response, err := b.talos.ApplyConfiguration(nodeCtx, &machineapi.ApplyConfigurationRequest{Data: configBytes, Mode: machineapi.ApplyConfigurationRequest_NO_REBOOT})
	if err != nil || response == nil {
		result.Outcome = domain.OperationUnconfirmed
		return result, errors.New("kubelet configuration apply is unconfirmed")
	}
	emit(domain.KubernetesUpgradeReadiness)
	if err := b.waitKubeletService(ctx, plan.TalosAddress); err != nil {
		result.Outcome = domain.OperationUnconfirmed
		return result, errors.New("Talos kubelet service did not become healthy")
	}
	if err := access.WaitKubelet(ctx, plan.Name, plan.UID, plan.TargetVersion, 5*time.Minute); err != nil {
		result.Outcome = domain.OperationUnconfirmed
		return result, errors.New("Kubernetes kubelet did not converge")
	}
	expectedConfig := sha256.Sum256(configBytes)
	_, appliedFingerprint, err := b.readActiveConfig(ctx, plan.TalosAddress)
	if err != nil || appliedFingerprint != hex.EncodeToString(expectedConfig[:]) {
		result.Outcome = domain.OperationUnconfirmed
		return result, errors.New("active node configuration does not match the kubelet update")
	}
	result.ConfigFingerprint = appliedFingerprint
	result.Outcome = domain.OperationCompleted
	return result, nil
}

func (b *kubernetesUpgradeBackend) readActiveConfig(ctx context.Context, node string) (*configresource.MachineConfig, string, error) {
	nodeCtx := talosclient.WithNode(ctx, node)
	active, err := safe.StateGetByID[*configresource.MachineConfig](nodeCtx, b.talos.COSI, configresource.ActiveID)
	if err != nil {
		return nil, "", err
	}
	data, err := active.Provider().EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(data)
	clear(data)
	return active, hex.EncodeToString(digest[:]), nil
}

func (b *kubernetesUpgradeBackend) waitKubeletService(ctx context.Context, node string) error {
	deadline, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		service, err := safe.StateGet[*v1alpha1.Service](talosclient.WithNode(deadline, node), b.talos.COSI, resource.NewMetadata(v1alpha1.NamespaceName, v1alpha1.ServiceType, "kubelet", resource.VersionUndefined))
		if err == nil && service.TypedSpec().Running && service.TypedSpec().Healthy {
			return nil
		}
		select {
		case <-deadline.Done():
			return errors.New("kubelet health timeout")
		case <-ticker.C:
		}
	}
}
