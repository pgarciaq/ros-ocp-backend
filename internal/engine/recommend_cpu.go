package engine

import "math"

const defaultIdleThresholdMC int64 = 10

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

	margin := ComputeAdaptiveMargin(avgP95, avgP50, avgMean, cfg.MinMargin, cfg.MaxMargin)

	costRequest := applyFloor(int64(math.Round(float64(costPctVal)*margin)), cfg.FloorMC)
	perfRequest := applyFloor(int64(math.Round(float64(perfPctVal)*margin)), cfg.FloorMC)

	costLimit := int64(math.Round(float64(costRequest) * cfg.LimitMultiplier))
	perfLimit := int64(math.Round(float64(perfRequest) * cfg.LimitMultiplier))

	trendSlope := ComputeTrendSlope(rows, func(r DigestRow) int64 { return r.CPUUsageP98MC })
	isIdle := DetectIdle(rows, defaultIdleThresholdMC)

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
