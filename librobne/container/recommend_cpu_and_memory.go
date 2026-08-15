package container

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// RecommendCPUAndMemory computes CPU and memory recommendations from the same
// digest rows in a single weighted-percentile pass, avoiding duplicate row
// iteration and decay weight lookups. It also returns explanation factors
// computed during the same pass for persistence as expl_* columns.
func RecommendCPUAndMemory(rows []types.DigestRow, cpuCfg types.CPUConfig, memCfg types.MemoryConfig) (types.CPURec, types.MemoryRec, types.ContainerExplanationFactors) {
	if len(rows) == 0 {
		return types.CPURec{}, types.MemoryRec{}, types.ContainerExplanationFactors{}
	}

	costCPU, perfCPU, avgCPUP95, avgCPUP50, avgCPUMean,
		costMem, perfMem, avgMemP95, avgMemP50, avgMemMean,
		cpuTrend, memTrend, isIdle := multiCPUAndMemoryWeightedPercentiles(rows, cpuCfg, memCfg)

	cpuMarginScaled := types.ComputeAdaptiveMarginScaled(avgCPUP95, avgCPUP50, avgCPUMean, cpuCfg.MinMargin, cpuCfg.MaxMargin)
	memMarginScaled := types.ComputeAdaptiveMarginScaled(avgMemP95, avgMemP50, avgMemMean, memCfg.MinMargin, memCfg.MaxMargin)

	costCPUReqBeforeFloor := types.ApplyScaledMargin(costCPU, cpuMarginScaled)
	costCPUReq := applyFloor(costCPUReqBeforeFloor, cpuCfg.FloorMC)
	cpuFloorApplied := costCPUReq > costCPUReqBeforeFloor
	perfCPUReq := applyFloor(types.ApplyScaledMargin(perfCPU, cpuMarginScaled), cpuCfg.FloorMC)

	limitMultScaled := types.ScaleLimitMultiplier(cpuCfg.LimitMultiplier)
	costCPULim := types.ApplyScaledMargin(costCPUReq, limitMultScaled)
	perfCPULim := types.ApplyScaledMargin(perfCPUReq, limitMultScaled)

	costMemReqBeforeBump := types.ApplyScaledMargin(costMem, memMarginScaled)
	perfMemReqBeforeBump := types.ApplyScaledMargin(perfMem, memMarginScaled)
	costMemReq := costMemReqBeforeBump
	perfMemReq := perfMemReqBeforeBump
	oomBumpApplied := false
	if memCfg.OOMCountSum > 0 {
		costMemReq = types.ApplyOOMBumpScaled(costMemReq, memCfg.OOMCountSum, memCfg.OOMBaseBump, memCfg.OOMMaxBump)
		perfMemReq = types.ApplyOOMBumpScaled(perfMemReq, memCfg.OOMCountSum, memCfg.OOMBaseBump, memCfg.OOMMaxBump)
		oomBumpApplied = costMemReq != costMemReqBeforeBump || perfMemReq != perfMemReqBeforeBump
	}

	costMemReqBeforeFloor := costMemReq
	costMemReq = applyFloor(costMemReq, memCfg.FloorKiB)
	memFloorApplied := costMemReq > costMemReqBeforeFloor
	perfMemReq = applyFloor(perfMemReq, memCfg.FloorKiB)

	memLimitMultScaled := types.ScaleLimitMultiplier(memCfg.LimitMultiplier)
	costMemLim := types.ApplyScaledMargin(costMemReq, memLimitMultScaled)
	perfMemLim := types.ApplyScaledMargin(perfMemReq, memLimitMultScaled)

	expl := types.ContainerExplanationFactors{
		DecayHalfLifeHours:  cpuCfg.DecayHalfLifeHours,
		CPUCostPctMC:        costCPU,
		CPUPerfPctMC:        perfCPU,
		CPUUsageP95MC:       avgCPUP95,
		CPUUsageP50MC:       avgCPUP50,
		CPUUsageMeanMC:      avgCPUMean,
		CPUAdaptiveMarginBP: int32(cpuMarginScaled), //nolint:gosec // margin BP is clamped well below int32
		CPUTrendSlope:       cpuTrend,
		MemCostPctKiB:       costMem,
		MemPerfPctKiB:       perfMem,
		MemUsageP95KiB:      avgMemP95,
		MemUsageP50KiB:      avgMemP50,
		MemUsageMeanKiB:     avgMemMean,
		MemAdaptiveMarginBP: int32(memMarginScaled), //nolint:gosec // margin BP is clamped well below int32
		MemTrendSlope:       memTrend,
		OOMCountSum:         memCfg.OOMCountSum,
		OOMBumpApplied:      oomBumpApplied,
		CPUFloorApplied:     cpuFloorApplied,
		MemFloorApplied:     memFloorApplied,
		IsIdle:              isIdle,
	}

	return types.CPURec{
			CostRequestMC: costCPUReq,
			CostLimitMC:   costCPULim,
			PerfRequestMC: perfCPUReq,
			PerfLimitMC:   perfCPULim,
			TrendSlope:    cpuTrend,
			IsIdle:        isIdle,
		}, types.MemoryRec{
			CostRequestKiB: costMemReq,
			CostLimitKiB:   costMemLim,
			PerfRequestKiB: perfMemReq,
			PerfLimitKiB:   perfMemLim,
			TrendSlope:     memTrend,
		}, expl
}

func multiCPUAndMemoryWeightedPercentiles(
	rows []types.DigestRow,
	cpuCfg types.CPUConfig,
	memCfg types.MemoryConfig,
) (
	costCPU, perfCPU, avgCPUP95, avgCPUP50, avgCPUMean int64,
	costMem, perfMem, avgMemP95, avgMemP50, avgMemMean int64,
	cpuTrend, memTrend float64,
	isIdle bool,
) {
	vals, extras := types.MultiWeightedPercentileWithExtras(rows, cpuCfg.Now, cpuCfg.DecayHalfLifeHours,
		&types.WindowExtraOpts{
			TrendMetric:      func(r types.DigestRow) int64 { return r.CPUUsageP98MC },
			MemTrendMetric:   func(r types.DigestRow) int64 { return r.MemUsageP95KiB },
			IdleThresholdMC:  cpuCfg.IdleThresholdMC,
			IdleThresholdMem: cpuCfg.IdleThresholdMemKiB,
			DetectIdle:       true,
		},
		func(r types.DigestRow) int64 { return types.SelectCPUUsagePercentile(r, cpuCfg.CostPercentile) },
		func(r types.DigestRow) int64 { return types.SelectCPUUsagePercentile(r, cpuCfg.PerfPercentile) },
		func(r types.DigestRow) int64 { return r.CPUUsageP95MC },
		func(r types.DigestRow) int64 { return r.CPUUsageP50MC },
		func(r types.DigestRow) int64 { return r.CPUUsageMeanMC },
		func(r types.DigestRow) int64 { return types.SelectMemUsagePercentile(r, memCfg.CostPercentile) },
		func(r types.DigestRow) int64 { return types.SelectMemUsagePercentile(r, memCfg.PerfPercentile) },
		func(r types.DigestRow) int64 { return r.MemUsageP95KiB },
		func(r types.DigestRow) int64 { return r.MemUsageP50KiB },
		func(r types.DigestRow) int64 { return r.MemUsageMeanKiB },
	)
	if len(vals) != 10 {
		return
	}
	return vals[0], vals[1], vals[2], vals[3], vals[4],
		vals[5], vals[6], vals[7], vals[8], vals[9],
		extras.TrendSlope, extras.MemTrendSlope, extras.IsIdle
}
