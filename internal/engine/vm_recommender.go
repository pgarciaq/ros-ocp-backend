package engine

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

const (
	vmEngineCost        = "cost"
	vmEnginePerformance = "performance"

	vmSeriesComputeOptimized = "compute-optimized"
	vmSeriesMemoryOptimized  = "memory-optimized"
	vmSeriesGeneralPurpose   = "general-purpose"

	vmIOHintHigh = "high-io"

	kibPerGiB = 1024 * 1024
)

// RecommendVM computes a VM recommendation from aggregated daily digests.
func RecommendVM(digests []model.DailyVMDigest, cfg VMRecConfig, term TermWindow, engine string, clusterTypes []InstanceType) (*model.VMRecommendation, error) {
	if len(digests) == 0 {
		return nil, fmt.Errorf("recommend VM: no digests")
	}

	windowed := filterVMDigestsByWindow(digests, term.LookbackDays)
	if len(windowed) < term.MinDataDays {
		return nil, nil
	}

	latest := latestVMDigest(windowed)
	isWindows := vmIsWindows(latest.GuestOS)
	guestOS := latest.GuestOS

	orgID := latest.OrgID
	clusterUUID := latest.ClusterUUID

	currentVCPU := vmCurrentVCPU(latest)
	currentMemGiB := vmCurrentMemoryGiB(latest)
	currentDiskGiB := vmCurrentDiskGiB(latest)

	abandonedMinDays := int(cfg.AbandonedMinDays)
	if abandonedMinDays < 1 {
		abandonedMinDays = 1
	}
	isAbandoned := DetectVMAbandoned(windowed, abandonedMinDays)

	idleCPUThreshold := cfg.IdleCPUMC
	idleMemKiB := cfg.IdleMemoryMiB * 1024
	if isWindows {
		idleCPUThreshold = cfg.IdleCPUMCWindows
		idleMemKiB = cfg.IdleMemoryMiBWindows * 1024
	}

	maxCPUP95 := vmMaxCPUUsage(windowed, engine == vmEnginePerformance)
	maxMemKiB := vmMaxMemoryUsageKiB(windowed, engine, cfg)

	isIdle := !isAbandoned && maxCPUP95 < idleCPUThreshold && maxMemKiB < idleMemKiB

	memFloorGiB := cfg.LinuxMemoryFloorGiB
	if isWindows {
		memFloorGiB = cfg.WindowsMemoryFloorGiB
	}

	var (
		recommendedVCPU      int32
		recommendedMemGiB    int32
		guestAgentDetected   bool
		confidence           string
		rawRecommendedVCPU   int32
		rawRecommendedMemGiB int32
	)

	if isAbandoned {
		recommendedVCPU = 0
		recommendedMemGiB = 0
		rawRecommendedVCPU = 0
		rawRecommendedMemGiB = 0
		guestAgentDetected = vmGuestAgentDetected(windowed)
		if guestAgentDetected {
			confidence = "high"
		} else {
			confidence = "moderate"
		}
	} else if isIdle {
		recommendedVCPU = 1
		recommendedMemGiB = memFloorGiB
		guestAgentDetected = vmGuestAgentDetected(windowed)
		if guestAgentDetected {
			confidence = "high"
		} else {
			confidence = "moderate"
		}
		rawRecommendedVCPU = recommendedVCPU
		rawRecommendedMemGiB = recommendedMemGiB
	} else {
		cpuMargin := cfg.CPUMarginMin
		if engine == vmEnginePerformance {
			cpuMargin = cfg.CPUMarginMax
		}

		recommendedMC := float64(maxCPUP95) * (1 + cpuMargin)
		rawRecommendedVCPU = int32(math.Max(1, math.Ceil(recommendedMC/1000.0)))

		guestAgentDetected = vmGuestAgentDetected(windowed)
		if guestAgentDetected {
			confidence = "high"
			peakActualKiB := vmPeakActualMemoryKiB(windowed, engine)
			recommendedKiB := float64(peakActualKiB) * (1 + cfg.MemMarginMin)
			rawRecommendedMemGiB = int32(math.Max(1, math.Ceil(recommendedKiB/kibPerGiB)))
		} else {
			confidence = "moderate"
			memMargin := cfg.MemMarginMin
			recommendedKiB := float64(maxMemKiB) * (1 + memMargin)
			rawRecommendedMemGiB = int32(math.Max(1, math.Ceil(recommendedKiB/kibPerGiB)))
		}

		if rawRecommendedMemGiB < memFloorGiB {
			rawRecommendedMemGiB = memFloorGiB
		}

		recommendedVCPU = rawRecommendedVCPU
		recommendedMemGiB = rawRecommendedMemGiB

		recommendedVCPU = applyVMDownsizeHysteresisVCPU(
			currentVCPU, recommendedVCPU, rawRecommendedVCPU, cfg,
		)
		recommendedMemGiB = applyVMDownsizeHysteresisMemory(
			currentMemGiB, recommendedMemGiB, rawRecommendedMemGiB, cfg,
		)
	}

	isOversized := rawRecommendedVCPU < currentVCPU || rawRecommendedMemGiB < currentMemGiB

	ioRead, ioWrite, ioReadBPS, ioWriteBPS, ioHint := vmIOProfile(windowed, cfg)

	diskDaysUntilFull, diskGrowthGiBPerDay, diskExpandGiB, hypervisorDiskGrowth :=
		vmDiskProjection(windowed, cfg)

	var (
		recommendedInstanceType *string
		recommendedSeries       *string
	)
	if cfg.EnableInstanceTypeMatching {
		preferredSeries := vmClassifySeries(recommendedVCPU, recommendedMemGiB, isIdle)
		if match := MatchInstanceType(recommendedVCPU, recommendedMemGiB, preferredSeries, clusterTypes); match != nil {
			recommendedInstanceType = &match.Name
			recommendedSeries = &match.Series
		} else {
			recommendedSeries = &preferredSeries
		}
	}

	// TODO(current_instance_type): populate from operator kubevirt_vmi_info labels or a dedicated query.

	notifications := vmBuildNotifications(vmNotificationParams{
		IsIdle:                  isIdle,
		IsAbandoned:             isAbandoned,
		AbandonedDays:           len(windowed),
		IsOversized:             isOversized,
		GuestAgentDetected:      guestAgentDetected,
		IOHint:                  ioHint,
		DiskDaysUntilFull:       diskDaysUntilFull,
		DiskGrowthGiBPerDay:     diskGrowthGiBPerDay,
		HypervisorDiskGrowth:    hypervisorDiskGrowth,
		RecommendedInstanceType: recommendedInstanceType,
		RecommendedSeries:       recommendedSeries,
		FilesystemUsedPct:       vmLatestFilesystemUsedPct(windowed),
	})

	now := time.Now().UTC()
	rec := &model.VMRecommendation{
		OrgID:                    orgID,
		ClusterUUID:              clusterUUID,
		VMName:                   latest.VMName,
		Namespace:                latest.Namespace,
		GuestOS:                  guestOS,
		CurrentVCPU:              currentVCPU,
		CurrentMemoryGiB:         currentMemGiB,
		CurrentDiskGiB:           currentDiskGiB,
		RecommendedVCPU:          recommendedVCPU,
		RecommendedMemoryGiB:     recommendedMemGiB,
		RecommendedDiskGiB:       diskExpandGiB,
		RecommendedInstanceType:  recommendedInstanceType,
		RecommendedSeries:        recommendedSeries,
		GuestAgentDetected:       guestAgentDetected,
		Confidence:               confidence,
		Term:                     term.Name,
		Engine:                   engine,
		IsIdle:                   isIdle,
		IsAbandoned:              isAbandoned,
		IsOversized:              isOversized,
		IOReadIOPSP95:            ioRead,
		IOWriteIOPSP95:           ioWrite,
		IOReadBPS95:              ioReadBPS,
		IOWriteBPS95:             ioWriteBPS,
		IOHint:                   ioHint,
		DiskDaysUntilFull:        diskDaysUntilFull,
		DiskGrowthGiBPerDay:      diskGrowthGiBPerDay,
		DiskRecommendedExpandGiB: diskExpandGiB,
		Notifications:            notifications,
		LastRecommendedAt:        now,
		CreatedAt:                now,
		UpdatedAt:                now,
	}

	return rec, nil
}

