package container

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// Parity between the solo and fused entry points (#602).
//
// RecommendCPUAndMemory is what production runs; RecommendCPU / RecommendMemory
// are what unit tests exercise. Every fixture below therefore asserts the full
// recommendation struct from both paths, so a divergence in any field (requests,
// limits, trend slope, idle flag) fails.
//
// Fixtures are built so no incidental property can make them pass:
//   - each row carries a distinct value in every column the selectors read, so a
//     changed decay weight moves the weighted average (no same-value collisions);
//   - the CPU and memory series have different shapes, so a mis-wired selector or
//     trend metric is visible rather than coincidentally equal;
//   - each drift test asserts that its own fixture is *sensitive* to the knob
//     under test (a control recommendation computed with the other config), so a
//     weakened fixture cannot silently green the test.

const (
	parityCPUFloorMC   = 10
	parityMemFloorKiB  = 4096
	parityLimitMult    = 1.2
	parityNoIdleMC     = 1 << 20
	parityNoIdleMemKiB = 1 << 30
)

// adaptiveCPUConfig has a live adaptive margin (min 1.15 / max 1.5) so the
// margin step of the finalize pipeline is exercised rather than clamped to 1.0.
func adaptiveCPUConfig(now time.Time, hl float64) types.CPUConfig {
	return types.CPUConfig{
		CostPercentile:      0.95,
		PerfPercentile:      0.95,
		MinMargin:           1.15,
		MaxMargin:           1.5,
		LimitMultiplier:     parityLimitMult,
		FloorMC:             parityCPUFloorMC,
		IdleThresholdMC:     parityNoIdleMC,
		IdleThresholdMemKiB: parityNoIdleMemKiB,
		DecayHalfLifeHours:  hl,
		Now:                 now,
	}
}

// adaptiveMemConfig mirrors adaptiveCPUConfig for memory.
func adaptiveMemConfig(now time.Time, hl float64) types.MemoryConfig {
	return types.MemoryConfig{
		CostPercentile:     0.95,
		PerfPercentile:     0.95,
		MinMargin:          1.15,
		MaxMargin:          1.5,
		LimitMultiplier:    parityLimitMult,
		FloorKiB:           parityMemFloorKiB,
		DecayHalfLifeHours: hl,
		Now:                now,
		OOMBaseBump:        2.0,
		OOMMaxBump:         3.0,
	}
}

// exactCPUConfig pins the margin to 1.0 so a fixture value flows through the
// pipeline unchanged and boundary cases can assert exact integers.
func exactCPUConfig(now time.Time) types.CPUConfig {
	cfg := adaptiveCPUConfig(now, 24)
	cfg.MinMargin, cfg.MaxMargin = 1.0, 1.0
	return cfg
}

// exactMemConfig pins the margin to 1.0 (see exactCPUConfig).
func exactMemConfig(now time.Time) types.MemoryConfig {
	cfg := adaptiveMemConfig(now, 24)
	cfg.MinMargin, cfg.MaxMargin = 1.0, 1.0
	return cfg
}

// parityRows is a four-day window with distinct values in every CPU and memory
// column. CPU rises linearly (constant slope); memory rises super-linearly
// (growing gaps), so CPU and memory trend slopes differ.
func parityRows(now time.Time) []types.DigestRow {
	rows := make([]types.DigestRow, 4)
	for i := range rows {
		n := int64(i + 1)
		age := time.Duration(i-3) * 24 * time.Hour
		rows[i] = types.DigestRow{
			BucketDate:     now.Add(age),
			CPUUsageP50MC:  40 * n,
			CPUUsageP60MC:  100 * n,
			CPUUsageP95MC:  150 * n,
			CPUUsageP98MC:  180 * n,
			CPUUsageMaxMC:  200 * n,
			MemUsageP50KiB: 200 * n,
			MemUsageP95KiB: 1000 * n,
			MemUsageP98KiB: 1500 * n,
			MemUsageMaxKiB: 2000 * n,
			// Means are populated so the adaptive margin is genuinely computed
			// (a zero mean short-circuits ComputeAdaptiveMarginScaled to MinMargin).
			CPUUsageMeanMC:  120 * n,
			MemUsageMeanKiB: 700 * n,
		}
	}
	return rows
}

