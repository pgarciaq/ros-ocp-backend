package gpu

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
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

// loadGPUIdleConfigDefault is the fallback used when the function variable
// LoadGPUIdleConfig has not been wired by the engine package (e.g. in unit tests).
func loadGPUIdleConfigDefault(_ context.Context, _ *pgxpool.Pool, _ string) GPUIdleConfig {
	return GPUIdleConfig{
		Enabled:            true,
		IdleSMActiveBP:     500,
		IdleDRAMActiveBP:   500,
		ZombieSMActiveBP:   GPUZombieThresholdBP,
		ZombieDRAMActiveBP: GPUZombieThresholdBP,
		MinObservationDays: 7,
	}
}

func init() {
	if LoadGPUIdleConfig == nil {
		LoadGPUIdleConfig = loadGPUIdleConfigDefault
	}
}

// ClassifyGPUIdleState determines if a GPU is zombie, idle, or active from the
// max daily sm_active and dram_active basis points over the observation window.
func ClassifyGPUIdleState(
	smActiveP95BP int64,
	dramActiveP95BP int64,
	observationDays int,
	cfg GPUIdleConfig,
) core.IdleResult {
	result := core.IdleResult{State: core.IdleStateActive}

	if !cfg.Enabled {
		return result
	}
	if observationDays < cfg.MinObservationDays {
		return result
	}

	if smActiveP95BP < cfg.ZombieSMActiveBP && dramActiveP95BP < cfg.ZombieDRAMActiveBP {
		result.State = core.IdleStateZombie
		return result
	}

	if smActiveP95BP < cfg.IdleSMActiveBP && dramActiveP95BP < cfg.IdleDRAMActiveBP {
		result.State = core.IdleStateIdle
		return result
	}

	return result
}

// ClassifyGPUIdleFromDigests classifies GPU idle state from daily digest rows and
// sets idle_since / duration when non-active.
func ClassifyGPUIdleFromDigests(digests []GPUDigestRow, cfg GPUIdleConfig) core.IdleResult {
	if len(digests) == 0 {
		return core.IdleResult{State: core.IdleStateActive}
	}
	smP95 := maxDailyGPUField(digests, func(d GPUDigestRow) int64 { return int64(d.SMActiveAvg) })
	dramP95 := maxDailyGPUField(digests, func(d GPUDigestRow) int64 { return int64(d.DRAMActiveAvg) })
	result := ClassifyGPUIdleState(smP95, dramP95, len(digests), cfg)
	if result.State == core.IdleStateActive {
		return result
	}
	result.IdleSince = findGPUIdleSince(digests, func(d GPUDigestRow) bool {
		sm := int64(d.SMActiveAvg)
		dram := int64(d.DRAMActiveAvg)
		if result.State == core.IdleStateZombie {
			return sm < cfg.ZombieSMActiveBP && dram < cfg.ZombieDRAMActiveBP
		}
		return sm < cfg.IdleSMActiveBP && dram < cfg.IdleDRAMActiveBP
	})
	result.DurationDays = core.ComputeIdleDuration(result.IdleSince)
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

// ApplyGPUIdleWasteCents sets EstimatedWasteCents on the idle result when the GPU
// is idle or zombie and a monthly GPU rate is available (full gpu_cost_per_month).
func ApplyGPUIdleWasteCents(result *core.IdleResult, gpuMonthlyRateUSD float64) {
	if result == nil || result.State == core.IdleStateActive || gpuMonthlyRateUSD <= 0 {
		return
	}
	result.WasteCents = money.USDToCents(gpuMonthlyRateUSD)
}