func vmIsWindows(guestOS string) bool {
	return strings.Contains(strings.ToLower(guestOS), "windows")
}

func filterVMDigestsByWindow(rows []model.DailyVMDigest, windowDays int) []model.DailyVMDigest {
	if len(rows) == 0 || windowDays < 1 {
		return nil
	}
	latest := latestVMDigest(rows)
	endDay := latest.BucketDate.Truncate(24 * time.Hour)
	cutoffDay := endDay.AddDate(0, 0, -(windowDays - 1))

	out := make([]model.DailyVMDigest, 0, len(rows))
	for _, r := range rows {
		d := r.BucketDate.Truncate(24 * time.Hour)
		if d.Before(cutoffDay) || d.After(endDay) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func latestVMDigest(rows []model.DailyVMDigest) model.DailyVMDigest {
	best := rows[0]
	for _, r := range rows[1:] {
		if r.BucketDate.After(best.BucketDate) {
			best = r
		}
	}
	return best
}

func vmCurrentVCPU(d model.DailyVMDigest) int32 {
	mc := d.CPURequestMC
	if d.CPULimitMC > mc {
		mc = d.CPULimitMC
	}
	if mc <= 0 {
		return 1
	}
	return int32(math.Max(1, math.Ceil(float64(mc)/1000.0)))
}

func vmCurrentMemoryGiB(d model.DailyVMDigest) int32 {
	if d.MemRequestKiB <= 0 {
		return 1
	}
	return int32(math.Max(1, math.Ceil(float64(d.MemRequestKiB)/float64(kibPerGiB))))
}

func vmCurrentDiskGiB(d model.DailyVMDigest) *int32 {
	if d.DiskAllocatedMaxBytes <= 0 {
		return nil
	}
	gib := int32(math.Ceil(float64(d.DiskAllocatedMaxBytes) / float64(1024*1024*1024)))
	return &gib
}

func vmMaxCPUUsage(days []model.DailyVMDigest, useP99 bool) int64 {
	var peak int64
	for _, d := range days {
		v := d.CPUUsageP95MC
		if useP99 {
			v = d.CPUUsageP99MC
		}
		if v > peak {
			peak = v
		}
	}
	return peak
}

func vmMaxMemoryUsageKiB(days []model.DailyVMDigest, engine string, cfg VMRecConfig) int64 {
	useP99 := engine == vmEnginePerformance
	var peak int64
	for _, d := range days {
		v := d.MemUsageP95KiB
		if useP99 {
			v = d.MemUsageP99KiB
		}
		if v > peak {
			peak = v
		}
	}
	return peak
}

func vmGuestAgentDetected(days []model.DailyVMDigest) bool {
	for _, d := range days {
		if d.MemAvailableP95KiB != nil {
			return true
		}
	}
	return false
}

func vmPeakActualMemoryKiB(days []model.DailyVMDigest, engine string) int64 {
	var peak int64
	for _, d := range days {
		if d.MemAvailableP95KiB == nil {
			continue
		}
		avail := *d.MemAvailableP95KiB
		actual := d.MemRequestKiB - avail
		if actual < 0 {
			actual = 0
		}
		if actual > peak {
			peak = actual
		}
	}
	if peak == 0 {
		return vmMaxMemoryUsageKiB(days, engine, VMRecConfig{})
	}
	return peak
}

func applyVMDownsizeHysteresisVCPU(current, recommended, raw int32, cfg VMRecConfig) int32 {
	if raw >= current {
		return recommended
	}
	threshold := float64(current) * cfg.DownsizeHysteresisRatio
	if float64(raw) <= threshold && current-raw >= cfg.MinVCPUChange {
		return raw
	}
	return current
}

func applyVMDownsizeHysteresisMemory(current, recommended, raw int32, cfg VMRecConfig) int32 {
	if raw >= current {
		return recommended
	}
	threshold := float64(current) * cfg.DownsizeHysteresisRatio
	if float64(raw) <= threshold && current-raw >= cfg.MinGiBChange {
		return raw
	}
	return current
}

func vmIOProfile(days []model.DailyVMDigest, cfg VMRecConfig) (readIOPS, writeIOPS, readBPS, writeBPS *int64, hint *string) {
	var peakRead, peakWrite int64
	for _, d := range days {
		if d.DiskReadIOPSP95 != nil && *d.DiskReadIOPSP95 > peakRead {
			peakRead = *d.DiskReadIOPSP95
		}
		if d.DiskWriteIOPSP95 != nil && *d.DiskWriteIOPSP95 > peakWrite {
			peakWrite = *d.DiskWriteIOPSP95
		}
	}
	if peakRead > 0 {
		readIOPS = &peakRead
	}
	if peakWrite > 0 {
		writeIOPS = &peakWrite
	}

	var peakReadBPS, peakWriteBPS int64
	for _, d := range days {
		if d.DiskReadBPS95 != nil && *d.DiskReadBPS95 > peakReadBPS {
			peakReadBPS = *d.DiskReadBPS95
		}
		if d.DiskWriteBPS95 != nil && *d.DiskWriteBPS95 > peakWriteBPS {
			peakWriteBPS = *d.DiskWriteBPS95
		}
	}
	if peakReadBPS > 0 {
		readBPS = &peakReadBPS
	}
	if peakWriteBPS > 0 {
		writeBPS = &peakWriteBPS
	}

	maxIOPS := peakRead
	if peakWrite > maxIOPS {
		maxIOPS = peakWrite
	}
	if maxIOPS > cfg.HighIOPSThreshold {
		h := vmIOHintHigh
		hint = &h
	}
	return readIOPS, writeIOPS, readBPS, writeBPS, hint
}

const vmBytesPerGiB = 1024 * 1024 * 1024

// vmDiskProjection returns disk growth/expansion signals. Strategy A (guest-agent
// filesystem) runs when filesystem metrics exist; otherwise Strategy B uses hypervisor
// disk_allocated_max_bytes trending.
func vmDiskProjection(days []model.DailyVMDigest, cfg VMRecConfig) (
	daysUntilFull *int32, growthGiBPerDay *float64, expandGiB *int32, hypervisorDiskGrowth bool,
) {
	if vmHasGuestAgentFilesystemData(days) {
		return vmDiskProjectionGuestAgent(days, cfg)
	}
	return vmDiskProjectionHypervisor(days, cfg)
}

func vmHasGuestAgentFilesystemData(days []model.DailyVMDigest) bool {
	for _, d := range days {
		if d.FilesystemUsedMaxBytes != nil && d.FilesystemCapacityBytes != nil {
			return true
		}
	}
	return false
}

func vmDiskProjectionGuestAgent(days []model.DailyVMDigest, cfg VMRecConfig) (
	daysUntilFull *int32, growthGiBPerDay *float64, expandGiB *int32, hypervisorDiskGrowth bool,
) {
	var fsDays []model.DailyVMDigest
	for _, d := range days {
		if d.FilesystemUsedMaxBytes != nil && d.FilesystemCapacityBytes != nil {
			fsDays = append(fsDays, d)
		}
	}
	if len(fsDays) < 2 {
		return nil, nil, nil, false
	}

	sortVMDigestsByDate(fsDays)
	earliest := fsDays[0]
	latest := fsDays[len(fsDays)-1]

	usedEarliest := *earliest.FilesystemUsedMaxBytes
	usedLatest := *latest.FilesystemUsedMaxBytes
	capacity := *latest.FilesystemCapacityBytes

	daysBetween := int(latest.BucketDate.Sub(earliest.BucketDate).Hours() / 24)
	if daysBetween < 1 {
		daysBetween = 1
	}

	growthPerDay := float64(usedLatest-usedEarliest) / float64(daysBetween)
	if growthPerDay <= 0 {
		return nil, nil, nil, false
	}

	growthGiB := growthPerDay / float64(vmBytesPerGiB)
	growthGiBPerDay = &growthGiB

	remaining := float64(capacity - usedLatest)
	daysFull := int32(math.Floor(remaining / growthPerDay))
	daysUntilFull = &daysFull

	if daysFull >= 90 {
		return daysUntilFull, growthGiBPerDay, nil, false
	}

	expand := vmComputeDiskExpandGiB(growthGiB, cfg)
	expandGiB = &expand
	return daysUntilFull, growthGiBPerDay, expandGiB, false
}

func vmDiskProjectionHypervisor(days []model.DailyVMDigest, cfg VMRecConfig) (
	daysUntilFull *int32, growthGiBPerDay *float64, expandGiB *int32, hypervisorDiskGrowth bool,
) {
	var allocDays []model.DailyVMDigest
	for _, d := range days {
		if d.DiskAllocatedMaxBytes > 0 {
			allocDays = append(allocDays, d)
		}
	}
	if len(allocDays) < 2 {
		return nil, nil, nil, false
	}

	sortVMDigestsByDate(allocDays)
	series := make([]float64, len(allocDays))
	for i, d := range allocDays {
		series[i] = float64(d.DiskAllocatedMaxBytes)
	}

	growthPerDay := linearRegressionSlope(series)
	if growthPerDay <= 0 {
		return nil, nil, nil, false
	}

	minGrowthBytes := float64(cfg.DiskMinGrowthMiBPerDay) * 1024 * 1024
	if growthPerDay < minGrowthBytes {
		return nil, nil, nil, false
	}

	growthGiB := growthPerDay / float64(vmBytesPerGiB)
	growthGiBPerDay = &growthGiB
	hypervisorDiskGrowth = true

	expand := vmComputeDiskExpandGiB(growthGiB, cfg)
	expandGiB = &expand
	return nil, growthGiBPerDay, expandGiB, hypervisorDiskGrowth
}

func vmComputeDiskExpandGiB(growthGiB float64, cfg VMRecConfig) int32 {
	projectedGiB := growthGiB * 180
	withHeadroom := projectedGiB * (1 + cfg.DiskHeadroomPct)
	step := float64(cfg.DiskRoundStepGiB)
	if step < 1 {
		step = 10
	}
	expand := int32(math.Ceil(withHeadroom/step) * step)
	if expand < int32(step) {
		expand = int32(step)
	}
	return expand
}

func sortVMDigestsByDate(days []model.DailyVMDigest) {
	for i := 1; i < len(days); i++ {
		for j := i; j > 0 && days[j].BucketDate.Before(days[j-1].BucketDate); j-- {
			days[j], days[j-1] = days[j-1], days[j]
		}
	}
}

func vmClassifySeries(vcpu, memGiB int32, isIdle bool) string {
	if isIdle || vcpu <= 0 || memGiB <= 0 {
		return vmSeriesGeneralPurpose
	}
	ratio := float64(vcpu) * 4.0 / float64(memGiB)
	if ratio > 2.0 {
		return vmSeriesComputeOptimized
	}
	if ratio < 0.5 {
		return vmSeriesMemoryOptimized
	}
	return vmSeriesGeneralPurpose
}

