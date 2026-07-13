package vm

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
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
			SampleCount:           96,
		}
		if mutate != nil {
			mutate(&d)
		}
		if d.MemAvailableP95KiB != nil && d.AgentSampleCount == 0 {
			d.AgentSampleCount = d.SampleCount
		}
		if d.CPUUsageMaxMC == 0 && d.CPUUsageP95MC > 0 {
			d.CPUUsageMaxMC = d.CPUUsageP99MC
			if d.CPUUsageMaxMC < d.CPUUsageP95MC {
				d.CPUUsageMaxMC = d.CPUUsageP95MC
			}
		}
		if d.MemUsageMaxKiB == 0 && d.MemUsageP95KiB > 0 {
			d.MemUsageMaxKiB = d.MemUsageP99KiB
			if d.MemUsageMaxKiB < d.MemUsageP95KiB {
				d.MemUsageMaxKiB = d.MemUsageP95KiB
			}
		}
		out[i] = d
	}
	return out
}

func TestVMRecommend_EmptyDigests_ReturnsError(t *testing.T) {
	rec, err := RecommendVM(nil, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEnginePerformance, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, int32(3), rec.RecommendedVCPU)
}

func TestDetermineVMConfidence_NewVMWithAgentFromBoot(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 1, func(d *model.DailyVMDigest) {
		d.SampleCount = 96
		d.AgentSampleCount = 96
		avail := int64(2 * 1024 * 1024)
		d.MemAvailableP95KiB = &avail
	})

	confidence, useAgent := DetermineVMConfidence(digests)
	assert.Equal(t, "high", confidence)
	assert.True(t, useAgent)
}

func TestDetermineVMConfidence_AgentInstalledMidDay(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 2, nil)
	digests[0].SampleCount = 96
	digests[0].AgentSampleCount = 70
	digests[1].SampleCount = 96
	digests[1].AgentSampleCount = 96
	avail := int64(2 * 1024 * 1024)
	digests[1].MemAvailableP95KiB = &avail

	confDay1, _ := DetermineVMConfidence(digests[:1])
	assert.Equal(t, "moderate", confDay1)

	confDay2, useAgent := DetermineVMConfidence(digests)
	assert.Equal(t, "high", confDay2)
	assert.True(t, useAgent)
}

func TestDetermineVMConfidence_AgentInstalled2HoursIn(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 1, func(d *model.DailyVMDigest) {
		d.SampleCount = 96
		d.AgentSampleCount = 88
		avail := int64(2 * 1024 * 1024)
		d.MemAvailableP95KiB = &avail
	})

	confidence, useAgent := DetermineVMConfidence(digests)
	assert.Equal(t, "high", confidence)
	assert.True(t, useAgent)
}

func TestConfidence_AgentRemoved(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	avail := int64(2 * 1024 * 1024)
	digests := vmDigestDays(base, 7, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 4 * 1024 * 1024
	})
	for i := 0; i < 5; i++ {
		digests[i].AgentSampleCount = 96
		digests[i].MemAvailableP95KiB = &avail
	}
	for i := 5; i < 7; i++ {
		digests[i].AgentSampleCount = 0
		digests[i].MemAvailableP95KiB = nil
	}

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "moderate", rec.Confidence)
	assert.False(t, rec.GuestAgentDetected)

	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	require.NotNil(t, vmHasNotificationCode(notifs, NotifVMGuestAgentInterrupted))
	assert.Nil(t, vmHasNotificationCode(notifs, NotifVMNoGuestAgent))
}

func TestConfidence_Flapping(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	avail := int64(2 * 1024 * 1024)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 4 * 1024 * 1024
		d.SampleCount = 96
		d.AgentSampleCount = 30
		d.MemAvailableP95KiB = &avail
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "moderate", rec.Confidence)
	assert.False(t, rec.GuestAgentDetected)
	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	require.NotNil(t, vmHasNotificationCode(notifs, NotifVMGuestAgentInterrupted))
}

func TestConfidence_NeverHadAgent(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 4 * 1024 * 1024
		d.AgentSampleCount = 0
		d.MemAvailableP95KiB = nil
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "moderate", rec.Confidence)
	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	require.NotNil(t, vmHasNotificationCode(notifs, NotifVMNoGuestAgent))
	assert.Nil(t, vmHasNotificationCode(notifs, NotifVMGuestAgentInterrupted))
}

