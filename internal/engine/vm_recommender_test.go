package engine

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

var testVMClusterUUID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func vmNotificationCodes(t *testing.T, raw []byte) []int16 {
	t.Helper()
	var notifs []VMNotification
	require.NoError(t, json.Unmarshal(raw, &notifs))
	codes := make([]int16, len(notifs))
	for i, n := range notifs {
		codes[i] = n.Code
	}
	return codes
}

func vmTestTerm() TermWindow {
	return TermWindow{Name: "short_term", LookbackDays: 7, MinDataDays: 3}
}

func vmUnmarshalNotifications(t *testing.T, raw []byte) []VMNotification {
	t.Helper()
	var out []VMNotification
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func vmHasNotificationCode(notifs []VMNotification, code int16) *VMNotification {
	for i := range notifs {
		if notifs[i].Code == code {
			return &notifs[i]
		}
	}
	return nil
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

	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	n := vmHasNotificationCode(notifs, NotifVMIdle)
	require.NotNil(t, n)
	assert.Equal(t, vmNotifTypeWarning, n.Type)
	assert.Contains(t, n.Message, "idle")
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

	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	n := vmHasNotificationCode(notifs, NotifVMOversized)
	require.NotNil(t, n)
	assert.Equal(t, vmNotifTypeWarning, n.Type)
	assert.Contains(t, n.Message, "oversized")
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

func TestDisk_HypervisorGrowthDetected(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 7, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 2 * 1024 * 1024
		d.FilesystemUsedMaxBytes = nil
		d.FilesystemCapacityBytes = nil
	})
	for i := range digests {
		gib := int64((100 + i*5) * 1024 * 1024 * 1024)
		digests[i].DiskAllocatedMaxBytes = gib
	}

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, rec.DiskDaysUntilFull)
	require.NotNil(t, rec.DiskGrowthGiBPerDay)
	assert.Greater(t, *rec.DiskGrowthGiBPerDay, 0.0)
	require.NotNil(t, rec.DiskRecommendedExpandGiB)
	assert.Greater(t, *rec.DiskRecommendedExpandGiB, int32(0))

	codes := vmNotificationCodes(t, rec.Notifications)
	assert.Contains(t, codes, NotifVMDiskGrowingNoCapacity)
}

func TestDisk_HypervisorStable(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	const gib = 100 * 1024 * 1024 * 1024
	digests := vmDigestDays(base, 7, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.FilesystemUsedMaxBytes = nil
		d.FilesystemCapacityBytes = nil
		d.DiskAllocatedMaxBytes = gib
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, rec.DiskDaysUntilFull)
	assert.Nil(t, rec.DiskGrowthGiBPerDay)
	assert.Nil(t, rec.DiskRecommendedExpandGiB)
}

func TestDisk_HypervisorShrinking(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 7, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.FilesystemUsedMaxBytes = nil
		d.FilesystemCapacityBytes = nil
	})
	for i := range digests {
		gib := int64((200 - i*10) * 1024 * 1024 * 1024)
		digests[i].DiskAllocatedMaxBytes = gib
	}

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, rec.DiskGrowthGiBPerDay)
	assert.Nil(t, rec.DiskRecommendedExpandGiB)
}

func TestDisk_HypervisorBelowMinGrowthThreshold(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 7, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.FilesystemUsedMaxBytes = nil
		d.FilesystemCapacityBytes = nil
	})
	// ~50 MiB/day slope (below default 100 MiB/day threshold)
	baseBytes := int64(100 * 1024 * 1024 * 1024)
	step := int64(50 * 1024 * 1024)
	for i := range digests {
		digests[i].DiskAllocatedMaxBytes = baseBytes + step*int64(i)
	}

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, rec.DiskGrowthGiBPerDay)
	assert.Nil(t, rec.DiskRecommendedExpandGiB)
}

