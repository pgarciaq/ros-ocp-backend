package engine

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

const gpuZombieThresholdBP = 100 // 1% — admin-only zombie cutoff

// GPUIdleConfig holds GPU-specific idle thresholds.
type GPUIdleConfig struct {
	Enabled            bool
	IdleSMActiveBP     int64 // sm_active P95 below this = idle (default 500 = 5%)
	IdleDRAMActiveBP   int64 // dram_active P95 below this = idle (default 500 = 5%)
	ZombieSMActiveBP   int64 // sm_active P95 below this = zombie (default 100 = 1%)
	ZombieDRAMActiveBP int64 // dram_active P95 below this = zombie (default 100 = 1%)
	MinObservationDays int   // default 7
}

// LoadGPUIdleConfig reads GPU idle detection settings from env (via config).
func LoadGPUIdleConfig() GPUIdleConfig {
	cfg := config.GetConfig()
	out := GPUIdleConfig{
		Enabled:            true,
		IdleSMActiveBP:     500,
		IdleDRAMActiveBP:   500,
		ZombieSMActiveBP:   gpuZombieThresholdBP,
		ZombieDRAMActiveBP: gpuZombieThresholdBP,
		MinObservationDays: 7,
	}
	if cfg == nil {
		return out
	}
	out.Enabled = cfg.IdleDetectionEnabled
	if cfg.IdleGPUSMActiveBP > 0 {
		out.IdleSMActiveBP = cfg.IdleGPUSMActiveBP
	}
	if cfg.IdleGPUDRAMActiveBP > 0 {
		out.IdleDRAMActiveBP = cfg.IdleGPUDRAMActiveBP
	}
	return out
}

// ClassifyGPUIdleState determines if a GPU is zombie, idle, or active from P95
// sm_active and dram_active basis points over the observation window.
func ClassifyGPUIdleState(
	smActiveP95BP int64,
	dramActiveP95BP int64,
	observationDays int,
	cfg GPUIdleConfig,
) IdleResult {
	result := IdleResult{State: IdleStateActive}

	if !cfg.Enabled {
		return result
	}
	if observationDays < cfg.MinObservationDays {
		return result
	}

	if smActiveP95BP < cfg.ZombieSMActiveBP && dramActiveP95BP < cfg.ZombieDRAMActiveBP {
		result.State = IdleStateZombie
		return result
	}

	if smActiveP95BP < cfg.IdleSMActiveBP && dramActiveP95BP < cfg.IdleDRAMActiveBP {
		result.State = IdleStateIdle
		return result
	}

	return result
}

// ClassifyGPUIdleFromDigests classifies GPU idle state from daily digest rows and
// sets idle_since / duration when non-active.
func ClassifyGPUIdleFromDigests(digests []GPUDigestRow, cfg GPUIdleConfig) IdleResult {
	if len(digests) == 0 {
		return IdleResult{State: IdleStateActive}
	}
	smP95 := percentile95GPUField(digests, func(d GPUDigestRow) int64 { return int64(d.SMActiveAvg) })
	dramP95 := percentile95GPUField(digests, func(d GPUDigestRow) int64 { return int64(d.DRAMActiveAvg) })
	result := ClassifyGPUIdleState(smP95, dramP95, len(digests), cfg)
	if result.State == IdleStateActive {
		return result
	}
	result.IdleSince = findGPUIdleSince(digests, func(d GPUDigestRow) bool {
		sm := int64(d.SMActiveAvg)
		dram := int64(d.DRAMActiveAvg)
		if result.State == IdleStateZombie {
			return sm < cfg.ZombieSMActiveBP && dram < cfg.ZombieDRAMActiveBP
		}
		return sm < cfg.IdleSMActiveBP && dram < cfg.IdleDRAMActiveBP
	})
	result.DurationDays = computeIdleDuration(result.IdleSince)
	return result
}

func percentile95GPUField(rows []GPUDigestRow, pick func(GPUDigestRow) int64) int64 {
	vals := make([]int64, len(rows))
	for i, r := range rows {
		vals[i] = pick(r)
	}
	return percentile95Int64(vals)
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

// ApplyGPUIdleWasteCents sets EstimatedWasteCents on the idle result when the GPU
// is idle or zombie and a monthly GPU rate is available (full gpu_cost_per_month).
func ApplyGPUIdleWasteCents(result *IdleResult, gpuMonthlyRateUSD float64) {
	if result == nil || result.State == IdleStateActive || gpuMonthlyRateUSD <= 0 {
		return
	}
	result.WasteCents = money.USDToCents(gpuMonthlyRateUSD)
}