// singleMemRow is a one-day window (decay weight 1.0 at the config's Now), so the
// selected percentile equals the fixture value and boundary math is exact.
func singleMemRow(now time.Time, memP95, cpuP95 int64) []types.DigestRow {
	return []types.DigestRow{{
		BucketDate:       now,
		CPUUsageP50MC:    cpuP95 / 2,
		CPUUsageP60MC:    cpuP95,
		CPUUsageP95MC:    cpuP95,
		CPUUsageP98MC:    cpuP95,
		CPUUsageMaxMC:    cpuP95,
		MemUsageP50KiB:   memP95 / 2,
		MemUsageP95KiB:   memP95,
		MemUsageP98KiB:   memP95,
		MemUsageMaxKiB:   memP95,
		MemRequestP50KiB: memP95 / 2,
		CPUUsageMeanMC:   cpuP95,
		MemUsageMeanKiB:  memP95,
	}}
}

// TestRecommendCPUAndMemory_MemWindow_UsesMemConfigNow is the drift vector from
// #602: the fused percentile walk used cpuCfg.Now / cpuCfg.DecayHalfLifeHours
// for the memory columns, so a memory config with a later clock silently lost
// the decay weighting of its newest (future-dated) rows.
func TestRecommendCPUAndMemory_MemWindow_UsesMemConfigNow(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := parityRows(now)

	cpuCfg := adaptiveCPUConfig(now, 24)
	memCfg := adaptiveMemConfig(now.Add(12*time.Hour), 24) // later clock, same half-life

	// Control: the same memory config evaluated against the CPU clock must give a
	// different answer, otherwise this fixture cannot detect the drift.
	memAtCPUWindow := RecommendMemory(rows, adaptiveMemConfig(now, 24))
	memAtMemWindow := RecommendMemory(rows, memCfg)
	require.NotEqual(t, memAtCPUWindow, memAtMemWindow,
		"fixture must be sensitive to the decay window, else the parity assert below is vacuous")

	fusedCPU, fusedMem, _ := RecommendCPUAndMemory(rows, cpuCfg, memCfg)

	// The fused memory recommendation must honour memCfg.Now: pre-fix it reused
	// cpuCfg.Now, so the newest row was decayed instead of clamped to full weight.
	assert.Equal(t, memAtMemWindow, fusedMem,
		"fused memory rec must use memCfg.Now for weighting, not cpuCfg.Now")
	// Control: the CPU side is unaffected by the memory window.
	assert.Equal(t, RecommendCPU(rows, cpuCfg), fusedCPU,
		"fused CPU rec must be unchanged by a differing memCfg.Now")
}

// TestRecommendCPUAndMemory_MemWindow_UsesMemConfigHalfLife covers the second half
// of the drift vector: differing decay half-life between the two configs.
func TestRecommendCPUAndMemory_MemWindow_UsesMemConfigHalfLife(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := parityRows(now)

	cpuCfg := adaptiveCPUConfig(now, 24)
	memCfg := adaptiveMemConfig(now, 240) // same clock, much flatter decay

	memAtShortHalfLife := RecommendMemory(rows, adaptiveMemConfig(now, 24))
	memAtLongHalfLife := RecommendMemory(rows, memCfg)
	require.NotEqual(t, memAtShortHalfLife, memAtLongHalfLife,
		"fixture must be sensitive to the decay half-life, else the parity assert below is vacuous")

	fusedCPU, fusedMem, _ := RecommendCPUAndMemory(rows, cpuCfg, memCfg)

	// Pre-fix the memory columns were weighted with cpuCfg's 24h half-life, which
	// over-weighted the newest day relative to a 240h half-life.
	assert.Equal(t, memAtLongHalfLife, fusedMem,
		"fused memory rec must use memCfg.DecayHalfLifeHours, not cpuCfg.DecayHalfLifeHours")
	assert.Equal(t, RecommendCPU(rows, cpuCfg), fusedCPU,
		"fused CPU rec must be unchanged by a differing memCfg.DecayHalfLifeHours")
}

// TestRecommendCPUAndMemory_SharedWindowParity walks the shared-window case
// (what every production caller builds) plus an idle and an OOM configuration,
// asserting full-struct parity in each.
func TestRecommendCPUAndMemory_SharedWindowParity(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := parityRows(now)

	tests := []struct {
		name   string
		mutate func(*types.CPUConfig, *types.MemoryConfig)
	}{
		{
			name:   "default",
			mutate: func(*types.CPUConfig, *types.MemoryConfig) {},
		},
		{
			name: "oom kills",
			mutate: func(_ *types.CPUConfig, m *types.MemoryConfig) {
				m.OOMCountSum = 3
			},
		},
		{
			name: "idle container",
			mutate: func(c *types.CPUConfig, _ *types.MemoryConfig) {
				// Threshold below the oldest row's max (800) => IsIdle flips.
				c.IdleThresholdMC = 100
			},
		},
		{
			name: "floors bite",
			mutate: func(c *types.CPUConfig, m *types.MemoryConfig) {
				c.FloorMC = 10_000
				m.FloorKiB = 100_000
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpuCfg := adaptiveCPUConfig(now, 72)
			memCfg := adaptiveMemConfig(now, 72)
			tc.mutate(&cpuCfg, &memCfg)

			fusedCPU, fusedMem, _ := RecommendCPUAndMemory(rows, cpuCfg, memCfg)
			assert.Equal(t, RecommendCPU(rows, cpuCfg), fusedCPU)
			assert.Equal(t, RecommendMemory(rows, memCfg), fusedMem)
		})
	}
}