func TestDisk_HypervisorInsufficientData(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.FilesystemUsedMaxBytes = nil
		d.FilesystemCapacityBytes = nil
		d.DiskAllocatedMaxBytes = 0
	})
	digests[0].DiskAllocatedMaxBytes = 200 * 1024 * 1024 * 1024

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, rec.DiskGrowthGiBPerDay)
	assert.Nil(t, rec.DiskRecommendedExpandGiB)
}

func TestDisk_GuestAgentUnchanged(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	usedEarly := int64(50 * 1024 * 1024 * 1024)
	usedMid := int64(65 * 1024 * 1024 * 1024)
	usedLate := int64(80 * 1024 * 1024 * 1024)
	capacity := int64(200 * 1024 * 1024 * 1024)

	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
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

	codes := vmNotificationCodes(t, rec.Notifications)
	assert.NotContains(t, codes, NotifVMDiskGrowingNoCapacity)
}

func TestDisk_GuestAgentOverridesHypervisor(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	used := int64(80 * 1024 * 1024 * 1024)
	capacity := int64(200 * 1024 * 1024 * 1024)

	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.FilesystemCapacityBytes = &capacity
	})
	for i := range digests {
		u := used
		digests[i].FilesystemUsedMaxBytes = &u
		// Hypervisor allocation growing — would trigger Strategy B without guest agent
		digests[i].DiskAllocatedMaxBytes = int64((100 + i*20) * 1024 * 1024 * 1024)
	}

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, rec.DiskGrowthGiBPerDay, "flat guest-agent usage should not project growth")
	assert.Nil(t, rec.DiskRecommendedExpandGiB)

	codes := vmNotificationCodes(t, rec.Notifications)
	assert.NotContains(t, codes, NotifVMDiskGrowingNoCapacity)
}

func TestDisk_HypervisorProjectionDirect(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, nil)
	digests[0].DiskAllocatedMaxBytes = 100 * 1024 * 1024 * 1024
	digests[1].DiskAllocatedMaxBytes = 110 * 1024 * 1024 * 1024
	digests[2].DiskAllocatedMaxBytes = 120 * 1024 * 1024 * 1024

	daysUntil, growth, expand, hvGrowth := vmDiskProjectionHypervisor(digests, DefaultVMRecConfig())
	assert.Nil(t, daysUntil)
	require.NotNil(t, growth)
	assert.InDelta(t, 10.0, *growth, 0.01)
	require.NotNil(t, expand)
	assert.True(t, hvGrowth)
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

func TestVMRecommend_NoGuestAgentNotification(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 4 * 1024 * 1024
		d.MemAvailableP95KiB = nil
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)

	n := vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMNoGuestAgent)
	require.NotNil(t, n)
	assert.Equal(t, vmNotifTypeInfo, n.Type)
	assert.Contains(t, n.Message, "guest agent")
}

func TestVMRecommend_HighIONotification(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	iops := int64(5000)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.DiskReadIOPSP95 = &iops
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)

	n := vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMHighIO)
	require.NotNil(t, n)
	assert.Equal(t, vmNotifTypeWarning, n.Type)
	assert.Contains(t, n.Message, "High disk I/O")
}

func TestVMRecommend_DiskFillingGuestAgentNotification(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	usedEarly := int64(50 * 1024 * 1024 * 1024)
	usedMid := int64(65 * 1024 * 1024 * 1024)
	usedLate := int64(80 * 1024 * 1024 * 1024)
	capacity := int64(200 * 1024 * 1024 * 1024)
	avail := int64(2 * 1024 * 1024)

	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 2 * 1024 * 1024
		d.MemAvailableP95KiB = &avail
		d.FilesystemCapacityBytes = &capacity
	})
	digests[0].FilesystemUsedMaxBytes = &usedEarly
	digests[1].FilesystemUsedMaxBytes = &usedMid
	digests[2].FilesystemUsedMaxBytes = &usedLate

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.DiskDaysUntilFull)
	assert.Less(t, *rec.DiskDaysUntilFull, int32(90))

	n := vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMDiskFillingGuest)
	require.NotNil(t, n)
	assert.Contains(t, n.Message, fmt.Sprintf("%d", *rec.DiskDaysUntilFull))
}

