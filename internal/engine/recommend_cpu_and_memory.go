package engine

// RecommendCPUAndMemory computes CPU and memory recommendations from the same
// digest rows in a single weighted-percentile pass, avoiding duplicate row
// iteration and decay weight lookups. It also returns explanation factors
// computed during the same pass for persistence as expl_* columns.
func RecommendCPUAndMemory(rows []DigestRow, cpuCfg CPUConfig, memCfg MemoryConfig) (CPURec, MemoryRec, ContainerExplanationFactors) {
	if len(rows) == 0 {
		return CPURec{}, MemoryRec{}, ContainerExplanationFactors{}
	}

	costCPU, perfCPU, avgCPUP95, avgCPUP50, avgCPUMean,
		costMem, perfMem, avgMemP95, avgMemP50, avgMemMean,
		cpuTrend, memTrend, isIdle := multiCPUAndMemoryWeightedPercentiles(rows, cpuCfg, memCfg)

	cpuMarginScaled := ComputeAdaptiveMarginScaled(avgCPUP95, avgCPUP50, avgCPUMean, cpuCfg.MinMargin, cpuCfg.MaxMargin)
	memMarginScaled := ComputeAdaptiveMarginScaled(avgMemP95, avgMemP50, avgMemMean, memCfg.MinMargin, memCfg.MaxMargin)

	costCPUReqBeforeFloor := ApplyScaledMargin(costCPU, cpuMarginScaled)
	costCPUReq := applyFloor(costCPUReqBeforeFloor, cpuCfg.FloorMC)
	cpuFloorApplied := costCPUReq > costCPUReqBeforeFloor
	perfCPUReq := applyFloor(ApplyScaledMargin(perfCPU, cpuMarginScaled), cpuCfg.FloorMC)

	limitMultScaled := ScaleLimitMultiplier(cpuCfg.LimitMultiplier)
	costCPULim := ApplyScaledMargin(costCPUReq, limitMultScaled)
	perfCPULim := ApplyScaledMargin(perfCPUReq, limitMultScaled)

	costMemReqBeforeBump := ApplyScaledMargin(costMem, memMarginScaled)
	perfMemReqBeforeBump := ApplyScaledMargin(perfMem, memMarginScaled)
	costMemReq := costMemReqBeforeBump
	perfMemReq := perfMemReqBeforeBump
	oomBumpApplied := false
	if memCfg.OOMCountSum > 0 {
		costMemReq = ApplyOOMBumpScaled(costMemReq, memCfg.OOMCountSum, memCfg.OOMBaseBump, memCfg.OOMMaxBump)
		perfMemReq = ApplyOOMBumpScaled(perfMemReq, memCfg.OOMCountSum, memCfg.OOMBaseBump, memCfg.OOMMaxBump)
		oomBumpApplied = costMemReq != costMemReqBeforeBump || perfMemReq != perfMemReqBeforeBump
	}

	memLimitMultScaled := ScaleLimitMultiplier(memCfg.LimitMultiplier)
	costMemLim := ApplyScaledMargin(costMemReq, memLimitMultScaled)
	perfMemLim := ApplyScaledMargin(perfMemReq, memLimitMultScaled)

	expl := ContainerExplanationFactors{
		DecayHalfLifeHours:  cpuCfg.DecayHalfLifeHours,
		CPUCostPctMC:        costCPU,
		CPUPerfPctMC:        perfCPU,
		CPUUsageP95MC:       avgCPUP95,
		CPUUsageP50MC:       avgCPUP50,
		CPUUsageMeanMC:      avgCPUMean,
		CPUAdaptiveMarginBP: int32(cpuMarginScaled),
		CPUTrendSlope:       cpuTrend,
		MemCostPctKiB:       costMem,
		MemPerfPctKiB:       perfMem,
		MemUsageP95KiB:      avgMemP95,
		MemUsageP50KiB:      avgMemP50,
		MemUsageMeanKiB:     avgMemMean,
		MemAdaptiveMarginBP: int32(memMarginScaled),
		MemTrendSlope:       memTrend,
		OOMCountSum:         memCfg.OOMCountSum,
		OOMBumpApplied:      oomBumpApplied,
		CPUFloorApplied:     cpuFloorApplied,
		IsIdle:              isIdle,
	}

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
		}, expl
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
