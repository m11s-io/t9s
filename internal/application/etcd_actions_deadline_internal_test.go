package application

import (
	"testing"
	"time"
)

func TestEtcdActionBudgetLeavesTimeForFreshReads(t *testing.T) {
	tests := []struct {
		name string
		kind EtcdActionKind
		want time.Duration
	}{
		{name: "snapshot", kind: EtcdActionSnapshot, want: snapshotTimeout + unaryReadTimeout},
		{name: "defragment", kind: EtcdActionDefragment, want: defragmentTimeout + unaryReadTimeout},
		{name: "control", kind: EtcdActionForfeitLeadership, want: controlOperationTimeout + unaryReadTimeout},
		{name: "membership", kind: EtcdActionRemoveMember, want: snapshotTimeout + controlOperationTimeout + 2*unaryReadTimeout},
		{name: "leave", kind: EtcdActionLeaveCluster, want: snapshotTimeout + controlOperationTimeout + 2*unaryReadTimeout},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := etcdActionBudget(test.kind); got != test.want {
				t.Fatalf("etcdActionBudget(%q) = %s, want %s", test.kind, got, test.want)
			}
		})
	}
}
