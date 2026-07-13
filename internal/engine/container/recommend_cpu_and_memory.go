package container

import "github.com/redhatinsights/ros-ocp-backend/internal/engine/core"

// RecommendCPUAndMemory computes CPU and memory recommendations from the same
// digest rows in a single weighted-percentile pass, avoiding duplicate row
// iteration and decay weight lookups. It also returns explanation factors
// computed during the same pass for persistence as expl_* columns.
func RecommendCPUAndMemory(rows []core.DigestRow, cpuCfg core.CPUConfig, memCfg core.MemoryConfig) (core.CPURec, core.MemoryRec, core.ContainerExplanationFactors) {
	if len(rows) == 0 {
		return core.CPURec{}, core.MemoryRec{}, core.ContainerExplanationFactors{}
	}

	costCPU, perfCPU, avgCPUP95, avgCPUP50, avgCPUMean,
		costMem, perfMem, avgMemP95, avgMemP50, avgMemMean,
		cpuTrend, memTrend, isIdle := multiCPUAndMemoryWeightedPercentiles(rows, cpuCfg, memCfg)

	cpuMarginScaled := core.ComputeAdaptiveMarginScaled(avgCPUP95, avgCPUP50, avgCPUMean, cpuCfg.MinMargin, cpuCfg.MaxMargin)
	memMarginScaled := core.ComputeAdaptiveMarginScaled(avgMemP95, avgMemP50, avgMemMean, memCfg.MinMargin, memCfg.MaxMargin)

	costCPUReqBeforeFloor := core.ApplyScaledMargin(costCPU, cpuMarginScaled)
	costCPUReq := applyFloor(costCPUReqBeforeFloor, cpuCfg.FloorMC)
	cpuFloorApplied := costCPUReq > costCPUReqBeforeFloor
	perfCPUReq := applyFloor(core.ApplyScaledMargin(perfCPU, cpuMarginScaled), cpuCfg.FloorMC)

	limitMultScaled := core.ScaleLimitMultiplier(cpuCfg.LimitMultiplier)
	costCPULim := core.ApplyScaledMargin(costCPUReq, limitMultScaled)
	perfCPULim := core.ApplyScaledMargin(perfCPUReq, limitMultScaled)

	costMemReqBeforeBump := core.ApplyScaledMargin(costMem, memMarginScaled)
	perfMemReqBeforeBump := core.ApplyScaledMargin(perfMem, memMarginScaled)
	costMemReq := costMemReqBeforeBump
	perfMemReq := perfMemReqBeforeBump
	oomBumpApplied := false
	if memCfg.OOMCountSum > 0 {
		costMemReq = core.ApplyOOMBumpScaled(costMemReq, memCfg.OOMCountSum, memCfg.OOMBaseBump, memCfg.OOMMaxBump)
		perfMemReq = core.ApplyOOMBumpScaled(perfMemReq, memCfg.OOMCountSum, memCfg.OOMBaseBump, memCfg.OOMMaxBump)
		oomBumpApplied = costMemReq != costMemReqBeforeBump || perfMemReq != perfMemReqBeforeBump
	}

	costMemReqBeforeFloor := costMemReq
	costMemReq = applyFloor(costMemReq, memCfg.FloorKiB)
	memFloorApplied := costMemReq > costMemReqBeforeFloor
	perfMemReq = applyFloor(perfMemReq, memCfg.FloorKiB)

	memLimitMultScaled := core.ScaleLimitMultiplier(memCfg.LimitMultiplier)
	costMemLim := core.ApplyScaledMargin(costMemReq, memLimitMultScaled)
	perfMemLim := core.ApplyScaledMargin(perfMemReq, memLimitMultScaled)

	expl := core.ContainerExplanationFactors{
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
		MemFloorApplied:     memFloorApplied,
		IsIdle:              isIdle,
	}

	return core.CPURec{
			CostRequestMC: costCPUReq,
			CostLimitMC:   costCPULim,
			PerfRequestMC: perfCPUReq,
			PerfLimitMC:   perfCPULim,
			TrendSlope:    cpuTrend,
			IsIdle:        isIdle,
		}, core.MemoryRec{
			CostRequestKiB: costMemReq,
			CostLimitKiB:   costMemLim,
			PerfRequestKiB: perfMemReq,
			PerfLimitKiB:   perfMemLim,
			TrendSlope:     memTrend,
		}, expl
}

func multiCPUAndMemoryWeightedPercentiles(
	rows []core.DigestRow,
	cpuCfg core.CPUConfig,
	memCfg core.MemoryConfig,
) (
	costCPU, perfCPU, avgCPUP95, avgCPUP50, avgCPUMean int64,
	costMem, perfMem, avgMemP95, avgMemP50, avgMemMean int64,
	cpuTrend, memTrend float64,
	isIdle bool,
) {
	vals, extras := core.MultiWeightedPercentileWithExtras(rows, cpuCfg.Now, cpuCfg.DecayHalfLifeHours,
		&core.WindowExtraOpts{
			TrendMetric:      func(r core.DigestRow) int64 { return r.CPUUsageP98MC },
			MemTrendMetric:   func(r core.DigestRow) int64 { return r.MemUsageP95KiB },
			IdleThresholdMC:  cpuCfg.IdleThresholdMC,
			IdleThresholdMem: cpuCfg.IdleThresholdMemKiB,
			DetectIdle:       true,
		},
		func(r core.DigestRow) int64 { return core.SelectCPUUsagePercentile(r, cpuCfg.CostPercentile) },
		func(r core.DigestRow) int64 { return core.SelectCPUUsagePercentile(r, cpuCfg.PerfPercentile) },
		func(r core.DigestRow) int64 { return r.CPUUsageP95MC },
		func(r core.DigestRow) int64 { return r.CPUUsageP50MC },
		func(r core.DigestRow) int64 { return r.CPUUsageMeanMC },
		func(r core.DigestRow) int64 { return core.SelectMemUsagePercentile(r, memCfg.CostPercentile) },
		func(r core.DigestRow) int64 { return core.SelectMemUsagePercentile(r, memCfg.PerfPercentile) },
		func(r core.DigestRow) int64 { return r.MemUsageP95KiB },
		func(r core.DigestRow) int64 { return r.MemUsageP50KiB },
		func(r core.DigestRow) int64 { return r.MemUsageMeanKiB },
	)
	if len(vals) != 10 {
		return
	}
	return vals[0], vals[1], vals[2], vals[3], vals[4],
		vals[5], vals[6], vals[7], vals[8], vals[9],
		extras.TrendSlope, extras.MemTrendSlope, extras.IsIdle
}