func TestConfidence_MinSampleThreshold(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	avail := int64(2 * 1024 * 1024)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 5 * 1024 * 1024
	})
	digests[2].AgentSampleCount = 10
	digests[2].MemAvailableP95KiB = &avail

	confidence, useAgent := DetermineVMConfidence(digests)
	assert.Equal(t, "moderate", confidence)
	assert.False(t, useAgent)
}

func TestConfidence_LessThanOneDay(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 4 * 1024 * 1024
	})
	digests[2].SampleCount = 15
	digests[2].AgentSampleCount = 0

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "low", rec.Confidence)
	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	require.NotNil(t, vmHasNotificationCode(notifs, NotifVMInsufficientData))
}

func TestDiskProjection_StrategyA_Requires2Days(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	used := int64(50 * 1024 * 1024 * 1024)
	capacity := int64(200 * 1024 * 1024 * 1024)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 2 * 1024 * 1024
		d.DiskAllocatedMaxBytes = 100 * 1024 * 1024 * 1024
	})
	digests[2].FilesystemUsedMaxBytes = &used
	digests[2].FilesystemCapacityBytes = &capacity

	daysUntil, growth, expand, hypervisor := vmDiskProjection(digests, DefaultVMRecConfig())
	assert.Nil(t, daysUntil)
	assert.Nil(t, growth)
	// Strategy B may still recommend expansion from allocation trend when 3 alloc days exist
	_ = expand
	_ = hypervisor
}

func TestDiskProjection_StrategyA_Has2Days(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	usedEarly := int64(50 * 1024 * 1024 * 1024)
	usedLate := int64(80 * 1024 * 1024 * 1024)
	capacity := int64(200 * 1024 * 1024 * 1024)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 2 * 1024 * 1024
	})
	digests[1].FilesystemUsedMaxBytes = &usedEarly
	digests[1].FilesystemCapacityBytes = &capacity
	digests[2].FilesystemUsedMaxBytes = &usedLate
	digests[2].FilesystemCapacityBytes = &capacity

	daysUntil, growth, expand, hypervisor := vmDiskProjection(digests, DefaultVMRecConfig())
	require.NotNil(t, daysUntil)
	require.NotNil(t, growth)
	require.NotNil(t, expand)
	assert.False(t, hypervisor)
}

func TestSizingSource_HighConfidence_UsesAvailable(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	avail := int64(6 * 1024 * 1024)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 2 * 1024 * 1024
		d.MemAvailableP95KiB = &avail
		d.AgentSampleCount = 96
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "high", rec.Confidence)
	// working set ~2 GiB + margin → below hypervisor p95 path (~2 GiB usage + margin would differ)
	assert.LessOrEqual(t, rec.RecommendedMemoryGiB, int32(3))
}

func TestSizingSource_ModerateConfidence_UsesUsage(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 5 * 1024 * 1024
		d.AgentSampleCount = 0
		d.MemAvailableP95KiB = nil
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "moderate", rec.Confidence)
	assert.GreaterOrEqual(t, rec.RecommendedMemoryGiB, int32(6))
}

func TestVMRecommend_GuestAgentHighConfidence(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	avail := int64(2 * 1024 * 1024)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 4 * 1024 * 1024
		d.MemAvailableP95KiB = &avail
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.True(t, rec.IsOversized)

	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	n := vmHasNotificationCode(notifs, NotifVMOversized)
	require.NotNil(t, n)
	assert.Equal(t, vmNotifTypeWarning, n.Type)
	assert.Contains(t, n.Message, "oversized")
}

func TestVMRecommend_OversizedOnlyOneDimension_NotOversized(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	t.Run("cpu_low_memory_high", func(t *testing.T) {
		digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
			d.CPURequestMC = 8000
			d.CPULimitMC = 8000
			d.CPUUsageP95MC = 2000
			d.MemRequestKiB = 4 * 1024 * 1024
			d.MemUsageP95KiB = 6 * 1024 * 1024
		})
		rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, rec)
		assert.False(t, rec.IsOversized, "VM needing more memory should not be oversized even if CPU recommends less")
	})

	t.Run("cpu_high_memory_low", func(t *testing.T) {
		digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
			d.CPURequestMC = 2000
			d.CPULimitMC = 2000
			d.CPUUsageP95MC = 3500
			d.MemRequestKiB = 16 * 1024 * 1024
			d.MemUsageP95KiB = 1 * 1024 * 1024
		})
		rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, rec)
		assert.False(t, rec.IsOversized, "VM needing more CPU should not be oversized even if memory recommends less")
	})
}

