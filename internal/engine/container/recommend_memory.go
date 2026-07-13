package container

import "github.com/redhatinsights/ros-ocp-backend/internal/engine/core"

// RecommendMemory computes both cost and performance memory recommendations
// from a set of daily digest rows.
// Cost model uses the configured percentile (default p95), performance model
// uses max. Both apply adaptive margin. Limit = request * limitMultiplier.
func RecommendMemory(rows []core.DigestRow, cfg core.MemoryConfig) core.MemoryRec {
	if len(rows) == 0 {
		return core.MemoryRec{}
	}

	costPctVal, perfPctVal, avgP95, avgP50, avgMean, trendSlope := multiMemWeightedPercentiles(rows, cfg)

	marginScaled := core.ComputeAdaptiveMarginScaled(avgP95, avgP50, avgMean, cfg.MinMargin, cfg.MaxMargin)

	costRequest := core.ApplyScaledMargin(costPctVal, marginScaled)
	perfRequest := core.ApplyScaledMargin(perfPctVal, marginScaled)

	if cfg.OOMCountSum > 0 {
		costRequest = core.ApplyOOMBumpScaled(costRequest, cfg.OOMCountSum, cfg.OOMBaseBump, cfg.OOMMaxBump)
		perfRequest = core.ApplyOOMBumpScaled(perfRequest, cfg.OOMCountSum, cfg.OOMBaseBump, cfg.OOMMaxBump)
	}

	costRequest = applyFloor(costRequest, cfg.FloorKiB)
	perfRequest = applyFloor(perfRequest, cfg.FloorKiB)

	limitMultScaled := core.ScaleLimitMultiplier(cfg.LimitMultiplier)
	costLimit := core.ApplyScaledMargin(costRequest, limitMultScaled)
	perfLimit := core.ApplyScaledMargin(perfRequest, limitMultScaled)

	return core.MemoryRec{
		CostRequestKiB: costRequest,
		CostLimitKiB:   costLimit,
		PerfRequestKiB: perfRequest,
		PerfLimitKiB:   perfLimit,
		TrendSlope:     trendSlope,
	}
}

func multiMemWeightedPercentiles(rows []core.DigestRow, cfg core.MemoryConfig) (costPctVal, perfPctVal, avgP95, avgP50, avgMean int64, trendSlope float64) {
	vals, extras := core.MultiWeightedPercentileWithExtras(rows, cfg.Now, cfg.DecayHalfLifeHours,
		&core.WindowExtraOpts{
			TrendMetric: func(r core.DigestRow) int64 { return r.MemUsageP95KiB },
		},
		func(r core.DigestRow) int64 { return core.SelectMemUsagePercentile(r, cfg.CostPercentile) },
		func(r core.DigestRow) int64 { return core.SelectMemUsagePercentile(r, cfg.PerfPercentile) },
		func(r core.DigestRow) int64 { return r.MemUsageP95KiB },
		func(r core.DigestRow) int64 { return r.MemUsageP50KiB },
		func(r core.DigestRow) int64 { return r.MemUsageMeanKiB },
	)
	if len(vals) != 5 {
		return
	}
	return vals[0], vals[1], vals[2], vals[3], vals[4], extras.TrendSlope
}
