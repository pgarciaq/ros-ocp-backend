package engine

// RecommendCPU computes both cost and performance CPU recommendations
// from a set of daily digest rows. Single-path algorithm (no 1-core
// discontinuity). Applies decay weighting, adaptive margin, floor, and
// idle detection.
func RecommendCPU(rows []DigestRow, cfg CPUConfig) CPURec {
	if len(rows) == 0 {
		return CPURec{}
	}

	costPctVal := WeightedPercentile(rows, cfg.Now, cfg.DecayHalfLifeHours,
		func(r DigestRow) int64 { return SelectCPUUsagePercentile(r, cfg.CostPercentile) })

	perfPctVal := WeightedPercentile(rows, cfg.Now, cfg.DecayHalfLifeHours,
		func(r DigestRow) int64 { return SelectCPUUsagePercentile(r, cfg.PerfPercentile) })

	avgP95 := WeightedPercentile(rows, cfg.Now, cfg.DecayHalfLifeHours,
		func(r DigestRow) int64 { return r.CPUUsageP95MC })
	avgP50 := WeightedPercentile(rows, cfg.Now, cfg.DecayHalfLifeHours,
		func(r DigestRow) int64 { return r.CPUUsageP50MC })
	avgMean := WeightedPercentile(rows, cfg.Now, cfg.DecayHalfLifeHours,
		func(r DigestRow) int64 { return r.CPUUsageMeanMC })

	marginScaled := ComputeAdaptiveMarginScaled(avgP95, avgP50, avgMean, cfg.MinMargin, cfg.MaxMargin)

	costRequest := applyFloor(ApplyScaledMargin(costPctVal, marginScaled), cfg.FloorMC)
	perfRequest := applyFloor(ApplyScaledMargin(perfPctVal, marginScaled), cfg.FloorMC)

	limitMultScaled := ScaleLimitMultiplier(cfg.LimitMultiplier)
	costLimit := ApplyScaledMargin(costRequest, limitMultScaled)
	perfLimit := ApplyScaledMargin(perfRequest, limitMultScaled)

	trendSlope := ComputeTrendSlope(rows, func(r DigestRow) int64 { return r.CPUUsageP98MC })
	isIdle := DetectIdle(rows, cfg.IdleThresholdMC, cfg.IdleThresholdMemKiB)

	return CPURec{
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
