package container

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// RecommendCPU computes both cost and performance CPU recommendations
// from a set of daily digest rows. Single-path algorithm (no 1-core
// discontinuity). Applies decay weighting, adaptive margin, floor, and
// idle detection.
func RecommendCPU(rows []types.DigestRow, cfg types.CPUConfig) types.CPURec {
	if len(rows) == 0 {
		return types.CPURec{}
	}

	costPctVal, perfPctVal, avgP95, avgP50, avgMean, trendSlope, isIdle := multiCPUWeightedPercentiles(rows, cfg)

	marginScaled := types.ComputeAdaptiveMarginScaled(avgP95, avgP50, avgMean, cfg.MinMargin, cfg.MaxMargin)

	costRequest := applyFloor(types.ApplyScaledMargin(costPctVal, marginScaled), cfg.FloorMC)
	perfRequest := applyFloor(types.ApplyScaledMargin(perfPctVal, marginScaled), cfg.FloorMC)

	limitMultScaled := types.ScaleLimitMultiplier(cfg.LimitMultiplier)
	costLimit := types.ApplyScaledMargin(costRequest, limitMultScaled)
	perfLimit := types.ApplyScaledMargin(perfRequest, limitMultScaled)

	return types.CPURec{
		CostRequestMC: costRequest,
		CostLimitMC:   costLimit,
		PerfRequestMC: perfRequest,
		PerfLimitMC:   perfLimit,
		TrendSlope:    trendSlope,
		IsIdle:        isIdle,
	}
}

func applyFloor(val, floor int64) int64 {
	if val < floor {
		return floor
	}
	return val
}

func multiCPUWeightedPercentiles(rows []types.DigestRow, cfg types.CPUConfig) (costPctVal, perfPctVal, avgP95, avgP50, avgMean int64, trendSlope float64, isIdle bool) {
	vals, extras := types.MultiWeightedPercentileWithExtras(rows, cfg.Now, cfg.DecayHalfLifeHours,
		&types.WindowExtraOpts{
			TrendMetric:      func(r types.DigestRow) int64 { return r.CPUUsageP98MC },
			IdleThresholdMC:  cfg.IdleThresholdMC,
			IdleThresholdMem: cfg.IdleThresholdMemKiB,
			DetectIdle:       true,
		},
		func(r types.DigestRow) int64 { return types.SelectCPUUsagePercentile(r, cfg.CostPercentile) },
		func(r types.DigestRow) int64 { return types.SelectCPUUsagePercentile(r, cfg.PerfPercentile) },
		func(r types.DigestRow) int64 { return r.CPUUsageP95MC },
		func(r types.DigestRow) int64 { return r.CPUUsageP50MC },
		func(r types.DigestRow) int64 { return r.CPUUsageMeanMC },
	)
	if len(vals) != 5 {
		return
	}
	return vals[0], vals[1], vals[2], vals[3], vals[4], extras.TrendSlope, extras.IsIdle
}
