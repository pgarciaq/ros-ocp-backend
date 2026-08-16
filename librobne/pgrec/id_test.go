package pgrec

import "testing"

func TestNativeContainerIDStable(t *testing.T) {
	t.Parallel()
	a := NativeContainerID("11111111-1111-1111-1111-111111111111", "ns", "wl", "deployment", "main")
	b := NativeContainerID("11111111-1111-1111-1111-111111111111", "ns", "wl", "deployment", "main")
	if a != b {
		t.Fatalf("NativeContainerID not deterministic: %s vs %s", a, b)
	}
	if a == NativeContainerID("11111111-1111-1111-1111-111111111111", "ns", "wl", "deployment", "other") {
		t.Fatal("NativeContainerID collided across container names")
	}
}

func TestNativeEntityIDsStable(t *testing.T) {
	t.Parallel()
	cluster := "11111111-1111-1111-1111-111111111111"
	if NativeNamespaceID(cluster, "ns") != NativeNamespaceID(cluster, "ns") {
		t.Fatal("NativeNamespaceID not deterministic")
	}
	if NativeNamespaceID(cluster, "ns") == NativeNamespaceID(cluster, "other") {
		t.Fatal("NativeNamespaceID collided")
	}
	if NativeNodeID(cluster, "n1") == NativeNodeID(cluster, "n2") {
		t.Fatal("NativeNodeID collided")
	}
	if NativePvcID(cluster, "ns", "pvc-a") == NativePvcID(cluster, "ns", "pvc-b") {
		t.Fatal("NativePvcID collided")
	}
	if NativeQuotaID(cluster, "ns", "q1") == NativeQuotaID(cluster, "ns", "q2") {
		t.Fatal("NativeQuotaID collided")
	}
	if NativeClusterQuotaID(cluster, "cq1") == NativeClusterQuotaID(cluster, "cq2") {
		t.Fatal("NativeClusterQuotaID collided")
	}
	if NativeVMID(cluster, "ns", "vm-a") == NativeVMID(cluster, "ns", "vm-b") {
		t.Fatal("NativeVMID collided")
	}
}

func TestSourceID(t *testing.T) {
	t.Parallel()
	if SourceID != "robne" {
		t.Fatalf("SourceID = %q, want robne", SourceID)
	}
}
