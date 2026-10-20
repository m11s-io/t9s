package kubernetes

import (
	"context"
	"errors"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

var errKubernetesNodeChanged = errors.New("Kubernetes node identity changed")

type UpgradeAccess interface {
	ClusterUID(ctx context.Context) (string, error)
	SchedulingState(ctx context.Context, name, uid string) (bool, error)
	WaitKubelet(ctx context.Context, name, uid, version string, timeout time.Duration) error
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

func (a *upgradeAccess) SchedulingState(ctx context.Context, name, uid string) (bool, error) {
	node, err := a.getNode(ctx, name, uid)
	if err != nil {
		return false, err
	}
	return node.Spec.Unschedulable, nil
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
