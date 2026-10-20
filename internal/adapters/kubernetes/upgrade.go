package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

var errKubernetesNodeChanged = errors.New("Kubernetes node identity changed")

type UpgradeAccess interface {
	ClusterUID(ctx context.Context) (string, error)
	SchedulingState(ctx context.Context, name, uid string) (bool, error)
	Cordon(ctx context.Context, name, uid string) (bool, error)
	Drain(ctx context.Context, name, uid string, timeout time.Duration) error
	WaitKubelet(ctx context.Context, name, uid, version string, timeout time.Duration) error
	Uncordon(ctx context.Context, name, uid string) error
}

type upgradeAccess struct{ client coreclient.CoreV1Interface }

func (a *upgradeAccess) ClusterUID(ctx context.Context) (string, error) {
	namespace, err := a.client.Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil || namespace.UID == "" {
		return "", errors.New("unable to verify Kubernetes cluster identity")
	}
	return string(namespace.UID), nil
}

func NewUpgradeAccess(config *rest.Config) (UpgradeAccess, error) {
	client, err := coreclient.NewForConfig(config)
	if err != nil {
		return nil, errors.New("unable to build Kubernetes upgrade access")
	}
	return &upgradeAccess{client: client}, nil
}

func (a *upgradeAccess) getNode(ctx context.Context, name, uid string) (*corev1.Node, error) {
	node, err := a.client.Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.New("unable to read Kubernetes node")
	}
	if string(node.UID) != uid {
		return nil, errKubernetesNodeChanged
	}
	return node, nil
}

func (a *upgradeAccess) Cordon(ctx context.Context, name, uid string) (bool, error) {
	node, err := a.getNode(ctx, name, uid)
	if err != nil {
		return false, err
	}
	if node.Spec.Unschedulable {
		return false, nil
	}
	node.Spec.Unschedulable = true
	updated, err := a.client.Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return false, errors.New("unable to cordon Kubernetes node")
	}
	if string(updated.UID) != uid || !updated.Spec.Unschedulable {
		return false, errKubernetesNodeChanged
	}
	return true, nil
}

func (a *upgradeAccess) SchedulingState(ctx context.Context, name, uid string) (bool, error) {
	node, err := a.getNode(ctx, name, uid)
	if err != nil {
		return false, err
	}
	return node.Spec.Unschedulable, nil
}

func (a *upgradeAccess) Drain(ctx context.Context, name, uid string, timeout time.Duration) error {
	drainCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if _, err := a.getNode(drainCtx, name, uid); err != nil {
			return err
		}
		pods, err := a.client.Pods("").List(drainCtx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + name})
		if err != nil {
			return errors.New("unable to inspect pods for node drain")
		}
		pending := false
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed || isMirrorPod(pod) || isDaemonSetPod(pod) {
				continue
			}
			if len(pod.OwnerReferences) == 0 || hasEmptyDir(pod) {
				return fmt.Errorf("drain is blocked by pod %s/%s that requires operator action", pod.Namespace, pod.Name)
			}
			pending = true
			eviction := &policyv1.Eviction{ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: pod.Namespace}, DeleteOptions: &metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: ptrUID(pod.UID)}}}
			err := a.client.Pods(pod.Namespace).EvictV1(drainCtx, eviction)
			if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
				if drainCtx.Err() != nil {
					return errors.New("node drain timed out")
				}
				if strings.Contains(strings.ToLower(err.Error()), "too many requests") || strings.Contains(strings.ToLower(err.Error()), "disruption budget") {
					continue
				}
				return errors.New("pod eviction failed; drain stopped")
			}
		}
		if !pending {
			return nil
		}
		select {
		case <-drainCtx.Done():
			return errors.New("node drain timed out")
		case <-time.After(2 * time.Second):
		}
	}
}

func (a *upgradeAccess) WaitKubelet(ctx context.Context, name, uid, version string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		node, err := a.getNode(waitCtx, name, uid)
		if err != nil {
			return err
		}
		if upgradeNodeReady(*node) && strings.TrimPrefix(node.Status.NodeInfo.KubeletVersion, "v") == strings.TrimPrefix(version, "v") {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return errors.New("Kubernetes kubelet did not become ready at the target version")
		case <-ticker.C:
		}
	}
}

func upgradeNodeReady(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func (a *upgradeAccess) Uncordon(ctx context.Context, name, uid string) error {
	node, err := a.getNode(ctx, name, uid)
	if err != nil {
		return err
	}
	if !node.Spec.Unschedulable {
		return nil
	}
	node.Spec.Unschedulable = false
	updated, err := a.client.Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return errors.New("unable to uncordon Kubernetes node")
	}
	if string(updated.UID) != uid || updated.Spec.Unschedulable {
		return errKubernetesNodeChanged
	}
	return nil
}

func isMirrorPod(pod corev1.Pod) bool { return pod.Annotations[corev1.MirrorPodAnnotationKey] != "" }

func isDaemonSetPod(pod corev1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

func hasEmptyDir(pod corev1.Pod) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.EmptyDir != nil {
			return true
		}
	}
	return false
}

func ptrUID(uid types.UID) *types.UID { return &uid }
