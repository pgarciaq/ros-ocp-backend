package container

import (
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// #602 performance contract. The fused path is what production runs, and the
// percentile pass is the hot loop of the whole native engine, so its allocation
// count is pinned as a test rather than left to benchmark archaeology: a
// regression here is invisible in review until someone profiles.
//
// The budget is 2 allocations, both inside the percentile walk and both
// unavoidable given its signature (it returns []int64 and accumulates a
// []float64 sized by the caller's column count). Everything else must stay on the
// stack: the Column extractor lists are uint8 data, the window opts do not
// escape, and Column.Value reads fields in place through a *DigestRow rather
// than copying the 312-byte struct per column.
//
// Before the Column walk this was 6 allocations / 224 B, and 4 of those were
// closures capturing a runtime percentile. See the benchmark for the time axis.

const fusedAllocBudget = 2

func allocTestConfigs() (types.CPUConfig, types.MemoryConfig, []types.DigestRow) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := benchWindow(now)
	cpuCfg := adaptiveCPUConfig(now, 336)
	memCfg := adaptiveMemConfig(now, 336)
	memCfg.OOMCountSum = 3
	return cpuCfg, memCfg, rows
}

func TestRecommendCPUAndMemory_AllocationBudget(t *testing.T) {
	cpuCfg, memCfg, rows := allocTestConfigs()

	got := testing.AllocsPerRun(200, func() {
		_, _, _ = RecommendCPUAndMemory(rows, cpuCfg, memCfg)
	})

	if got > fusedAllocBudget {
		t.Errorf("fused path allocated %.0f times per call, budget is %d: a closure or an "+
			"escaping value has crept back onto the hot path", got, fusedAllocBudget)
	}
}

func TestRecommendCPU_AllocationBudget(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := benchWindow(now)
	cpuCfg := adaptiveCPUConfig(now, 336)

	// Same two allocations as the fused path: the walk's returned []int64 and its
	// float64 accumulator.
	if got := testing.AllocsPerRun(200, func() { _ = RecommendCPU(rows, cpuCfg) }); got > fusedAllocBudget {
		t.Errorf("RecommendCPU allocated %.0f times per call, budget is %d", got, fusedAllocBudget)
	}
}

func TestRecommendMemory_AllocationBudget(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := benchWindow(now)
	memCfg := adaptiveMemConfig(now, 336)

	if got := testing.AllocsPerRun(200, func() { _ = RecommendMemory(rows, memCfg) }); got > fusedAllocBudget {
		t.Errorf("RecommendMemory allocated %.0f times per call, budget is %d", got, fusedAllocBudget)
	}
}

// TestRecommendCPUAndMemory_MismatchedWindow_AllocationBudget covers the
// mismatched-window fallback too: it runs two walks, so it gets its own bound
// rather than silently reusing the single-walk budget.
func TestRecommendCPUAndMemory_MismatchedWindow_AllocationBudget(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := benchWindow(now)
	cpuCfg := adaptiveCPUConfig(now, 336)
	memCfg := adaptiveMemConfig(now.Add(time.Hour), 240)

	// Two walks => two returned slices plus two accumulators. This path has no
	// production caller today (production configs always share a window), so the
	// bound only needs to catch a reintroduced closure.
	const fallbackBudget = 2 * fusedAllocBudget
	if got := testing.AllocsPerRun(200, func() {
		_, _, _ = RecommendCPUAndMemory(rows, cpuCfg, memCfg)
	}); got > fallbackBudget {
		t.Errorf("mismatched-window path allocated %.0f times per call, budget is %d", got, fallbackBudget)
	}
}
