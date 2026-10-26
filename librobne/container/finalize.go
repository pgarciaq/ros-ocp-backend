package container

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// Shared post-percentile pipeline (#602).
//
// RecommendCPU, RecommendMemory and RecommendCPUAndMemory all run the same
// sequence over their weighted percentiles — adaptive margin, then (memory)
// OOM bump, then floor, then limit multiplier. Only the explanation flags
// differ in availability: the solo entry points have nowhere to report them,
// the fused one persists them as expl_* columns. Computing them in the shared
// helper keeps the flags and the recommendation from drifting apart.

// cpuPercentileOut is the raw output of one CPU weighted-percentile pass.
type cpuPercentileOut struct {
	costPct    int64
	perfPct    int64
	avgP95     int64
	avgP50     int64
	avgMean    int64
	trendSlope float64
	isIdle     bool
}

// memPercentileOut is the raw output of one memory weighted-percentile pass.
type memPercentileOut struct {
	costPct    int64
	perfPct    int64
	avgP95     int64
	avgP50     int64
	avgMean    int64
	trendSlope float64
}

// cpuFinalizeMeta carries intermediates the fused path persists as expl_*.
// Solo callers discard it.
type cpuFinalizeMeta struct {
	marginScaled int64
	floorApplied bool
}

// memFinalizeMeta carries intermediates the fused path persists as expl_*.
// Solo callers discard it.
type memFinalizeMeta struct {
	marginScaled   int64
	oomBumpApplied bool
	floorApplied   bool
}

// finalizeCPU turns raw CPU weighted percentiles into a recommendation:
// adaptive margin → floor → limit multiplier. Shared by RecommendCPU and
// RecommendCPUAndMemory.
func finalizeCPU(p cpuPercentileOut, cfg types.CPUConfig) (types.CPURec, cpuFinalizeMeta) {
	marginScaled := types.ComputeAdaptiveMarginScaled(p.avgP95, p.avgP50, p.avgMean, cfg.MinMargin, cfg.MaxMargin)

	costRequestBeforeFloor := types.ApplyScaledMargin(p.costPct, marginScaled)
	costRequest := applyFloor(costRequestBeforeFloor, cfg.FloorMC)
	floorApplied := costRequest > costRequestBeforeFloor
	perfRequest := applyFloor(types.ApplyScaledMargin(p.perfPct, marginScaled), cfg.FloorMC)

	limitMultScaled := types.ScaleLimitMultiplier(cfg.LimitMultiplier)
	costLimit := types.ApplyScaledMargin(costRequest, limitMultScaled)
	perfLimit := types.ApplyScaledMargin(perfRequest, limitMultScaled)

	return types.CPURec{
		CostRequestMC: costRequest,
		CostLimitMC:   costLimit,
		PerfRequestMC: perfRequest,
		PerfLimitMC:   perfLimit,
		TrendSlope:    p.trendSlope,
		IsIdle:        p.isIdle,
	}, cpuFinalizeMeta{marginScaled: marginScaled, floorApplied: floorApplied}
}

// finalizeMem turns raw memory weighted percentiles into a recommendation:
// adaptive margin → OOM bump → floor → limit multiplier. The bump applies to
// the margin-scaled value, so the floor is evaluated last and still wins.
func finalizeMem(p memPercentileOut, cfg types.MemoryConfig) (types.MemoryRec, memFinalizeMeta) {
	marginScaled := types.ComputeAdaptiveMarginScaled(p.avgP95, p.avgP50, p.avgMean, cfg.MinMargin, cfg.MaxMargin)

	costRequestBeforeBump := types.ApplyScaledMargin(p.costPct, marginScaled)
	perfRequestBeforeBump := types.ApplyScaledMargin(p.perfPct, marginScaled)
	costRequest := costRequestBeforeBump
	perfRequest := perfRequestBeforeBump
	oomBumpApplied := false
	if cfg.OOMCountSum > 0 {
		costRequest = types.ApplyOOMBumpScaled(costRequest, cfg.OOMCountSum, cfg.OOMBaseBump, cfg.OOMMaxBump)
		perfRequest = types.ApplyOOMBumpScaled(perfRequest, cfg.OOMCountSum, cfg.OOMBaseBump, cfg.OOMMaxBump)
		oomBumpApplied = costRequest != costRequestBeforeBump || perfRequest != perfRequestBeforeBump
	}

	costRequestBeforeFloor := costRequest
	costRequest = applyFloor(costRequest, cfg.FloorKiB)
	floorApplied := costRequest > costRequestBeforeFloor
	perfRequest = applyFloor(perfRequest, cfg.FloorKiB)

	limitMultScaled := types.ScaleLimitMultiplier(cfg.LimitMultiplier)
	costLimit := types.ApplyScaledMargin(costRequest, limitMultScaled)
	perfLimit := types.ApplyScaledMargin(perfRequest, limitMultScaled)

	return types.MemoryRec{
			CostRequestKiB: costRequest,
			CostLimitKiB:   costLimit,
			PerfRequestKiB: perfRequest,
			PerfLimitKiB:   perfLimit,
			TrendSlope:     p.trendSlope,
		}, memFinalizeMeta{
			marginScaled:   marginScaled,
			oomBumpApplied: oomBumpApplied,
			floorApplied:   floorApplied,
		}
}

func applyFloor(val, floor int64) int64 {
	if val < floor {
		return floor
	}
	return val
}
