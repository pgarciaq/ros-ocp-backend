package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectSharedPVCs_ByName_SharedPVC(t *testing.T) {
	d1 := vmDigestForPlacement("db-primary", "data", "node-1", 8000, 16<<20, 200<<30)
	d1.PVCs = []PVCDigest{
		{PVCName: "shared-data", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
		{PVCName: "logs-primary", DiskCapacityBytes: 50 << 30, VolumeMode: "Filesystem"},
	}
	d2 := vmDigestForPlacement("db-standby", "data", "node-2", 8000, 16<<20, 200<<30)
	d2.PVCs = []PVCDigest{
		{PVCName: "shared-data", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
		{PVCName: "logs-standby", DiskCapacityBytes: 50 << 30, VolumeMode: "Filesystem"},
	}

	cluster := []Digest{d1, d2}
	cfg := DefaultVMRecConfig()
	notifs, shared := DetectSharedPVCs(NewClusterContext(cluster), d1, cfg)
	require.True(t, shared)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMSharedStorage, notifs[0].Code)
	assert.Contains(t, notifs[0].Message, "shared-data")
	assert.Contains(t, notifs[0].Message, "db-standby")
}

func TestDetectSharedPVCs_ByName_NoOverlap(t *testing.T) {
	d1 := vmDigestForPlacement("web-1", "apps", "node-1", 4000, 8<<20, 100<<30)
	d1.PVCs = []PVCDigest{
		{PVCName: "web-1-data", DiskCapacityBytes: 50 << 30, VolumeMode: "Filesystem"},
	}
	d2 := vmDigestForPlacement("web-2", "apps", "node-1", 4000, 8<<20, 100<<30)
	d2.PVCs = []PVCDigest{
		{PVCName: "web-2-data", DiskCapacityBytes: 50 << 30, VolumeMode: "Filesystem"},
	}

	cluster := []Digest{d1, d2}
	cfg := DefaultVMRecConfig()
	notifs, shared := DetectSharedPVCs(NewClusterContext(cluster), d1, cfg)
	assert.Nil(t, notifs)
	assert.False(t, shared)
}

func TestDetectSharedPVCs_ByName_MultipleSharedPVCs(t *testing.T) {
	d1 := vmDigestForPlacement("vm-a", "ns", "n1", 4000, 8<<20, 100<<30)
	d1.PVCs = []PVCDigest{
		{PVCName: "pvc-shared-1", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
		{PVCName: "pvc-shared-2", DiskCapacityBytes: 50 << 30, VolumeMode: "Block"},
	}
	d2 := vmDigestForPlacement("vm-b", "ns", "n2", 4000, 8<<20, 100<<30)
	d2.PVCs = []PVCDigest{
		{PVCName: "pvc-shared-1", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
		{PVCName: "pvc-shared-2", DiskCapacityBytes: 50 << 30, VolumeMode: "Block"},
	}

	cluster := []Digest{d1, d2}
	cfg := DefaultVMRecConfig()
	notifs, shared := DetectSharedPVCs(NewClusterContext(cluster), d1, cfg)
	require.True(t, shared)
	require.Len(t, notifs, 2)
	for _, n := range notifs {
		assert.Equal(t, NotifVMSharedStorage, n.Code)
		assert.Contains(t, n.Message, "vm-b")
	}
}

func TestDetectSharedPVCs_ByName_DifferentNamespace(t *testing.T) {
	d1 := vmDigestForPlacement("vm-a", "ns-1", "n1", 4000, 8<<20, 100<<30)
	d1.PVCs = []PVCDigest{
		{PVCName: "shared-pvc", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
	}
	d2 := vmDigestForPlacement("vm-b", "ns-2", "n2", 4000, 8<<20, 100<<30)
	d2.PVCs = []PVCDigest{
		{PVCName: "shared-pvc", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
	}

	cluster := []Digest{d1, d2}
	cfg := DefaultVMRecConfig()
	notifs, shared := DetectSharedPVCs(NewClusterContext(cluster), d1, cfg)
	assert.Nil(t, notifs)
	assert.False(t, shared)
}

func TestDetectSharedPVCs_ProxyFallback_NoPVCData(t *testing.T) {
	d1 := vmDigestForPlacement("db-primary", "data", "node-1", 8000, 16<<20, 200<<30)
	d2 := vmDigestForPlacement("db-standby", "data", "node-2", 8000, 16<<20, 200<<30)

	cluster := []Digest{d1, d2}
	cfg := DefaultVMRecConfig()
	notifs, shared := DetectSharedPVCs(NewClusterContext(cluster), d1, cfg)
	require.True(t, shared)
	require.Len(t, notifs, 1)
	assert.Contains(t, notifs[0].Message, "Correlated workload group")
	assert.Contains(t, notifs[0].Message, "db-standby")
}

func TestDetectSharedPVCs_Disabled(t *testing.T) {
	d1 := vmDigestForPlacement("vm-a", "ns", "n1", 4000, 8<<20, 100<<30)
	d1.PVCs = []PVCDigest{
		{PVCName: "shared-pvc", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
	}
	d2 := vmDigestForPlacement("vm-b", "ns", "n2", 4000, 8<<20, 100<<30)
	d2.PVCs = []PVCDigest{
		{PVCName: "shared-pvc", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
	}

	cluster := []Digest{d1, d2}
	cfg := DefaultVMRecConfig()
	cfg.EnableSharedPVCCorrelation = false
	notifs, shared := DetectSharedPVCs(NewClusterContext(cluster), d1, cfg)
	assert.Nil(t, notifs)
	assert.False(t, shared)
}

func TestNewClusterContext_BuildsReverseIndex(t *testing.T) {
	d1 := vmDigestForPlacement("vm-a", "ns", "n1", 4000, 8<<20, 100<<30)
	d1.PVCs = []PVCDigest{
		{PVCName: "pvc-1", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
		{PVCName: "pvc-2", DiskCapacityBytes: 50 << 30, VolumeMode: "Block"},
	}
	d2 := vmDigestForPlacement("vm-b", "ns", "n2", 4000, 8<<20, 100<<30)
	d2.PVCs = []PVCDigest{
		{PVCName: "pvc-1", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
	}
	d3 := vmDigestForPlacement("vm-c", "other-ns", "n3", 4000, 8<<20, 100<<30)
	d3.PVCs = []PVCDigest{
		{PVCName: "pvc-1", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
	}

	ctx := NewClusterContext([]Digest{d1, d2, d3})
	require.NotNil(t, ctx)
	assert.Len(t, ctx.Latest, 3)

	assert.ElementsMatch(t, []string{"vm-a", "vm-b"}, ctx.PVCToVMs[pvcNSKey{Namespace: "ns", PVCName: "pvc-1"}])
	assert.ElementsMatch(t, []string{"vm-a"}, ctx.PVCToVMs[pvcNSKey{Namespace: "ns", PVCName: "pvc-2"}])
	assert.ElementsMatch(t, []string{"vm-c"}, ctx.PVCToVMs[pvcNSKey{Namespace: "other-ns", PVCName: "pvc-1"}])
}

func TestNewClusterContext_EmptySlice(t *testing.T) {
	ctx := NewClusterContext(nil)
	require.NotNil(t, ctx)
	assert.Empty(t, ctx.Latest)
	assert.Empty(t, ctx.PVCToVMs)
}

func TestDetectSharedPVCs_NilClusterContext(t *testing.T) {
	d1 := vmDigestForPlacement("vm-a", "ns", "n1", 4000, 8<<20, 100<<30)
	d1.PVCs = []PVCDigest{
		{PVCName: "shared-pvc", DiskCapacityBytes: 100 << 30, VolumeMode: "Filesystem"},
	}
	cfg := DefaultVMRecConfig()
	notifs, shared := DetectSharedPVCs(nil, d1, cfg)
	assert.Nil(t, notifs)
	assert.False(t, shared)
}
