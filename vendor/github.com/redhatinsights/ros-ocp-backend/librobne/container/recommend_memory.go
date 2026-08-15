package container

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// RecommendMemory computes both cost and performance memory recommendations
// from a set of daily digest rows.
// Cost model uses the configured percentile (default p95), performance model
// uses max. Both apply adaptive margin. Limit = request * limitMultiplier.
func RecommendMemory(rows []types.DigestRow, cfg types.MemoryConfig) types.MemoryRec {
	if len(rows) == 0 {
		return types.MemoryRec{}
	}

	costPctVal, perfPctVal, avgP95, avgP50, avgMean, trendSlope := multiMemWeightedPercentiles(rows, cfg)

	marginScaled := types.ComputeAdaptiveMarginScaled(avgP95, avgP50, avgMean, cfg.MinMargin, cfg.MaxMargin)

	costRequest := types.ApplyScaledMargin(costPctVal, marginScaled)
	perfRequest := types.ApplyScaledMargin(perfPctVal, marginScaled)

	if cfg.OOMCountSum > 0 {
		costRequest = types.ApplyOOMBumpScaled(costRequest, cfg.OOMCountSum, cfg.OOMBaseBump, cfg.OOMMaxBump)
		perfRequest = types.ApplyOOMBumpScaled(perfRequest, cfg.OOMCountSum, cfg.OOMBaseBump, cfg.OOMMaxBump)
	}

	costRequest = applyFloor(costRequest, cfg.FloorKiB)
	perfRequest = applyFloor(perfRequest, cfg.FloorKiB)

	limitMultScaled := types.ScaleLimitMultiplier(cfg.LimitMultiplier)
	costLimit := types.ApplyScaledMargin(costRequest, limitMultScaled)
	perfLimit := types.ApplyScaledMargin(perfRequest, limitMultScaled)

	return types.MemoryRec{
		CostRequestKiB: costRequest,
		CostLimitKiB:   costLimit,
		PerfRequestKiB: perfRequest,
		PerfLimitKiB:   perfLimit,
		TrendSlope:     trendSlope,
	}
}

func multiMemWeightedPercentiles(rows []types.DigestRow, cfg types.MemoryConfig) (costPctVal, perfPctVal, avgP95, avgP50, avgMean int64, trendSlope float64) {
	vals, extras := types.MultiWeightedPercentileWithExtras(rows, cfg.Now, cfg.DecayHalfLifeHours,
		&types.WindowExtraOpts{
			TrendMetric: func(r types.DigestRow) int64 { return r.MemUsageP95KiB },
		},
		func(r types.DigestRow) int64 { return types.SelectMemUsagePercentile(r, cfg.CostPercentile) },
		func(r types.DigestRow) int64 { return types.SelectMemUsagePercentile(r, cfg.PerfPercentile) },
		func(r types.DigestRow) int64 { return r.MemUsageP95KiB },
		func(r types.DigestRow) int64 { return r.MemUsageP50KiB },
		func(r types.DigestRow) int64 { return r.MemUsageMeanKiB },
	)
	if len(vals) != 5 {
		return
	}
	return vals[0], vals[1], vals[2], vals[3], vals[4], extras.TrendSlope
}
