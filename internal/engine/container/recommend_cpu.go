package container

import "github.com/redhatinsights/ros-ocp-backend/internal/engine/core"

// RecommendCPU computes both cost and performance CPU recommendations
// from a set of daily digest rows. Single-path algorithm (no 1-core
// discontinuity). Applies decay weighting, adaptive margin, floor, and
// idle detection.
func RecommendCPU(rows []core.DigestRow, cfg core.CPUConfig) core.CPURec {
	if len(rows) == 0 {
		return core.CPURec{}
	}

	costPctVal, perfPctVal, avgP95, avgP50, avgMean, trendSlope, isIdle := multiCPUWeightedPercentiles(rows, cfg)

	marginScaled := core.ComputeAdaptiveMarginScaled(avgP95, avgP50, avgMean, cfg.MinMargin, cfg.MaxMargin)

	costRequest := applyFloor(core.ApplyScaledMargin(costPctVal, marginScaled), cfg.FloorMC)
	perfRequest := applyFloor(core.ApplyScaledMargin(perfPctVal, marginScaled), cfg.FloorMC)

	limitMultScaled := core.ScaleLimitMultiplier(cfg.LimitMultiplier)
	costLimit := core.ApplyScaledMargin(costRequest, limitMultScaled)
	perfLimit := core.ApplyScaledMargin(perfRequest, limitMultScaled)

	return core.CPURec{
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

func multiCPUWeightedPercentiles(rows []core.DigestRow, cfg core.CPUConfig) (costPctVal, perfPctVal, avgP95, avgP50, avgMean int64, trendSlope float64, isIdle bool) {
	vals, extras := core.MultiWeightedPercentileWithExtras(rows, cfg.Now, cfg.DecayHalfLifeHours,
		&core.WindowExtraOpts{
			TrendMetric:      func(r core.DigestRow) int64 { return r.CPUUsageP98MC },
			IdleThresholdMC:  cfg.IdleThresholdMC,
			IdleThresholdMem: cfg.IdleThresholdMemKiB,
			DetectIdle:       true,
		},
		func(r core.DigestRow) int64 { return core.SelectCPUUsagePercentile(r, cfg.CostPercentile) },
		func(r core.DigestRow) int64 { return core.SelectCPUUsagePercentile(r, cfg.PerfPercentile) },
		func(r core.DigestRow) int64 { return r.CPUUsageP95MC },
		func(r core.DigestRow) int64 { return r.CPUUsageP50MC },
		func(r core.DigestRow) int64 { return r.CPUUsageMeanMC },
	)
	if len(vals) != 5 {
		return
	}
	return vals[0], vals[1], vals[2], vals[3], vals[4], extras.TrendSlope, extras.IsIdle
}
