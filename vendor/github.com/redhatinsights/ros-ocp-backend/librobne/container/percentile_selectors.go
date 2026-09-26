package container

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// Shared selector lists and window extras for the percentile pass (#602).
//
// The solo and fused entry points must select exactly the same columns and
// derive the same trend/idle signals; the only intentional difference is how
// many columns one walk covers. Building the lists here keeps that guarantee in
// one place instead of copy-pasted in each entry point.

// Selector counts per resource. The fused walk passes both lists in one call, so
// the expected output length is their sum.
const (
	cpuSelectorCount = 5
	memSelectorCount = 5
)

// selector names one column of a digest row. It is a descriptor, not a closure:
// the list stays on the stack and each column is read in place from the row the
// walk already holds, instead of heap-allocating a capturing closure per call
// and copying the 312-byte DigestRow per column read (#602).
type selector = types.Column

// cpuSelectors appends the CPU columns a percentile pass aggregates to dst, in
// order: cost percentile, performance percentile, then p95/p50/mean (which feed
// the adaptive margin). The configured percentiles are resolved to concrete
// columns by the shared percentile mapping, so the selector list is pure data.
func cpuSelectors(dst []selector, cfg types.CPUConfig) []selector {
	return append(dst,
		types.CPUPercentileColumn(cfg.CostPercentile),
		types.CPUPercentileColumn(cfg.PerfPercentile),
		types.ColCPUUsageP95MC,
		types.ColCPUUsageP50MC,
		types.ColCPUUsageMeanMC,
	)
}

// memSelectors appends the memory columns a percentile pass aggregates to dst,
// in the same order as cpuSelectors.
func memSelectors(dst []selector, cfg types.MemoryConfig) []selector {
	return append(dst,
		types.MemPercentileColumn(cfg.CostPercentile),
		types.MemPercentileColumn(cfg.PerfPercentile),
		types.ColMemUsageP95KiB,
		types.ColMemUsageP50KiB,
		types.ColMemUsageMeanKiB,
	)
}

// cpuWindowExtras configures trend (on CPU p98) and idle detection for a CPU
// percentile pass.
func cpuWindowExtras(cfg types.CPUConfig) *types.ColumnWindowOpts {
	return &types.ColumnWindowOpts{
		TrendColumn:     types.ColCPUUsageP98MC,
		IdleThresholdMC: cfg.IdleThresholdMC,
		//nolint:revive // field name matches the frozen WindowExtraOpts counterpart
		IdleThresholdMem: cfg.IdleThresholdMemKiB,
		DetectIdle:       true,
	}
}

// memWindowExtras configures trend for a memory percentile pass. The memory
// trend is the regression slope of MemUsageP95KiB, matching what the fused path
// reports as MemTrendSlope.
func memWindowExtras() *types.ColumnWindowOpts {
	return &types.ColumnWindowOpts{
		MemTrendColumn: types.ColMemUsageP95KiB,
	}
}

// fusedWindowExtras configures both trends and idle detection for a single walk
// covering CPU and memory columns. Idle thresholds come from the CPU config
// (MemoryConfig has no idle thresholds).
func fusedWindowExtras(cpuCfg types.CPUConfig) *types.ColumnWindowOpts {
	return &types.ColumnWindowOpts{
		TrendColumn:      types.ColCPUUsageP98MC,
		MemTrendColumn:   types.ColMemUsageP95KiB,
		IdleThresholdMC:  cpuCfg.IdleThresholdMC,
		IdleThresholdMem: cpuCfg.IdleThresholdMemKiB,
		DetectIdle:       true,
	}
}

// sameDecayWindow reports whether both configs describe the same decay window.
// Production callers build CPUConfig and MemoryConfig from the same `now` and
// the same term half-life, so they agree and the fused path stays single-pass.
func sameDecayWindow(cpuCfg types.CPUConfig, memCfg types.MemoryConfig) bool {
	return cpuCfg.DecayHalfLifeHours == memCfg.DecayHalfLifeHours && cpuCfg.Now.Equal(memCfg.Now)
}
