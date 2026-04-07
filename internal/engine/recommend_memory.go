package engine

import "math"

// RecommendMemory computes both cost and performance memory recommendations
// from a set of daily digest rows.
// Cost model uses the configured percentile (default p95), performance model
// uses max. Both apply adaptive margin. Limit = request * limitMultiplier.
func RecommendMemory(rows []DigestRow, cfg MemoryConfig) MemoryRec {
	if len(rows) == 0 {
		return MemoryRec{}
	}

	costPctVal := WeightedPercentile(rows, cfg.Now, cfg.DecayHalfLifeHours,
		func(r DigestRow) int64 { return SelectMemUsagePercentile(r, cfg.CostPercentile) })

	perfPctVal := WeightedPercentile(rows, cfg.Now, cfg.DecayHalfLifeHours,
		func(r DigestRow) int64 { return SelectMemUsagePercentile(r, cfg.PerfPercentile) })

	avgP95 := WeightedPercentile(rows, cfg.Now, cfg.DecayHalfLifeHours,
		func(r DigestRow) int64 { return r.MemUsageP95KiB })
	avgP50 := WeightedPercentile(rows, cfg.Now, cfg.DecayHalfLifeHours,
		func(r DigestRow) int64 { return r.MemUsageP50KiB })
	avgMean := WeightedPercentile(rows, cfg.Now, cfg.DecayHalfLifeHours,
		func(r DigestRow) int64 { return r.MemUsageMeanKiB })

	margin := ComputeAdaptiveMargin(avgP95, avgP50, avgMean, cfg.MinMargin, cfg.MaxMargin)

	costRequest := int64(math.Round(float64(costPctVal) * margin))
	perfRequest := int64(math.Round(float64(perfPctVal) * margin))

	costLimit := int64(math.Round(float64(costRequest) * cfg.LimitMultiplier))
	perfLimit := int64(math.Round(float64(perfRequest) * cfg.LimitMultiplier))

	trendSlope := ComputeTrendSlope(rows, func(r DigestRow) int64 { return r.MemUsageP95KiB })

	return MemoryRec{
		CostRequestKiB: costRequest,
		CostLimitKiB:   costLimit,
		PerfRequestKiB: perfRequest,
		PerfLimitKiB:   perfLimit,
		TrendSlope:     trendSlope,
	}
}