// TestRecommendCPUAndMemory_ExplFactorsMatchRecs pins the fused-only explanation
// flags to what the shared finalize actually computed. Pre-refactor the flags
// were computed inline in the fused body; a shared helper that returned a stale
// or defaulted flag would leave the expl_* columns silently wrong.
func TestRecommendCPUAndMemory_ExplFactorsMatchRecs(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := parityRows(now)

	tests := []struct {
		name               string
		mutate             func(*types.CPUConfig, *types.MemoryConfig)
		wantCPUFloor       bool
		wantMemFloor       bool
		wantOOMBumpApplied bool
	}{
		{
			name:         "no floors no oom",
			mutate:       func(*types.CPUConfig, *types.MemoryConfig) {},
			wantCPUFloor: false, // usage (~200 MC) is far above the 10 MC floor
			wantMemFloor: false, // usage (~3267 KiB) is below the 4096 KiB floor
		},
		{
			name: "both floors and oom",
			mutate: func(c *types.CPUConfig, m *types.MemoryConfig) {
				c.FloorMC = 10_000
				m.FloorKiB = 100_000
				m.OOMCountSum = 3
			},
			wantCPUFloor:       true,
			wantMemFloor:       true,
			wantOOMBumpApplied: true,
		},
		{
			name: "oom present but zero base bump changes nothing",
			mutate: func(_ *types.CPUConfig, m *types.MemoryConfig) {
				m.FloorKiB = 100_000
				m.OOMCountSum = 1
				m.OOMBaseBump = 0.0 // log2(2)*0 == 0 => multiplier 1.0 => value unchanged
			},
			wantCPUFloor:       false,
			wantMemFloor:       true,
			wantOOMBumpApplied: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cpuCfg := adaptiveCPUConfig(now, 72)
			memCfg := adaptiveMemConfig(now, 72)
			tc.mutate(&cpuCfg, &memCfg)

			cpuRec, memRec, expl := RecommendCPUAndMemory(rows, cpuCfg, memCfg)

			// Each flag must describe the value the recommendation actually carries.
			assert.Equal(t, tc.wantCPUFloor, expl.CPUFloorApplied,
				"CPUFloorApplied must report whether the floor raised the cost request")
			assert.Equal(t, tc.wantMemFloor, expl.MemFloorApplied,
				"MemFloorApplied must report whether the floor raised the cost request")
			assert.Equal(t, tc.wantOOMBumpApplied, expl.OOMBumpApplied,
				"OOMBumpApplied must report whether the OOM bump changed the request")

			// When a floor is reported, the request must sit exactly at the floor.
			if expl.CPUFloorApplied {
				assert.Equal(t, cpuCfg.FloorMC, cpuRec.CostRequestMC)
			}
			if expl.MemFloorApplied {
				assert.Equal(t, memCfg.FloorKiB, memRec.CostRequestKiB)
			}

			// The adaptive margin must be live, not clamped to a no-op 1.0.
			assert.Greater(t, expl.CPUAdaptiveMarginBP, int32(types.MarginScale),
				"fixture must exercise the adaptive margin above 1.0")
			assert.Greater(t, expl.MemAdaptiveMarginBP, int32(types.MarginScale),
				"fixture must exercise the adaptive margin above 1.0")
		})
	}
}

