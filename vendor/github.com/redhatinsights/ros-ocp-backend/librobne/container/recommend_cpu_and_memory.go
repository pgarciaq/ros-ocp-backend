package container

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// RecommendCPUAndMemory computes CPU and memory recommendations from the same
// digest rows in a single weighted-percentile pass when both configs describe
// the same decay window, avoiding duplicate row iteration and decay weight
// lookups. Production callers build both configs from one term's timestamp and
// half-life. Lower-level callers may provide different windows; each resource is
// then computed with its own config. The returned explanation has one
// DecayHalfLifeHours field and records cpuCfg's value in that case. Explanation
// factors are computed here for persistence as expl_* columns.
func RecommendCPUAndMemory(rows []types.DigestRow, cpuCfg types.CPUConfig, memCfg types.MemoryConfig) (types.CPURec, types.MemoryRec, types.ContainerExplanationFactors) {
	if len(rows) == 0 {
		return types.CPURec{}, types.MemoryRec{}, types.ContainerExplanationFactors{}
	}

	cpuPct, memPct := cpuAndMemPercentiles(rows, cpuCfg, memCfg)

	cpuRec, cpuMeta := finalizeCPU(cpuPct, cpuCfg)
	memRec, memMeta := finalizeMem(memPct, memCfg)

	expl := types.ContainerExplanationFactors{
		DecayHalfLifeHours:  cpuCfg.DecayHalfLifeHours,
		CPUCostPctMC:        cpuPct.costPct,
		CPUPerfPctMC:        cpuPct.perfPct,
		CPUUsageP95MC:       cpuPct.avgP95,
		CPUUsageP50MC:       cpuPct.avgP50,
		CPUUsageMeanMC:      cpuPct.avgMean,
		CPUAdaptiveMarginBP: int32(cpuMeta.marginScaled), //nolint:gosec // margin BP is clamped well below int32
		CPUTrendSlope:       cpuPct.trendSlope,
		MemCostPctKiB:       memPct.costPct,
		MemPerfPctKiB:       memPct.perfPct,
		MemUsageP95KiB:      memPct.avgP95,
		MemUsageP50KiB:      memPct.avgP50,
		MemUsageMeanKiB:     memPct.avgMean,
		MemAdaptiveMarginBP: int32(memMeta.marginScaled), //nolint:gosec // margin BP is clamped well below int32
		MemTrendSlope:       memPct.trendSlope,
		OOMCountSum:         memCfg.OOMCountSum,
		OOMBumpApplied:      memMeta.oomBumpApplied,
		CPUFloorApplied:     cpuMeta.floorApplied,
		MemFloorApplied:     memMeta.floorApplied,
		IsIdle:              cpuPct.isIdle,
	}

	return cpuRec, memRec, expl
}

// cpuAndMemPercentiles computes both resources' weighted percentiles. When the
// two configs describe the same decay window — every production caller builds
// them from one `now` and one term half-life — the rows are walked once and the
// per-row decay weight is shared. When they disagree, each side is weighted by
// its own config; sharing one walk there would silently weight the memory
// columns by the CPU config's clock and half-life (#602).
func cpuAndMemPercentiles(rows []types.DigestRow, cpuCfg types.CPUConfig, memCfg types.MemoryConfig) (cpuPercentileOut, memPercentileOut) {
	if !sameDecayWindow(cpuCfg, memCfg) {
		return cpuPercentiles(rows, cpuCfg), memPercentiles(rows, memCfg)
	}

	var selectorBuf [cpuSelectorCount + memSelectorCount]selector
	selectors := memSelectors(cpuSelectors(selectorBuf[:0], cpuCfg), memCfg)
	vals, extras := types.MultiWeightedPercentileColumns(rows, cpuCfg.Now, cpuCfg.DecayHalfLifeHours,
		fusedWindowExtras(cpuCfg), selectors...)
	if len(vals) != cpuSelectorCount+memSelectorCount {
		return cpuPercentileOut{}, memPercentileOut{}
	}

	return cpuPercentileOut{
			costPct:    vals[0],
			perfPct:    vals[1],
			avgP95:     vals[2],
			avgP50:     vals[3],
			avgMean:    vals[4],
			trendSlope: extras.TrendSlope,
			isIdle:     extras.IsIdle,
		}, memPercentileOut{
			costPct:    vals[cpuSelectorCount],
			perfPct:    vals[cpuSelectorCount+1],
			avgP95:     vals[cpuSelectorCount+2],
			avgP50:     vals[cpuSelectorCount+3],
			avgMean:    vals[cpuSelectorCount+4],
			trendSlope: extras.MemTrendSlope,
		}
}
