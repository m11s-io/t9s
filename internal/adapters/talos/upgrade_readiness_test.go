package talos

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestUpgradeReadinessRetriesAPIDowntime(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	reads := 0
	client.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		reads++
		if reads == 1 {
			return true, nil, apierrors.NewServiceUnavailable("API restarting")
		}
		return true, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cp-1"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}, NodeInfo: corev1.NodeSystemInfo{BootID: "new"}}}, nil
	})
	m := machineryUpgradeMaintenance{clientset: client, nodeName: "cp-1", preUpgradeBootID: "old"}
	if err := m.WaitKubernetesReady(t.Context(), 7*time.Second); err != nil {
		t.Fatalf("temporary API downtime aborted recovery: %v", err)
	}
	if reads < 2 {
		t.Fatalf("reads=%d", reads)
	}
}
func TestUpgradeReadinessAPIDowntimeIsBounded(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	unavailable := apierrors.NewServiceUnavailable("API restarting")
	client.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, unavailable })
	m := machineryUpgradeMaintenance{clientset: client, nodeName: "cp-1"}
	err := m.WaitKubernetesReady(t.Context(), 10*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, unavailable) {
		t.Fatalf("expected bounded wait and API error, got %v", err)
	}
}