// TestRecommendMemory_OOMBumpAppliedBeforeFloor locks the pipeline order. Both
// orderings are plausible code; only one is correct (bump the observed usage,
// then enforce the floor). With a bump that crosses the floor the two differ
// (6000 vs 12288 KiB), so a swapped order cannot pass.
func TestRecommendMemory_OOMBumpAppliedBeforeFloor(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := singleMemRow(now, 2000, 200)

	cpuCfg := exactCPUConfig(now)
	memCfg := exactMemConfig(now)
	memCfg.OOMCountSum = 1
	memCfg.OOMBaseBump = 2.0 // log2(1+1) == 1 => x3
	memCfg.OOMMaxBump = 3.0

	_, fusedMem, expl := RecommendCPUAndMemory(rows, cpuCfg, memCfg)
	soloMem := RecommendMemory(rows, memCfg)

	// 2000 usage, no margin, x3 OOM bump, then the 4096 floor: 6000, not 4096*3.
	assert.Equal(t, int64(6000), fusedMem.CostRequestKiB,
		"OOM bump must be applied to the usage value before the floor is enforced")
	assert.Equal(t, int64(6000), fusedMem.PerfRequestKiB,
		"OOM bump must be applied to the perf usage value before the floor is enforced")
	assert.Equal(t, int64(7200), fusedMem.CostLimitKiB, "limit = request * 1.2")
	assert.True(t, expl.OOMBumpApplied, "bump changed the request, so the flag must be set")
	assert.Equal(t, soloMem, fusedMem, "solo and fused must agree on the OOM-then-floor order")
}

// TestRecommend_FloorBoundaries exercises the floor boundary from both sides:
// one below (floor applies, flag set), exactly at (value unchanged, flag clear),
// one above (floor does not apply).
func TestRecommend_FloorBoundaries(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

	memCases := []struct {
		name      string
		memP95    int64
		wantReq   int64
		wantFloor bool
	}{
		{name: "zero usage", memP95: 0, wantReq: parityMemFloorKiB, wantFloor: true},
		{name: "one below floor", memP95: parityMemFloorKiB - 1, wantReq: parityMemFloorKiB, wantFloor: true},
		{name: "exactly at floor", memP95: parityMemFloorKiB, wantReq: parityMemFloorKiB, wantFloor: false},
		{name: "one above floor", memP95: parityMemFloorKiB + 1, wantReq: parityMemFloorKiB + 1, wantFloor: false},
	}

	for _, tc := range memCases {
		t.Run("mem/"+tc.name, func(t *testing.T) {
			rows := singleMemRow(now, tc.memP95, 200)
			cpuCfg := exactCPUConfig(now)
			memCfg := exactMemConfig(now)

			cpuRec, memRec, expl := RecommendCPUAndMemory(rows, cpuCfg, memCfg)

			assert.Equal(t, tc.wantReq, memRec.CostRequestKiB)
			assert.Equal(t, tc.wantReq, memRec.PerfRequestKiB)
			assert.Equal(t, tc.wantFloor, expl.MemFloorApplied,
				"floor flag must be clear when the value is already at or above the floor")
			assert.Equal(t, RecommendMemory(rows, memCfg), memRec)
			assert.Equal(t, RecommendCPU(rows, cpuCfg), cpuRec)
		})
	}

	cpuCases := []struct {
		name      string
		cpuP95    int64
		wantReq   int64
		wantFloor bool
	}{
		{name: "zero usage", cpuP95: 0, wantReq: parityCPUFloorMC, wantFloor: true},
		{name: "one below floor", cpuP95: parityCPUFloorMC - 1, wantReq: parityCPUFloorMC, wantFloor: true},
		{name: "exactly at floor", cpuP95: parityCPUFloorMC, wantReq: parityCPUFloorMC, wantFloor: false},
		{name: "one above floor", cpuP95: parityCPUFloorMC + 1, wantReq: parityCPUFloorMC + 1, wantFloor: false},
	}

	for _, tc := range cpuCases {
		t.Run("cpu/"+tc.name, func(t *testing.T) {
			rows := singleMemRow(now, 2000, tc.cpuP95)
			cpuCfg := exactCPUConfig(now)
			memCfg := exactMemConfig(now)

			cpuRec, _, expl := RecommendCPUAndMemory(rows, cpuCfg, memCfg)

			assert.Equal(t, tc.wantReq, cpuRec.CostRequestMC)
			assert.Equal(t, tc.wantReq, cpuRec.PerfRequestMC)
			assert.Equal(t, tc.wantFloor, expl.CPUFloorApplied,
				"floor flag must be clear when the value is already at or above the floor")
			assert.Equal(t, RecommendCPU(rows, cpuCfg), cpuRec)
		})
	}
}

