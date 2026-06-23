package model

import (
	"testing"

	"github.com/google/uuid"
)

func TestNativeNodeID_Deterministic(t *testing.T) {
	id1 := NativeNodeID("cluster-1", "worker-0")
	id2 := NativeNodeID("cluster-1", "worker-0")
	if id1 != id2 {
		t.Errorf("NativeNodeID should be deterministic: %s != %s", id1, id2)
	}
}

func TestNativeNodeID_DiffersFromNamespaceID(t *testing.T) {
	nodeID := NativeNodeID("cluster-1", "kube-system")
	nsID := NativeNamespaceID("cluster-1", "kube-system")
	if nodeID == nsID {
		t.Error("node ID must not collide with namespace ID for same cluster/name")
	}
}

func TestNativePvcID_Deterministic(t *testing.T) {
	id1 := NativePvcID("c1", "apps", "data")
	id2 := NativePvcID("c1", "apps", "data")
	if id1 != id2 {
		t.Errorf("NativePvcID should be deterministic: %s != %s", id1, id2)
	}
}

func TestNativeQuotaID_Deterministic(t *testing.T) {
	id1 := NativeQuotaID("c1", "apps", "compute-quota")
	id2 := NativeQuotaID("c1", "apps", "compute-quota")
	if id1 != id2 {
		t.Errorf("NativeQuotaID should be deterministic: %s != %s", id1, id2)
	}
}

func TestNativeClusterQuotaID_Deterministic(t *testing.T) {
	id1 := NativeClusterQuotaID("c1", "tenant-quota")
	id2 := NativeClusterQuotaID("c1", "tenant-quota")
	if id1 != id2 {
		t.Errorf("NativeClusterQuotaID should be deterministic: %s != %s", id1, id2)
	}
}

func TestNativeSnapshotID_Deterministic(t *testing.T) {
	id1 := NativeSnapshotID("c1", "apps", "snap-1")
	id2 := NativeSnapshotID("c1", "apps", "snap-1")
	if id1 != id2 {
		t.Errorf("NativeSnapshotID should be deterministic: %s != %s", id1, id2)
	}
}

func TestNativeVMID_Deterministic(t *testing.T) {
	id1 := NativeVMID("c1", "vms", "web-vm")
	id2 := NativeVMID("c1", "vms", "web-vm")
	if id1 != id2 {
		t.Errorf("NativeVMID should be deterministic: %s != %s", id1, id2)
	}
}

func TestNativeRecommendationIDs_AreValidUUIDs(t *testing.T) {
	ids := []string{
		NativeNodeID("c", "n"),
		NativePvcID("c", "ns", "pvc"),
		NativeQuotaID("c", "ns", "q"),
		NativeClusterQuotaID("c", "cq"),
		NativeSnapshotID("c", "ns", "snap"),
		NativeVMID("c", "ns", "vm"),
	}
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			t.Errorf("expected valid UUID, got %q: %v", id, err)
		}
	}
}
