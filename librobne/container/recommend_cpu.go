package container

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// RecommendCPU computes both cost and performance CPU recommendations
// from a set of daily digest rows. Single-path algorithm (no 1-core
// discontinuity). Applies decay weighting, adaptive margin, floor, and
// idle detection.
func RecommendCPU(rows []types.DigestRow, cfg types.CPUConfig) types.CPURec {
	if len(rows) == 0 {
		return types.CPURec{}
	}

	rec, _ := finalizeCPU(cpuPercentiles(rows, cfg), cfg)
	return rec
}

// cpuPercentiles walks the window once and returns the CPU weighted percentiles
// plus the trend slope and idle flag for the same pass.
func cpuPercentiles(rows []types.DigestRow, cfg types.CPUConfig) cpuPercentileOut {
	// Stack array for the selector list: the walk does not retain it, so keeping
	// it off the heap matters on the recommendation hot path (#602).
	var selectorBuf [cpuSelectorCount]selector
	vals, extras := types.MultiWeightedPercentileColumns(rows, cfg.Now, cfg.DecayHalfLifeHours,
		cpuWindowExtras(cfg), cpuSelectors(selectorBuf[:0], cfg)...)
	if len(vals) != cpuSelectorCount {
		return cpuPercentileOut{}
	}
	return cpuPercentileOut{
		costPct:    vals[0],
		perfPct:    vals[1],
		avgP95:     vals[2],
		avgP50:     vals[3],
		avgMean:    vals[4],
		trendSlope: extras.TrendSlope,
		isIdle:     extras.IsIdle,
	}
}
