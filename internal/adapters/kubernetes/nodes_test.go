package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeReaderListIndexesByNameAndInternalIP(t *testing.T) {
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "worker-1",
			Labels: map[string]string{"node-role.kubernetes.io/worker": ""},
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.5"},
				{Type: corev1.NodeHostName, Address: "worker-1.local"},
			},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue, Reason: "KubeletReady"},
			},
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.31.0"},
		},
	}
	clientset := fake.NewSimpleClientset(&node)
	reader := newNodeReader(clientset)

	result, err := reader.List(t.Context())

	require.NoError(t, err)
	byName, ok := result["worker-1"]
	require.True(t, ok, "must be indexed by Node.Name")
	assert.Equal(t, []string{"worker"}, byName.Roles)
	assert.Equal(t, "v1.31.0", byName.KubeletVersion)
	require.Len(t, byName.Conditions, 1)
	assert.Equal(t, "Ready", byName.Conditions[0].Type)
	assert.Equal(t, "True", byName.Conditions[0].Status)

	byAddress, ok := result["10.0.0.5"]
	require.True(t, ok, "must also be indexed by every InternalIP address")
	assert.Equal(t, byName, byAddress)

	_, ok = result["worker-1.local"]
	assert.False(t, ok, "only InternalIP addresses are indexed, not hostnames")
}
