package vm

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func vmDigestForPlacement(vmName, ns, node string, vcpuMC, memKiB int64, diskBytes int64) Digest {
	return Digest{
		OrgID:                 "org1",
		ClusterUUID:           uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		VMName:                vmName,
		Namespace:             ns,
		NodeName:              node,
		BucketDate:            time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		CPURequestMC:          vcpuMC,
		CPULimitMC:            vcpuMC,
		MemRequestKiB:         memKiB,
		DiskAllocatedMaxBytes: diskBytes,
	}
}

func TestExtractVMNamePrefix(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"web-server-01", "web-server"},
		{"db-replica-2", "db-replica"},
		{"myapp-abc12def", "myapp"},
		{"standalone-vm", "standalone-vm"},
		{"a", "a"},
		{"cache-node-a", "cache-node"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, extractVMNamePrefix(tc.name))
		})
	}
}

func TestDetectSameNodeRedundancy_NamePrefixColocation(t *testing.T) {
	cluster := []Digest{
		vmDigestForPlacement("web-server-01", "apps", "node-1", 4000, 8<<20, 100<<30),
		vmDigestForPlacement("web-server-02", "apps", "node-1", 8000, 16<<20, 200<<30),
	}
	cfg := DefaultVMRecConfig()
	notifs := DetectSameNodeRedundancy(cluster, cluster[0], cfg)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMRedundantColocation, notifs[0].Code)
	assert.Contains(t, notifs[0].Message, "web-server-02")
}

func TestDetectSameNodeRedundancy_Colocation(t *testing.T) {
	cluster := []Digest{
		vmDigestForPlacement("vm-a", "apps", "node-1", 4000, 8<<20, 100<<30),
		vmDigestForPlacement("vm-b", "apps", "node-1", 4000, 8<<20, 100<<30),
		vmDigestForPlacement("vm-c", "apps", "node-2", 4000, 8<<20, 100<<30),
	}
	cfg := DefaultVMRecConfig()
	notifs := DetectSameNodeRedundancy(cluster, cluster[0], cfg)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMRedundantColocation, notifs[0].Code)
	assert.Contains(t, notifs[0].Message, "vm-b")
}

func TestDetectSameNodeRedundancy_SkewDistribution(t *testing.T) {
	cluster := []Digest{
		vmDigestForPlacement("vm-1", "apps", "node-a", 2000, 4<<20, 50<<30),
		vmDigestForPlacement("vm-2", "apps", "node-a", 2000, 4<<20, 50<<30),
		vmDigestForPlacement("vm-3", "apps", "node-a", 2000, 4<<20, 50<<30),
		vmDigestForPlacement("vm-4", "apps", "node-b", 2000, 4<<20, 50<<30),
	}
	cfg := DefaultVMRecConfig()
	cfg.PlacementSkewRatio = 3
	notifs := DetectSameNodeRedundancy(cluster, cluster[3], cfg)
	codes := make([]int16, len(notifs))
	for i, n := range notifs {
		codes[i] = n.Code
	}
	assert.Contains(t, codes, NotifVMUnevenNodeDistribution)
}

func TestDetectSharedPVCs_CorrelatedPeers(t *testing.T) {
	cluster := []Digest{
		vmDigestForPlacement("db-primary", "data", "node-1", 8000, 16<<20, 200<<30),
		vmDigestForPlacement("db-standby", "data", "node-2", 8000, 16<<20, 200<<30),
	}
	cfg := DefaultVMRecConfig()
	notifs, shared := DetectSharedPVCs(NewClusterContext(cluster), cluster[0], cfg)
	require.True(t, shared)
	require.Len(t, notifs, 1)
	assert.Equal(t, NotifVMSharedStorage, notifs[0].Code)
	assert.Contains(t, notifs[0].Message, "db-standby")
}

func TestCheckNUMAFit_Oversized(t *testing.T) {
	n := CheckNUMAFit(128, 64)
	require.NotNil(t, n)
	assert.Equal(t, NotifVMNUMAOversized, n.Code)
}

func TestCheckNUMAFit_WithinCapacity(t *testing.T) {
	assert.Nil(t, CheckNUMAFit(32, 64))
	assert.Nil(t, CheckNUMAFit(64, 64))
}

func TestDetectSharedPVCs_NoPeers(t *testing.T) {
	cluster := []Digest{
		vmDigestForPlacement("solo", "apps", "node-1", 4000, 8<<20, 100<<30),
	}
	cfg := DefaultVMRecConfig()
	notifs, shared := DetectSharedPVCs(NewClusterContext(cluster), cluster[0], cfg)
	assert.Nil(t, notifs)
	assert.False(t, shared)
}

func TestDetectSameNodeRedundancy_Disabled(t *testing.T) {
	cluster := []Digest{
		vmDigestForPlacement("vm-a", "apps", "node-1", 4000, 8<<20, 100<<30),
		vmDigestForPlacement("vm-b", "apps", "node-1", 4000, 8<<20, 100<<30),
	}
	cfg := DefaultVMRecConfig()
	cfg.EnablePlacementChecks = false
	assert.Nil(t, DetectSameNodeRedundancy(cluster, cluster[0], cfg))
}

func TestRecommendVM_PlacementFlags(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	disk := int64(100 << 30)
	mk := func(vm, node string) []Digest {
		return vmDigestDays(base, 3, func(d *Digest) {
			d.VMName = vm
			d.Namespace = "ha"
			d.NodeName = node
			d.CPURequestMC = 4000
			d.CPULimitMC = 4000
			d.MemRequestKiB = 8 << 20
			d.DiskAllocatedMaxBytes = disk
			d.CPUUsageP95MC = 3500
			d.CPUUsageP99MC = 3500
			d.MemUsageP95KiB = 7 << 20
			d.MemUsageP99KiB = 7 << 20
		})
	}
	digests := mk("vm-a", "node-1")
	cluster := append(mk("vm-a", "node-1"), mk("vm-b", "node-1")...)
	cfg := DefaultVMRecConfig()
	cfg.NUMANodeMemoryGiB = 4

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil, NewClusterContext(cluster), nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.True(t, rec.IsRedundantPlacement)
	assert.True(t, rec.HasSharedStorage)

	var notifs []VMNotification
	require.NoError(t, json.Unmarshal(rec.Notifications, &notifs))
	codes := make(map[int16]bool)
	for _, n := range notifs {
		codes[n.Code] = true
	}
	assert.True(t, codes[NotifVMRedundantColocation])
	assert.True(t, codes[NotifVMSharedStorage])
	assert.True(t, codes[NotifVMNUMAOversized])
}
