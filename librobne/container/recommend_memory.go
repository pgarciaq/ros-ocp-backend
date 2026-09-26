package container

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// RecommendMemory computes both cost and performance memory recommendations
// from a set of daily digest rows.
// Cost model uses the configured percentile (default p95), performance model
// uses max. Both apply adaptive margin. Limit = request * limitMultiplier.
func RecommendMemory(rows []types.DigestRow, cfg types.MemoryConfig) types.MemoryRec {
	if len(rows) == 0 {
		return types.MemoryRec{}
	}

	rec, _ := finalizeMem(memPercentiles(rows, cfg), cfg)
	return rec
}

// memPercentiles walks the window once and returns the memory weighted
// percentiles plus the trend slope for the same pass.
func memPercentiles(rows []types.DigestRow, cfg types.MemoryConfig) memPercentileOut {
	// Stack array for the selector list: see recommend_cpu.go.
	var selectorBuf [memSelectorCount]selector
	vals, extras := types.MultiWeightedPercentileColumns(rows, cfg.Now, cfg.DecayHalfLifeHours,
		memWindowExtras(), memSelectors(selectorBuf[:0], cfg)...)
	if len(vals) != memSelectorCount {
		return memPercentileOut{}
	}
	return memPercentileOut{
		costPct:    vals[0],
		perfPct:    vals[1],
		avgP95:     vals[2],
		avgP50:     vals[3],
		avgMean:    vals[4],
		trendSlope: extras.MemTrendSlope,
	}
}