func TestVMRecommend_DiskGrowingHypervisorNotification(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	allocEarly := int64(100 * 1024 * 1024 * 1024)
	allocMid := int64(110 * 1024 * 1024 * 1024)
	allocLate := int64(130 * 1024 * 1024 * 1024)

	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 2 * 1024 * 1024
		d.FilesystemUsedMaxBytes = nil
		d.FilesystemCapacityBytes = nil
	})
	digests[0].DiskAllocatedMaxBytes = allocEarly
	digests[1].DiskAllocatedMaxBytes = allocMid
	digests[2].DiskAllocatedMaxBytes = allocLate

	cfg := DefaultVMRecConfig()
	cfg.DiskMinGrowthMiBPerDay = 1

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.DiskGrowthGiBPerDay)

	n := vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMDiskGrowingNoCapacity)
	require.NotNil(t, n)
	assert.Equal(t, vmNotifTypeInfo, n.Type)
	assert.Contains(t, n.Message, "GiB/day")
}

func TestVMRecommend_InstanceTypeNotification(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.EnableInstanceTypeMatching = true

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 8000
		d.CPUUsageP95MC = 7000
		d.MemRequestKiB = 2 * 1024 * 1024
		d.MemUsageP95KiB = 1 * 1024 * 1024
	})

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.RecommendedInstanceType)

	n := vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMInstanceTypeRec)
	require.NotNil(t, n)
	assert.Contains(t, n.Message, *rec.RecommendedInstanceType)
}

func TestVMRecommend_DiskCriticalNotification(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	used := int64(95 * 1024 * 1024 * 1024)
	capacity := int64(100 * 1024 * 1024 * 1024)
	avail := int64(1 * 1024 * 1024)

	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 2 * 1024 * 1024
		d.MemAvailableP95KiB = &avail
		d.FilesystemUsedMaxBytes = &used
		d.FilesystemCapacityBytes = &capacity
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)

	n := vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMDiskCritical)
	require.NotNil(t, n)
	assert.Equal(t, vmNotifTypeCritical, n.Type)
	assert.Contains(t, n.Message, "95%")
}

func TestVMRecommend_MultipleNotifications(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	iops := int64(5000)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 10000
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 4 * 1024 * 1024
		d.MemAvailableP95KiB = nil
		d.DiskReadIOPSP95 = &iops
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)

	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	assert.NotNil(t, vmHasNotificationCode(notifs, NotifVMOversized))
	assert.NotNil(t, vmHasNotificationCode(notifs, NotifVMNoGuestAgent))
	assert.NotNil(t, vmHasNotificationCode(notifs, NotifVMHighIO))
}

func TestVMRecommend_HappyPathEmptyNotifications(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	avail := int64(4 * 1024 * 1024)
	used := int64(40 * 1024 * 1024 * 1024)
	capacity := int64(200 * 1024 * 1024 * 1024)

	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 4000
		d.CPULimitMC = 4000
		d.CPUUsageP95MC = 3500
		d.MemUsageP95KiB = 3 * 1024 * 1024
		d.MemAvailableP95KiB = &avail
		d.FilesystemUsedMaxBytes = &used
		d.FilesystemCapacityBytes = &capacity
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.False(t, rec.IsIdle)
	assert.False(t, rec.IsOversized)

	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	assert.Empty(t, notifs)
}

func TestVMRecommend_InstanceSeriesFromRecommendation(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.EnableInstanceTypeMatching = true

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 20000
		d.CPUUsageP95MC = 20000
		d.MemRequestKiB = 2 * 1024 * 1024
		d.MemUsageP95KiB = 1 * 1024 * 1024
	})

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.RecommendedSeries)
	assert.Equal(t, vmSeriesComputeOptimized, *rec.RecommendedSeries)
	require.NotNil(t, rec.RecommendedInstanceType)
	assert.Equal(t, "cx1.8xlarge", *rec.RecommendedInstanceType)
}
