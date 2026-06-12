package engine

// RecommendCPUAndMemory computes CPU and memory recommendations from the same
// digest rows in a single weighted-percentile pass, avoiding duplicate row
// iteration and decay weight lookups.
func RecommendCPUAndMemory(rows []DigestRow, cpuCfg CPUConfig, memCfg MemoryConfig) (CPURec, MemoryRec) {
	if len(rows) == 0 {
		return CPURec{}, MemoryRec{}
	}

	costCPU, perfCPU, avgCPUP95, avgCPUP50, avgCPUMean,
		costMem, perfMem, avgMemP95, avgMemP50, avgMemMean,
		cpuTrend, memTrend, isIdle := multiCPUAndMemoryWeightedPercentiles(rows, cpuCfg, memCfg)

	cpuMarginScaled := ComputeAdaptiveMarginScaled(avgCPUP95, avgCPUP50, avgCPUMean, cpuCfg.MinMargin, cpuCfg.MaxMargin)
	memMarginScaled := ComputeAdaptiveMarginScaled(avgMemP95, avgMemP50, avgMemMean, memCfg.MinMargin, memCfg.MaxMargin)

	costCPUReq := applyFloor(ApplyScaledMargin(costCPU, cpuMarginScaled), cpuCfg.FloorMC)
	perfCPUReq := applyFloor(ApplyScaledMargin(perfCPU, cpuMarginScaled), cpuCfg.FloorMC)

	limitMultScaled := ScaleLimitMultiplier(cpuCfg.LimitMultiplier)
	costCPULim := ApplyScaledMargin(costCPUReq, limitMultScaled)
	perfCPULim := ApplyScaledMargin(perfCPUReq, limitMultScaled)

	costMemReq := ApplyScaledMargin(costMem, memMarginScaled)
	perfMemReq := ApplyScaledMargin(perfMem, memMarginScaled)

	if memCfg.OOMCountSum > 0 {
		costMemReq = ApplyOOMBumpScaled(costMemReq, memCfg.OOMCountSum, memCfg.OOMBaseBump, memCfg.OOMMaxBump)
		perfMemReq = ApplyOOMBumpScaled(perfMemReq, memCfg.OOMCountSum, memCfg.OOMBaseBump, memCfg.OOMMaxBump)
	}

	memLimitMultScaled := ScaleLimitMultiplier(memCfg.LimitMultiplier)
	costMemLim := ApplyScaledMargin(costMemReq, memLimitMultScaled)
	perfMemLim := ApplyScaledMargin(perfMemReq, memLimitMultScaled)

	return CPURec{
			CostRequestMC: costCPUReq,
			CostLimitMC:   costCPULim,
			PerfRequestMC: perfCPUReq,
			PerfLimitMC:   perfCPULim,
			TrendSlope:    cpuTrend,
			IsIdle:        isIdle,
		}, MemoryRec{
			CostRequestKiB: costMemReq,
			CostLimitKiB:   costMemLim,
			PerfRequestKiB: perfMemReq,
			PerfLimitKiB:   perfMemLim,
			TrendSlope:     memTrend,
		}
}

func multiCPUAndMemoryWeightedPercentiles(
	rows []DigestRow,
	cpuCfg CPUConfig,
	memCfg MemoryConfig,
) (
	costCPU, perfCPU, avgCPUP95, avgCPUP50, avgCPUMean int64,
	costMem, perfMem, avgMemP95, avgMemP50, avgMemMean int64,
	cpuTrend, memTrend float64,
	isIdle bool,
) {
	vals, extras := MultiWeightedPercentileWithExtras(rows, cpuCfg.Now, cpuCfg.DecayHalfLifeHours,
		&WindowExtraOpts{
			TrendMetric:      func(r DigestRow) int64 { return r.CPUUsageP98MC },
			MemTrendMetric:   func(r DigestRow) int64 { return r.MemUsageP95KiB },
			IdleThresholdMC:  cpuCfg.IdleThresholdMC,
			IdleThresholdMem: cpuCfg.IdleThresholdMemKiB,
			DetectIdle:       true,
		},
		func(r DigestRow) int64 { return SelectCPUUsagePercentile(r, cpuCfg.CostPercentile) },
		func(r DigestRow) int64 { return SelectCPUUsagePercentile(r, cpuCfg.PerfPercentile) },
		func(r DigestRow) int64 { return r.CPUUsageP95MC },
		func(r DigestRow) int64 { return r.CPUUsageP50MC },
		func(r DigestRow) int64 { return r.CPUUsageMeanMC },
		func(r DigestRow) int64 { return SelectMemUsagePercentile(r, memCfg.CostPercentile) },
		func(r DigestRow) int64 { return SelectMemUsagePercentile(r, memCfg.PerfPercentile) },
		func(r DigestRow) int64 { return r.MemUsageP95KiB },
		func(r DigestRow) int64 { return r.MemUsageP50KiB },
		func(r DigestRow) int64 { return r.MemUsageMeanKiB },
	)
	if len(vals) != 10 {
		return
	}
	return vals[0], vals[1], vals[2], vals[3], vals[4],
		vals[5], vals[6], vals[7], vals[8], vals[9],
		extras.TrendSlope, extras.MemTrendSlope, extras.IsIdle
}
