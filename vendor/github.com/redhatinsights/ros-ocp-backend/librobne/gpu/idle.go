package gpu

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

const GPUZombieThresholdBP = 100 // 1% — admin-only zombie cutoff

// GPUIdleConfig holds GPU-specific idle thresholds.
type GPUIdleConfig struct {
	Enabled            bool
	IdleSMActiveBP     int64 // sm_active P95 below this = idle (default 500 = 5%)
	IdleDRAMActiveBP   int64 // dram_active P95 below this = idle (default 500 = 5%)
	ZombieSMActiveBP   int64 // sm_active P95 below this = zombie (default 100 = 1%)
	ZombieDRAMActiveBP int64 // dram_active P95 below this = zombie (default 100 = 1%)
	MinObservationDays int   // default 7
}

// DefaultGPUIdleConfig returns built-in GPU idle/zombie thresholds (no I/O).
func DefaultGPUIdleConfig() GPUIdleConfig {
	return GPUIdleConfig{
		Enabled:            true,
		IdleSMActiveBP:     500,
		IdleDRAMActiveBP:   500,
		ZombieSMActiveBP:   GPUZombieThresholdBP,
		ZombieDRAMActiveBP: GPUZombieThresholdBP,
		MinObservationDays: 7,
	}
}

// ClassifyGPUIdleState determines if a GPU is zombie, idle, or active from the
// max daily sm_active and dram_active basis points over the observation window.
func ClassifyGPUIdleState(
	smActiveP95BP int64,
	dramActiveP95BP int64,
	observationDays int,
	cfg GPUIdleConfig,
) types.IdleResult {
	result := types.IdleResult{State: types.IdleStateActive}

	if !cfg.Enabled {
		return result
	}
	if observationDays < cfg.MinObservationDays {
		return result
	}

	if smActiveP95BP < cfg.ZombieSMActiveBP && dramActiveP95BP < cfg.ZombieDRAMActiveBP {
		result.State = types.IdleStateZombie
		return result
	}

	if smActiveP95BP < cfg.IdleSMActiveBP && dramActiveP95BP < cfg.IdleDRAMActiveBP {
		result.State = types.IdleStateIdle
		return result
	}

	return result
}

// ClassifyGPUIdleFromDigests classifies GPU idle state from daily digest rows and
// sets idle_since / duration when non-active.
func ClassifyGPUIdleFromDigests(digests []GPUDigestRow, cfg GPUIdleConfig) types.IdleResult {
	if len(digests) == 0 {
		return types.IdleResult{State: types.IdleStateActive}
	}
	smP95 := maxDailyGPUField(digests, func(d GPUDigestRow) int64 { return int64(d.SMActiveAvg) })
	dramP95 := maxDailyGPUField(digests, func(d GPUDigestRow) int64 { return int64(d.DRAMActiveAvg) })
	result := ClassifyGPUIdleState(smP95, dramP95, len(digests), cfg)
	if result.State == types.IdleStateActive {
		return result
	}
	result.IdleSince = findGPUIdleSince(digests, func(d GPUDigestRow) bool {
		sm := int64(d.SMActiveAvg)
		dram := int64(d.DRAMActiveAvg)
		if result.State == types.IdleStateZombie {
			return sm < cfg.ZombieSMActiveBP && dram < cfg.ZombieDRAMActiveBP
		}
		return sm < cfg.IdleSMActiveBP && dram < cfg.IdleDRAMActiveBP
	})
	result.DurationDays = types.ComputeIdleDuration(result.IdleSince)
	return result
}

// maxDailyGPUField returns the maximum daily GPU utilization metric across the window.
// Conservative upper bound for window P95; see maxDailyCPUUsageP95.
func maxDailyGPUField(rows []GPUDigestRow, pick func(GPUDigestRow) int64) int64 {
	var max int64
	for _, r := range rows {
		if v := pick(r); v > max {
			max = v
		}
	}
	return max
}

func findGPUIdleSince(rows []GPUDigestRow, predicate func(GPUDigestRow) bool) *time.Time {
	if len(rows) == 0 {
		return nil
	}
	start := len(rows) - 1
	for start >= 0 && predicate(rows[start]) {
		start--
	}
	firstIdle := start + 1
	if firstIdle >= len(rows) {
		return nil
	}
	t := rows[firstIdle].IntervalStart
	return &t
}
