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

var testVMClusterUUID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func vmTestTerm() TermWindow {
	return TermWindow{Name: "short_term", LookbackDays: 7, MinDataDays: 3}
}

func vmDigestDays(base time.Time, n int, mutate func(*model.DailyVMDigest)) []model.DailyVMDigest {
	out := make([]model.DailyVMDigest, n)
	for i := 0; i < n; i++ {
		d := model.DailyVMDigest{
			OrgID:                 "org-test",
			ClusterUUID:           testVMClusterUUID,
			VMName:                "test-vm",
			Namespace:             "test-ns",
			GuestOS:               "linux",
			BucketDate:            base.AddDate(0, 0, i),
			CPURequestMC:          4000,
			CPULimitMC:            8000,
			MemRequestKiB:         8 * 1024 * 1024,
			CPUUsageP95MC:         30,
			CPUUsageP99MC:         40,
			MemUsageP95KiB:        100 * 1024,
			MemUsageP99KiB:        120 * 1024,
			DiskAllocatedMaxBytes: 100 * 1024 * 1024 * 1024,
		}
		if mutate != nil {
			mutate(&d)
		}
		out[i] = d
	}
	return out
}

func TestVMRecommend_EmptyDigests_ReturnsError(t *testing.T) {
	rec, err := RecommendVM(nil, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.Error(t, err)
	require.Nil(t, rec)
	assert.Contains(t, err.Error(), "no digests")
}

func TestVMRecommend_AllZeroCPU_TreatedAsIdle(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 0
		d.CPUUsageP99MC = 0
		d.MemUsageP95KiB = 100 * 1024
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.True(t, rec.IsIdle)
	assert.Equal(t, int32(1), rec.RecommendedVCPU)
}

func TestVMRecommend_IdleLinux(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.GuestOS = "linux"
		d.CPUUsageP95MC = 40
		d.MemUsageP95KiB = 400 * 1024
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.True(t, rec.IsIdle)
	assert.Equal(t, int32(1), rec.RecommendedVCPU)
	assert.Equal(t, int32(1), rec.RecommendedMemoryGiB)

	var codes []int16
	require.NoError(t, json.Unmarshal(rec.Notifications, &codes))
	assert.Contains(t, codes, NotifVMIdle)
}

func TestVMRecommend_IdleWindows(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.GuestOS = "Microsoft Windows Server 2022"
		d.CPUUsageP95MC = 150
		d.MemUsageP95KiB = 2 * 1024 * 1024
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.True(t, rec.IsIdle)
	assert.Equal(t, int32(2), rec.RecommendedMemoryGiB)
}

func TestVMRecommend_NonIdleCostEngine(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 4000
		d.CPULimitMC = 4000
		d.CPUUsageP95MC = 5200
		d.MemUsageP95KiB = 6 * 1024 * 1024
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.False(t, rec.IsIdle)
	assert.Equal(t, int32(4), rec.CurrentVCPU)
	assert.Equal(t, int32(6), rec.RecommendedVCPU)
}

func TestVMRecommend_NonIdlePerformanceEngine(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 4000
		d.CPUUsageP95MC = 1000
		d.CPUUsageP99MC = 2000
		d.MemUsageP95KiB = 4 * 1024 * 1024
		d.MemUsageP99KiB = 5 * 1024 * 1024
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEnginePerformance)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, int32(3), rec.RecommendedVCPU)
}

func TestVMRecommend_GuestAgentHighConfidence(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	avail := int64(2 * 1024 * 1024)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 4 * 1024 * 1024
		d.MemAvailableP95KiB = &avail
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.True(t, rec.GuestAgentDetected)
	assert.Equal(t, "high", rec.Confidence)
}

func TestVMRecommend_NoGuestAgentModerateConfidence(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 4 * 1024 * 1024
		d.MemAvailableP95KiB = nil
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.False(t, rec.GuestAgentDetected)
	assert.Equal(t, "moderate", rec.Confidence)
}

func TestVMRecommend_DownsizeHysteresisBlocks(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 8000
		d.CPUUsageP95MC = 5200
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, int32(8), rec.CurrentVCPU)
	assert.Equal(t, int32(8), rec.RecommendedVCPU)
}

func TestVMRecommend_DownsizeHysteresisAllows(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 10000
		d.CPUUsageP95MC = 3000
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, int32(10), rec.CurrentVCPU)
	assert.Equal(t, int32(4), rec.RecommendedVCPU)
}

func TestVMRecommend_OversizedDetection(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 10000
		d.CPUUsageP95MC = 3000
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.True(t, rec.IsOversized)

	var codes []int16
	require.NoError(t, json.Unmarshal(rec.Notifications, &codes))
	assert.Contains(t, codes, NotifVMOversized)
}

func TestVMRecommend_DiskProjectionGrowth(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	usedEarly := int64(50 * 1024 * 1024 * 1024)
	usedMid := int64(65 * 1024 * 1024 * 1024)
	usedLate := int64(80 * 1024 * 1024 * 1024)
	capacity := int64(200 * 1024 * 1024 * 1024)

	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.VMName = "disk-vm"
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 2 * 1024 * 1024
		d.FilesystemCapacityBytes = &capacity
	})
	digests[0].FilesystemUsedMaxBytes = &usedEarly
	digests[1].FilesystemUsedMaxBytes = &usedMid
	digests[2].FilesystemUsedMaxBytes = &usedLate

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.DiskDaysUntilFull)
	require.NotNil(t, rec.DiskGrowthGiBPerDay)
	require.NotNil(t, rec.DiskRecommendedExpandGiB)
	assert.Greater(t, *rec.DiskDaysUntilFull, int32(0))
}

func TestVMRecommend_DiskProjectionNoFilesystem(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.FilesystemUsedMaxBytes = nil
		d.FilesystemCapacityBytes = nil
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, rec.DiskDaysUntilFull)
	assert.Nil(t, rec.DiskGrowthGiBPerDay)
	assert.Nil(t, rec.DiskRecommendedExpandGiB)
}

func TestVMRecommend_HighIOProfile(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	iops := int64(5000)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.DiskReadIOPSP95 = &iops
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.IOHint)
	assert.Equal(t, vmIOHintHigh, *rec.IOHint)
}

func TestVMClassifySeries_ComputeOptimized(t *testing.T) {
	assert.Equal(t, vmSeriesComputeOptimized, vmClassifySeries(8, 2, false))
}

func TestVMClassifySeries_MemoryOptimized(t *testing.T) {
	assert.Equal(t, vmSeriesMemoryOptimized, vmClassifySeries(2, 32, false))
}

func TestVMClassifySeries_GeneralPurpose(t *testing.T) {
	assert.Equal(t, vmSeriesGeneralPurpose, vmClassifySeries(4, 8, false))
	assert.Equal(t, vmSeriesGeneralPurpose, vmClassifySeries(2, 4, true))
}

func TestVMRecommend_InstanceSeriesFromRecommendation(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.EnableInstanceTypeMatching = true

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 32000
		d.CPUUsageP95MC = 28000
		d.MemRequestKiB = 2 * 1024 * 1024
		d.MemUsageP95KiB = 1 * 1024 * 1024
	})

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.RecommendedSeries)
	assert.Equal(t, vmSeriesComputeOptimized, *rec.RecommendedSeries)
}
