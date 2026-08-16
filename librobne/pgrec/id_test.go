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

func TestSourceID(t *testing.T) {
	t.Parallel()
	if SourceID != "robne" {
		t.Fatalf("SourceID = %q, want robne", SourceID)
	}
}