func TestVMRecommend_OversizedBothDimensions(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 10000
		d.CPULimitMC = 10000
		d.CPUUsageP95MC = 2000
		d.MemRequestKiB = 16 * 1024 * 1024
		d.MemUsageP95KiB = 1 * 1024 * 1024
	})
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.True(t, rec.IsOversized, "VM oversized on both CPU and memory should be classified as oversized")
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.IOHint)
	assert.Equal(t, vmIOHintHigh, *rec.IOHint)
}

func TestVMClassifySeries_ComputeOptimized(t *testing.T) {
	cfg := DefaultVMRecConfig()
	assert.Equal(t, vmSeriesComputeOptimized, vmClassifySeries(nil, 8, 2, false, cfg))
}

func TestVMClassifySeries_MemoryOptimized(t *testing.T) {
	cfg := DefaultVMRecConfig()
	assert.Equal(t, vmSeriesMemoryOptimized, vmClassifySeries(nil, 2, 32, false, cfg))
}

func TestVMClassifySeries_GeneralPurpose(t *testing.T) {
	cfg := DefaultVMRecConfig()
	assert.Equal(t, vmSeriesGeneralPurpose, vmClassifySeries(nil, 4, 8, false, cfg))
	assert.Equal(t, vmSeriesGeneralPurpose, vmClassifySeries(nil, 2, 4, true, cfg))
}

func TestVMRecommend_NoGuestAgentNotification(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 3000
		d.MemUsageP95KiB = 4 * 1024 * 1024
		d.MemAvailableP95KiB = nil
	})

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)

	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	assert.NotNil(t, vmHasNotificationCode(notifs, NotifVMOversized))
	assert.NotNil(t, vmHasNotificationCode(notifs, NotifVMNoGuestAgent))
	assert.NotNil(t, vmHasNotificationCode(notifs, NotifVMHighIO))
}

func TestVMRecommend_HappyPathEmptyNotifications(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	avail := int64(1 * 1024 * 1024)
	used := int64(40 * 1024 * 1024 * 1024)
	capacity := int64(200 * 1024 * 1024 * 1024)

	cfg := DefaultVMRecConfig()
	cfg.EnableInstanceTypeMatching = false

	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 4000
		d.CPULimitMC = 4000
		d.CPUUsageP95MC = 3400
		d.MemRequestKiB = 8 * 1024 * 1024
		d.MemUsageP95KiB = 3 * 1024 * 1024
		d.MemAvailableP95KiB = &avail
		d.FilesystemUsedMaxBytes = &used
		d.FilesystemCapacityBytes = &capacity
	})

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
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

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.RecommendedSeries)
	assert.Equal(t, vmSeriesComputeOptimized, *rec.RecommendedSeries)
	require.NotNil(t, rec.RecommendedInstanceType)
	assert.Equal(t, "cx1.8xlarge", *rec.RecommendedInstanceType)
}

func vmDigestDaysAllZero(base time.Time, n int) []model.DailyVMDigest {
	return vmDigestDays(base, n, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 0
		d.CPUUsageP99MC = 0
		d.CPUUsageMaxMC = 0
		d.MemUsageP95KiB = 0
		d.MemUsageP99KiB = 0
		d.MemUsageMaxKiB = 0
	})
}

func TestVMAbandoned_AllZeroUsage(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDaysAllZero(base, 5)

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.True(t, rec.IsAbandoned)
	assert.False(t, rec.IsIdle)
	assert.Equal(t, int32(0), rec.RecommendedVCPU)
	assert.Equal(t, int32(0), rec.RecommendedMemoryGiB)

	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	n := vmHasNotificationCode(notifs, NotifVMAbandoned)
	require.NotNil(t, n)
	assert.Equal(t, vmNotifTypeCritical, n.Type)
	assert.Contains(t, n.Message, "abandoned")
	assert.Contains(t, n.Message, "5 days")
	assert.Nil(t, vmHasNotificationCode(notifs, NotifVMIdle))
}

