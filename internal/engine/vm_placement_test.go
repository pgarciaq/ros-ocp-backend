package engine

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func vmDigestForPlacement(vmName, ns, node string, vcpuMC, memKiB int64, diskBytes int64) model.DailyVMDigest {
	return model.DailyVMDigest{
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

func TestDetectSameNodeRedundancy_Colocation(t *testing.T) {
	cluster := []model.DailyVMDigest{
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
	cluster := []model.DailyVMDigest{
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
	cluster := []model.DailyVMDigest{
		vmDigestForPlacement("db-primary", "data", "node-1", 8000, 16<<20, 200<<30),
		vmDigestForPlacement("db-standby", "data", "node-2", 8000, 16<<20, 200<<30),
	}
	cfg := DefaultVMRecConfig()
	notifs, shared := DetectSharedPVCs(cluster, cluster[0], cfg)
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
	cluster := []model.DailyVMDigest{
		vmDigestForPlacement("solo", "apps", "node-1", 4000, 8<<20, 100<<30),
	}
	cfg := DefaultVMRecConfig()
	notifs, shared := DetectSharedPVCs(cluster, cluster[0], cfg)
	assert.Nil(t, notifs)
	assert.False(t, shared)
}

func TestDetectSameNodeRedundancy_Disabled(t *testing.T) {
	cluster := []model.DailyVMDigest{
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
	mk := func(vm, node string) []model.DailyVMDigest {
		return vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
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

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil, cluster)
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