// TestRecommendCPUAndMemory_TrendWiring pins which series each trend slope comes
// from. The solo memory path and the fused path must agree, and memory's slope
// must not be the CPU slope (the CPU fixture is linear, memory quadratic).
func TestRecommendCPUAndMemory_TrendWiring(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := parityRows(now)

	cpuCfg := adaptiveCPUConfig(now, 72)
	memCfg := adaptiveMemConfig(now, 72)

	fusedCPU, fusedMem, expl := RecommendCPUAndMemory(rows, cpuCfg, memCfg)
	soloCPU := RecommendCPU(rows, cpuCfg)
	soloMem := RecommendMemory(rows, memCfg)

	require.NotEqual(t, soloCPU.TrendSlope, soloMem.TrendSlope,
		"fixture must give CPU and memory different slopes, else a mis-wired trend metric is invisible")

	assert.Equal(t, soloMem.TrendSlope, fusedMem.TrendSlope,
		"fused memory trend slope must come from the memory series")
	assert.Equal(t, soloCPU.TrendSlope, fusedCPU.TrendSlope,
		"fused CPU trend slope must come from the CPU series")
	assert.Equal(t, fusedMem.TrendSlope, expl.MemTrendSlope,
		"expl.MemTrendSlope must match the memory recommendation's slope")
	assert.Equal(t, fusedCPU.TrendSlope, expl.CPUTrendSlope,
		"expl.CPUTrendSlope must match the CPU recommendation's slope")
}

// TestRecommendCPUAndMemory_EmptyRows keeps the no-rows contract on all three
// entry points after the shared helpers were introduced.
func TestRecommendCPUAndMemory_EmptyRows(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	cpuCfg := adaptiveCPUConfig(now, 72)
	memCfg := adaptiveMemConfig(now, 72)

	cpuRec, memRec, expl := RecommendCPUAndMemory(nil, cpuCfg, memCfg)
	assert.Equal(t, types.CPURec{}, cpuRec)
	assert.Equal(t, types.MemoryRec{}, memRec)
	assert.Equal(t, types.ContainerExplanationFactors{}, expl)
	assert.Equal(t, types.CPURec{}, RecommendCPU(nil, cpuCfg))
	assert.Equal(t, types.MemoryRec{}, RecommendMemory(nil, memCfg))
}

// benchWindow builds a 30-day digest window resembling a production term.
func benchWindow(now time.Time) []types.DigestRow {
	const days = 30
	rows := make([]types.DigestRow, days)
	for i := range rows {
		n := int64(i + 1)
		rows[i] = types.DigestRow{
			BucketDate:      now.Add(time.Duration(i-days+1) * 24 * time.Hour),
			CPUUsageP50MC:   40 * n,
			CPUUsageP60MC:   100 * n,
			CPUUsageP95MC:   150 * n,
			CPUUsageP98MC:   180 * n,
			CPUUsageMaxMC:   200 * n,
			MemUsageP50KiB:  200 * n,
			MemUsageP95KiB:  1000 * n,
			MemUsageP98KiB:  1500 * n,
			MemUsageMaxKiB:  2000 * n,
			CPUUsageMeanMC:  120 * n,
			MemUsageMeanKiB: 700 * n,
		}
	}
	return rows
}

// BenchmarkRecommendCPUAndMemory measures the production path: one fused pass
// over the window with a shared decay window. #602 requires this to stay
// single-pass, so this benchmark is the regression signal for that obligation.
func BenchmarkRecommendCPUAndMemory(b *testing.B) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := benchWindow(now)
	cpuCfg := adaptiveCPUConfig(now, 336)
	memCfg := adaptiveMemConfig(now, 336)
	memCfg.OOMCountSum = 3

	var cpuRec types.CPURec
	var memRec types.MemoryRec
	var expl types.ContainerExplanationFactors

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cpuRec, memRec, expl = RecommendCPUAndMemory(rows, cpuCfg, memCfg)
	}
	_, _, _ = cpuRec, memRec, expl
}

// BenchmarkRecommendCPUAndMemory_SeparateCalls is the two-walk cost the fused
// path must stay below.
func BenchmarkRecommendCPUAndMemory_SeparateCalls(b *testing.B) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := benchWindow(now)
	cpuCfg := adaptiveCPUConfig(now, 336)
	memCfg := adaptiveMemConfig(now, 336)
	memCfg.OOMCountSum = 3

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RecommendCPU(rows, cpuCfg)
		_ = RecommendMemory(rows, memCfg)
	}
}

// BenchmarkRecommendCPUAndMemory_MismatchedWindow measures the two-walk fallback
// taken when the CPU and memory configs disagree on clock or half-life.
func BenchmarkRecommendCPUAndMemory_MismatchedWindow(b *testing.B) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := benchWindow(now)
	cpuCfg := adaptiveCPUConfig(now, 336)
	memCfg := adaptiveMemConfig(now, 240)

	var cpuRec types.CPURec
	var memRec types.MemoryRec
	var expl types.ContainerExplanationFactors

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cpuRec, memRec, expl = RecommendCPUAndMemory(rows, cpuCfg, memCfg)
	}
	_, _, _ = cpuRec, memRec, expl
}
