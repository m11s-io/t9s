package kubernetes

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

const nodeRoleLabelPrefix = "node-role.kubernetes.io/"

type nodeReader struct {
	clientset kubernetes.Interface
}

func newNodeReader(clientset kubernetes.Interface) ports.KubernetesNodeReader {
	return &nodeReader{clientset: clientset}
}

func (r *nodeReader) List(ctx context.Context) (map[string]domain.KubernetesNodeSnapshot, error) {
	list, err := r.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list kubernetes nodes: %w", err)
	}

	result := make(map[string]domain.KubernetesNodeSnapshot, len(list.Items))
	for _, node := range list.Items {
		snapshot := convertKubernetesNode(node)
		result[node.Name] = snapshot
		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeInternalIP {
				result[address.Address] = snapshot
			}
		}
	}

	return result, nil
}

func convertKubernetesNode(node corev1.Node) domain.KubernetesNodeSnapshot {
	var roles []string
	for label := range node.Labels {
		if role, ok := strings.CutPrefix(label, nodeRoleLabelPrefix); ok {
			roles = append(roles, role)
		}
	}

	conditions := make([]domain.KubernetesCondition, len(node.Status.Conditions))
	for index, condition := range node.Status.Conditions {
		conditions[index] = domain.KubernetesCondition{
			Type:   string(condition.Type),
			Status: string(condition.Status),
			Reason: condition.Reason,
		}
	}

	return domain.KubernetesNodeSnapshot{
		Roles:          roles,
		KubeletVersion: node.Status.NodeInfo.KubeletVersion,
		Conditions:     conditions,
	}
}