func TestVMAbandoned_InsufficientDays(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDaysAllZero(base, 2)

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Nil(t, rec, "below MinDataDays for term")
}

func TestVMAbandoned_PartialUsage(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDaysAllZero(base, 4)
	digests[3].CPUUsageMaxMC = 10
	digests[3].MemUsageMaxKiB = 0
	digests[3].CPUUsageP95MC = 10

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.False(t, rec.IsAbandoned)
}

func TestVMAbandoned_SupersedesIdle(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDaysAllZero(base, 3)

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.True(t, rec.IsAbandoned)
	assert.False(t, rec.IsIdle)

	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	assert.NotNil(t, vmHasNotificationCode(notifs, NotifVMAbandoned))
	assert.Nil(t, vmHasNotificationCode(notifs, NotifVMIdle))
}

func TestVMAbandoned_RecommendsZero(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDaysAllZero(base, 3)
	digests[0].CPURequestMC = 8000
	digests[0].MemRequestKiB = 16 * 1024 * 1024

	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, int32(0), rec.RecommendedVCPU)
	assert.Equal(t, int32(0), rec.RecommendedMemoryGiB)
}

func TestVMAbandoned_ConfigurableThreshold(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDaysAllZero(base, 3)

	cfg := DefaultVMRecConfig()
	cfg.AbandonedMinDays = 5

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.False(t, rec.IsAbandoned)
}

func TestWindows_KernelReserveSubtracted(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	usageKiB := int64(10 * 1024 * 1024)
	availKiB := int64(22 * 1024 * 1024) // 32 GiB request - 10 GiB working set
	makeWindows := func(d *model.DailyVMDigest) {
		d.GuestOS = "windows"
		d.MemRequestKiB = 32 * 1024 * 1024
		d.MemUsageP95KiB = usageKiB
		d.MemAvailableP95KiB = &availKiB
		d.AgentSampleCount = d.SampleCount
	}
	winDigests := vmDigestDays(base, 3, makeWindows)
	linDigests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.GuestOS = "linux"
		d.MemRequestKiB = 32 * 1024 * 1024
		d.MemUsageP95KiB = usageKiB
		d.MemAvailableP95KiB = &availKiB
		d.AgentSampleCount = d.SampleCount
	})

	cfg := DefaultVMRecConfig()
	winRec, err := RecommendVM(winDigests, cfg, vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, winRec)
	linRec, err := RecommendVM(linDigests, cfg, vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, linRec)
	assert.Less(t, winRec.RecommendedMemoryGiB, linRec.RecommendedMemoryGiB)
}

func TestWindows_KernelReserveDoesNotGoBelowFloor(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	avail := int64(32*1024*1024 - 100*1024) // tiny working set
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.GuestOS = "windows"
		d.MemRequestKiB = 32 * 1024 * 1024
		d.MemUsageP95KiB = 100 * 1024
		d.MemAvailableP95KiB = &avail
		d.AgentSampleCount = d.SampleCount
	})
	cfg := DefaultVMRecConfig()
	cfg.WindowsMemoryFloorGiB = 2
	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.GreaterOrEqual(t, rec.RecommendedMemoryGiB, cfg.WindowsMemoryFloorGiB)
}

func TestWindows_KernelReserveConfigurable(t *testing.T) {
	saved := defaultVMRecConfig
	t.Cleanup(func() { defaultVMRecConfig = saved })

	t.Setenv("ROS_VM_WINDOWS_KERNEL_RESERVE_GIB", "4")
	config.ResetForTest()
	InitVMRecDefaults(config.GetConfig())
	assert.InDelta(t, 4.0, VMRecConfigResolved().WindowsKernelReserveGiB, 1e-9)
}

func TestWindowsUpdateSpike_NotificationTriggered(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.GuestOS = "windows"
		d.CPUUsageP95MC = 1000
		d.CPUUsageP99MC = 2000
	})
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	require.NotNil(t, vmHasNotificationCode(notifs, NotifVMWindowsUpdateSpike))
}

