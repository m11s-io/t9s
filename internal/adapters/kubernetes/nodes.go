package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

const (
	nodeRoleLabelPrefix = "node-role.kubernetes.io/"
	nodeRoleLabelSuffix = "kubernetes.io/role"
)

type nodeLister interface {
	List(context.Context, metav1.ListOptions) (*corev1.NodeList, error)
}

type nodeReader struct {
	nodes nodeLister
}

func newNodeReader(nodes nodeLister) ports.KubernetesNodeReader {
	return &nodeReader{nodes: nodes}
}

func (r *nodeReader) List(ctx context.Context) (map[string]domain.KubernetesNodeSnapshot, error) {
	list, err := r.nodes.List(ctx, metav1.ListOptions{})
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
	for label, value := range node.Labels {
		switch {
		case strings.HasPrefix(label, nodeRoleLabelPrefix):
			if role := strings.TrimPrefix(label, nodeRoleLabelPrefix); role != "" {
				roles = append(roles, role)
			}
		case strings.HasSuffix(label, nodeRoleLabelSuffix) && value != "":
			roles = append(roles, value)
		}
	}

	sort.Strings(roles)

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