func TestWindowsUpdateSpike_NoNotificationWhenSmallSpread(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.GuestOS = "windows"
		d.CPUUsageP95MC = 1000
		d.CPUUsageP99MC = 1100
		d.MemUsageP95KiB = 100 * 1024
		d.MemUsageP99KiB = 110 * 1024
	})
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMWindowsUpdateSpike))
}

func TestWindowsUpdateSpike_OnlyForWindows(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.GuestOS = "linux"
		d.CPUUsageP95MC = 1000
		d.CPUUsageP99MC = 2000
	})
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMWindowsUpdateSpike))
}

func TestCrashLoop_NotificationTriggered(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.RestartCountSum = 2
	})
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	notifs := vmUnmarshalNotifications(t, rec.Notifications)
	n := vmHasNotificationCode(notifs, NotifVMCrashLoop)
	require.NotNil(t, n)
	assert.Contains(t, n.Message, "6")
}

func TestCrashLoop_BelowThreshold(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 2, func(d *model.DailyVMDigest) {
		d.RestartCountSum = 1
	})
	term := TermWindow{Name: "short", LookbackDays: 7, MinDataDays: 2}
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), term, vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMCrashLoop))
}

func TestCrashLoop_NilRestartCount(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, nil)
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Nil(t, vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMCrashLoop))
}

func TestUnknownOS_NotificationAdded(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.GuestOS = ""
	})
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMUnknownOS))
}

func TestUnknownOS_UsesLinuxDefaults(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.GuestOS = ""
		d.CPUUsageP95MC = 40
		d.MemUsageP95KiB = 400 * 1024
	})
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.True(t, rec.IsIdle)
}

func TestDownsizeStability_AllDaysBelow_RecommendsDownsize(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 10000
		d.CPUUsageP95MC = 2000
		d.CPUUsageP99MC = 2100
	})
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEnginePerformance, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Less(t, rec.RecommendedVCPU, rec.CurrentVCPU)
	assert.Nil(t, vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMDownsizeHeld))
}

func TestDownsizeStability_OneDayAbove_HoldsAtCurrent(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 10000
		d.CPUUsageP95MC = 2000
		d.CPUUsageP99MC = 2100
	})
	digests[0].CPUUsageP95MC = 2000
	digests[1].CPUUsageP95MC = 2000
	digests[0].CPUUsageP99MC = 2600
	digests[1].CPUUsageP99MC = 2600
	digests[2].CPUUsageP95MC = 9000
	digests[2].CPUUsageP99MC = 2600
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEnginePerformance, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, int32(10), rec.CurrentVCPU)
	assert.Equal(t, int32(10), rec.RecommendedVCPU)
	require.NotNil(t, vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMDownsizeHeld))
}

func TestDownsizeStability_OnlyPerformanceEngine(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 10000
		d.CPUUsageP95MC = 2000
		d.CPUUsageP99MC = 2100
	})
	rec, err := RecommendVM(digests, DefaultVMRecConfig(), vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Less(t, rec.RecommendedVCPU, rec.CurrentVCPU)
	assert.Nil(t, vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMDownsizeHeld))
}

func TestDownsizeStability_Configurable(t *testing.T) {
	saved := defaultVMRecConfig
	t.Cleanup(func() { defaultVMRecConfig = saved })

	t.Setenv("ROS_VM_DOWNSIZE_STABILITY_DAYS", "5")
	config.ResetForTest()
	InitVMRecDefaults(config.GetConfig())
	assert.Equal(t, 5, VMRecConfigResolved().DownsizeStabilityDays)
}

func TestDownsizeStability_InsufficientDays(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	term := TermWindow{Name: "short", LookbackDays: 7, MinDataDays: 2}
	digests := vmDigestDays(base, 2, func(d *model.DailyVMDigest) {
		d.CPURequestMC = 10000
		d.CPUUsageP95MC = 2000
		d.CPUUsageP99MC = 2100
	})
	cfg := DefaultVMRecConfig()
	cfg.DownsizeStabilityDays = 3
	rec, err := RecommendVM(digests, cfg, term, vmEnginePerformance, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, rec.CurrentVCPU, rec.RecommendedVCPU)
	require.NotNil(t, vmHasNotificationCode(vmUnmarshalNotifications(t, rec.Notifications), NotifVMDownsizeHeld))
}
