package types

import (
	"math"
	"testing"
	"time"
)

// fullyPopulatedRow has a distinct value in every Column-reachable field, so a
// Column that reads the wrong field cannot coincidentally match.
func fullyPopulatedRow() DigestRow {
	return DigestRow{
		CPUUsageP50MC:   11,
		CPUUsageP60MC:   12,
		CPUUsageP95MC:   13,
		CPUUsageP98MC:   14,
		CPUUsageP99MC:   15,
		CPUUsageMaxMC:   16,
		CPUUsageMeanMC:  17,
		MemUsageP50KiB:  21,
		MemUsageP60KiB:  22,
		MemUsageP95KiB:  23,
		MemUsageP98KiB:  24,
		MemUsageP99KiB:  25,
		MemUsageMaxKiB:  26,
		MemUsageMeanKiB: 27,
	}
}

// TestColumnValue_MapsEveryColumn is exhaustive over the Column constants: each
// one must read its own field, and the table must cover every constant so a new
// column cannot be added without a case here. A missing case in Value returns 0
// for every row, which this catches because no populated value is 0.
func TestColumnValue_MapsEveryColumn(t *testing.T) {
	row := fullyPopulatedRow()

	// Every Column constant with the value it must read. Keys are the constants
	// themselves, so renaming or renumbering a constant fails to compile.
	all := map[Column]int64{
		ColNone:            0,
		ColCPUUsageP50MC:   11,
		ColCPUUsageP60MC:   12,
		ColCPUUsageP95MC:   13,
		ColCPUUsageP98MC:   14,
		ColCPUUsageP99MC:   15,
		ColCPUUsageMaxMC:   16,
		ColCPUUsageMeanMC:  17,
		ColMemUsageP50KiB:  21,
		ColMemUsageP60KiB:  22,
		ColMemUsageP95KiB:  23,
		ColMemUsageP98KiB:  24,
		ColMemUsageP99KiB:  25,
		ColMemUsageMaxKiB:  26,
		ColMemUsageMeanKiB: 27,
	}

	if len(all) != columnCount {
		t.Fatalf("table covers %d columns but columnCount is %d: a Column constant is untested",
			len(all), columnCount)
	}
	for c := ColNone; int(c) < columnCount; c++ {
		if _, ok := all[c]; !ok {
			t.Errorf("Column(%d) %v has no entry in the exhaustive table", int(c), c)
		}
	}

	for c, want := range all {
		if got := c.Value(&row); got != want {
			t.Errorf("Column(%d) %v read %d, want %d (wrong field mapping)", int(c), c, got, want)
		}
	}
}

// TestColumnValue_OutOfRangeIsZero pins the documented degradation: an unknown
// Column reads 0 rather than panicking, so a bug degrades a recommendation to
// the configured floor instead of crashing a processor batch.
func TestColumnValue_OutOfRangeIsZero(t *testing.T) {
	row := fullyPopulatedRow()
	for _, c := range []Column{Column(columnCount), Column(columnCount + 1), 255} {
		if got := c.Value(&row); got != 0 {
			t.Errorf("out-of-range Column(%d) read %d, want 0", int(c), got)
		}
	}
}

// TestColumnValue_DoesNotMutateRow guards the pointer-receiver contract: reading
// a column must not write through the pointer into the caller's row.
func TestColumnValue_DoesNotMutateRow(t *testing.T) {
	row := fullyPopulatedRow()
	before := row
	for c := ColNone; int(c) < columnCount; c++ {
		_ = c.Value(&row)
	}
	if row != before {
		t.Error("Column.Value mutated the row it read from")
	}
}

// percentileIntervals sweeps the percentile domain densely around every
// documented boundary, so the resolver's if-chain ordering is pinned rather than
// just its six named points. Values between the documented percentiles must
// resolve to the nearest lower one, negatives and NaN must fall to P50.
func percentileIntervals() []float64 {
	return []float64{
		-1.0, -0.5, 0.0, 0.1, 0.499999, 0.5, 0.500001,
		0.59, 0.599999, 0.6, 0.600001, 0.7, 0.9,
		0.949999, 0.95, 0.950001, 0.97,
		0.979999, 0.98, 0.980001, 0.985, 0.989999,
		0.99, 0.990001, 0.995, 0.999999,
		1.0, 1.000001, 2.0, 100.0,
		math.NaN(), math.Inf(1), math.Inf(-1),
	}
}

// TestPercentileColumn_ResolvesChain is the contract the two percentile walkers
// share: the descending if-chain, with its default arm catching negatives and NaN
// (every comparison against NaN is false).
//
// This table is the ONLY guard on the chain's arm ordering: SelectCPUUsagePercentile
// now delegates to CPUPercentileColumn, so a mutation that reorders two arms keeps
// the selector and the resolver in agreement (the differential walk test cannot see
// it) while silently changing which column a percentile between the two thresholds
// resolves to. Hence probes strictly between every adjacent documented percentile,
// not just the six named points.
func TestPercentileColumn_ResolvesChain(t *testing.T) {
	tests := []struct {
		pct     float64
		wantCPU Column
		wantMem Column
	}{
		{-1.0, ColCPUUsageP50MC, ColMemUsageP50KiB},
		{0.0, ColCPUUsageP50MC, ColMemUsageP50KiB},
		{0.499999, ColCPUUsageP50MC, ColMemUsageP50KiB},
		{0.5, ColCPUUsageP50MC, ColMemUsageP50KiB},
		{0.599999, ColCPUUsageP50MC, ColMemUsageP50KiB},
		{0.6, ColCPUUsageP60MC, ColMemUsageP60KiB},
		{0.7, ColCPUUsageP60MC, ColMemUsageP60KiB},
		{0.949999, ColCPUUsageP60MC, ColMemUsageP60KiB},
		{0.95, ColCPUUsageP95MC, ColMemUsageP95KiB},
		{0.979999, ColCPUUsageP95MC, ColMemUsageP95KiB},
		{0.98, ColCPUUsageP98MC, ColMemUsageP98KiB},
		// Strictly between P98 and P99: a reordered 0.99/0.98 arm pair would send
		// these to P99. This probe is the regression guard for that reorder.
		{0.985, ColCPUUsageP98MC, ColMemUsageP98KiB},
		{0.989999, ColCPUUsageP98MC, ColMemUsageP98KiB},
		{0.99, ColCPUUsageP99MC, ColMemUsageP99KiB},
		// Strictly between P99 and max.
		{0.995, ColCPUUsageP99MC, ColMemUsageP99KiB},
		{0.999999, ColCPUUsageP99MC, ColMemUsageP99KiB},
		{1.0, ColCPUUsageMaxMC, ColMemUsageMaxKiB},
		{2.0, ColCPUUsageMaxMC, ColMemUsageMaxKiB},
		// NaN: every comparison is false, so the default arm wins -> P50.
		{math.NaN(), ColCPUUsageP50MC, ColMemUsageP50KiB},
		{math.Inf(1), ColCPUUsageMaxMC, ColMemUsageMaxKiB},
		{math.Inf(-1), ColCPUUsageP50MC, ColMemUsageP50KiB},
	}

	for _, tt := range tests {
		if got := CPUPercentileColumn(tt.pct); got != tt.wantCPU {
			t.Errorf("CPUPercentileColumn(%v) = %v, want %v", tt.pct, got, tt.wantCPU)
		}
		if got := MemPercentileColumn(tt.pct); got != tt.wantMem {
			t.Errorf("MemPercentileColumn(%v) = %v, want %v", tt.pct, got, tt.wantMem)
		}
	}
}

// TestSelectUsagePercentile_MatchesColumnResolver is the anti-drift guard: the
// closure-shaped selector and the Column resolver are two views of one mapping,
// and this proves they cannot disagree for any input in the swept domain —
// including the rows where a wrong column would read a different field.
func TestSelectUsagePercentile_MatchesColumnResolver(t *testing.T) {
	row := fullyPopulatedRow()

	for _, pct := range percentileIntervals() {
		cpuCol := CPUPercentileColumn(pct)
		if got, want := SelectCPUUsagePercentile(row, pct), cpuCol.Value(&row); got != want {
			t.Errorf("SelectCPUUsagePercentile(%v) = %d but column %v = %d", pct, got, cpuCol, want)
		}
		memCol := MemPercentileColumn(pct)
		if got, want := SelectMemUsagePercentile(row, pct), memCol.Value(&row); got != want {
			t.Errorf("SelectMemUsagePercentile(%v) = %d but column %v = %d", pct, got, memCol, want)
		}
	}
}

// percentileFixtureWindows builds digest windows that exercise the walk's edge
// behaviour: age clamping, zero-weight rows, the decay-table cutoff, and shapes
// that make trend slopes differ between series.
func percentileFixtureWindows(now time.Time) []struct {
	name string
	rows []DigestRow
	hl   float64
} {
	series := func(n int, scale int64) []DigestRow {
		rows := make([]DigestRow, n)
		for i := range rows {
			v := int64(i+1) * scale
			rows[i] = DigestRow{
				BucketDate:     now.Add(time.Duration(i-n+1) * 24 * time.Hour),
				CPUUsageP50MC:  v,
				CPUUsageP60MC:  v + 1,
				CPUUsageP95MC:  v + 2,
				CPUUsageP98MC:  v + 3,
				CPUUsageP99MC:  v + 4,
				CPUUsageMaxMC:  v + 5,
				CPUUsageMeanMC: v + 6,
				// Memory is a different shape (quadratic) so CPU and memory
				// trend slopes are distinguishable.
				MemUsageP50KiB:  v * v,
				MemUsageP60KiB:  v*v + 1,
				MemUsageP95KiB:  v*v + 2,
				MemUsageP98KiB:  v*v + 3,
				MemUsageP99KiB:  v*v + 4,
				MemUsageMaxKiB:  v*v + 5,
				MemUsageMeanKiB: v*v + 6,
			}
		}
		return rows
	}

	// Future-dated rows: ageHours clamps to 0, so the newest rows get full weight.
	future := series(3, 10)
	for i := range future {
		future[i].BucketDate = now.Add(time.Duration(i+1) * 24 * time.Hour)
	}

	// One row far enough in the past that the decay table returns 0 for a short
	// half-life, exercising the `w == 0 -> continue` skip.
	ancient := series(3, 10)
	ancient[0].BucketDate = now.Add(-10000 * time.Hour)

	// A single row: n < 2, so both trend guards suppress the slope.
	single := series(1, 10)

	// Identical values: weighted averages collapse to the value, which would mask
	// a wrong weight; included so the differential test also covers it.
	flat := series(4, 0)

	// Exactly two rows: the smallest window where a trend slope is defined. The
	// n >= 2 guard is what makes it defined, so a guard raised to n >= 3 would
	// silently suppress the slope here and nowhere else in the matrix.
	pair := series(2, 10)

	// Rows whose max columns land on round numbers, so opts shapes can set an
	// idle threshold exactly equal to a row's value. The idle check is `>=`, so an
	// exact-equality fixture is the only thing that distinguishes it from `>`.
	idleBoundary := []DigestRow{
		{
			BucketDate: now.Add(-24 * time.Hour), CPUUsageMaxMC: 100, MemUsageMaxKiB: 1000,
			CPUUsageP50MC: 10, CPUUsageP60MC: 20, CPUUsageP95MC: 30, CPUUsageP98MC: 40, CPUUsageP99MC: 50, CPUUsageMeanMC: 25,
			MemUsageP50KiB: 100, MemUsageP60KiB: 200, MemUsageP95KiB: 300, MemUsageP98KiB: 400, MemUsageP99KiB: 500, MemUsageMeanKiB: 250,
		},
		{
			BucketDate: now, CPUUsageMaxMC: 200, MemUsageMaxKiB: 2000,
			CPUUsageP50MC: 20, CPUUsageP60MC: 30, CPUUsageP95MC: 40, CPUUsageP98MC: 50, CPUUsageP99MC: 60, CPUUsageMeanMC: 35,
			MemUsageP50KiB: 200, MemUsageP60KiB: 300, MemUsageP95KiB: 400, MemUsageP98KiB: 500, MemUsageP99KiB: 600, MemUsageMeanKiB: 350,
		},
	}

	return []struct {
		name string
		rows []DigestRow
		hl   float64
	}{
		{"empty", nil, 24},
		{"single", single, 24},
		{"pair", pair, 24},
		{"flat", flat, 24},
		{"future-dated", future, 24},
		{"zero-weight-tail", ancient, 6},
		{"idle-boundary", idleBoundary, 24},
		// Half-life corner cases: 0 and negative mean "no decay", non-integer
		// bypasses the lookup table, and a very large integer half-life exceeds
		// the precomputed table cap and takes the math.Exp fallback.
		{"series-hl0", series(5, 10), 0},
		{"series-hl-neg", series(5, 10), -3},
		{"series-hl-fraction", series(5, 10), 13.5},
		{"series-hl-tiny", series(5, 10), 0.5},
		{"series-hl-huge", series(5, 10), 60000},
		{"series-hl-336", series(30, 10), 336},
		{"series-hl-1", series(5, 10), 1},
	}
}

// TestMultiWeightedPercentileColumns_MatchesClosureWalk is the differential guard
// that makes having two walks safe rather than a latent drift vector: for every
// fixture window, half-life, and opts shape, the descriptor walk must return
// values and extras identical to the closure walk.
func TestMultiWeightedPercentileColumns_MatchesClosureWalk(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

	allColumns := []Column{
		ColCPUUsageP50MC, ColCPUUsageP60MC, ColCPUUsageP95MC, ColCPUUsageP98MC,
		ColCPUUsageP99MC, ColCPUUsageMaxMC, ColCPUUsageMeanMC,
		ColMemUsageP50KiB, ColMemUsageP60KiB, ColMemUsageP95KiB, ColMemUsageP98KiB,
		ColMemUsageP99KiB, ColMemUsageMaxKiB, ColMemUsageMeanKiB,
	}

	// Column list paired with the closure list it must equal, including the
	// dynamic percentile positions so the resolvers are covered too.
	type pair struct {
		name    string
		columns []Column
		closure []func(DigestRow) int64
	}
	pairs := []pair{
		{
			name:    "all-columns",
			columns: allColumns,
			closure: []func(DigestRow) int64{
				func(r DigestRow) int64 { return r.CPUUsageP50MC },
				func(r DigestRow) int64 { return r.CPUUsageP60MC },
				func(r DigestRow) int64 { return r.CPUUsageP95MC },
				func(r DigestRow) int64 { return r.CPUUsageP98MC },
				func(r DigestRow) int64 { return r.CPUUsageP99MC },
				func(r DigestRow) int64 { return r.CPUUsageMaxMC },
				func(r DigestRow) int64 { return r.CPUUsageMeanMC },
				func(r DigestRow) int64 { return r.MemUsageP50KiB },
				func(r DigestRow) int64 { return r.MemUsageP60KiB },
				func(r DigestRow) int64 { return r.MemUsageP95KiB },
				func(r DigestRow) int64 { return r.MemUsageP98KiB },
				func(r DigestRow) int64 { return r.MemUsageP99KiB },
				func(r DigestRow) int64 { return r.MemUsageMaxKiB },
				func(r DigestRow) int64 { return r.MemUsageMeanKiB },
			},
		},
		{
			name:    "dynamic-percentiles",
			columns: []Column{ColCPUUsageP50MC, ColCPUUsageP95MC, ColCPUUsageP50MC, ColCPUUsageP95MC, ColCPUUsageMeanMC, ColMemUsageP50KiB, ColMemUsageP95KiB, ColMemUsageP50KiB, ColMemUsageP95KiB, ColMemUsageMeanKiB},
			closure: []func(DigestRow) int64{
				func(r DigestRow) int64 { return SelectCPUUsagePercentile(r, 0.50) },
				func(r DigestRow) int64 { return SelectCPUUsagePercentile(r, 0.95) },
				func(r DigestRow) int64 { return SelectCPUUsagePercentile(r, 0.50) },
				func(r DigestRow) int64 { return SelectCPUUsagePercentile(r, 0.95) },
				func(r DigestRow) int64 { return r.CPUUsageMeanMC },
				func(r DigestRow) int64 { return SelectMemUsagePercentile(r, 0.50) },
				func(r DigestRow) int64 { return SelectMemUsagePercentile(r, 0.95) },
				func(r DigestRow) int64 { return SelectMemUsagePercentile(r, 0.50) },
				func(r DigestRow) int64 { return SelectMemUsagePercentile(r, 0.95) },
				func(r DigestRow) int64 { return r.MemUsageMeanKiB },
			},
		},
		{
			name:    "reversed-order",
			columns: []Column{ColMemUsageMeanKiB, ColCPUUsageMeanMC, ColMemUsageMaxKiB, ColCPUUsageMaxMC},
			closure: []func(DigestRow) int64{
				func(r DigestRow) int64 { return r.MemUsageMeanKiB },
				func(r DigestRow) int64 { return r.CPUUsageMeanMC },
				func(r DigestRow) int64 { return r.MemUsageMaxKiB },
				func(r DigestRow) int64 { return r.CPUUsageMaxMC },
			},
		},
	}

	// Opts shapes: nil, both trends, CPU trend only, memory trend only, idle on
	// and off, and thresholds that flip the idle verdict.
	type optShape struct {
		name string
		cols *ColumnWindowOpts
		fns  *WindowExtraOpts
	}
	optShapes := []optShape{
		{
			name: "nil-opts",
			cols: nil,
			fns:  nil,
		},
		{
			name: "both-trends",
			cols: &ColumnWindowOpts{TrendColumn: ColCPUUsageP98MC, MemTrendColumn: ColMemUsageP95KiB},
			fns:  &WindowExtraOpts{TrendMetric: func(r DigestRow) int64 { return r.CPUUsageP98MC }, MemTrendMetric: func(r DigestRow) int64 { return r.MemUsageP95KiB }},
		},
		{
			name: "cpu-trend-only",
			cols: &ColumnWindowOpts{TrendColumn: ColCPUUsageP98MC},
			fns:  &WindowExtraOpts{TrendMetric: func(r DigestRow) int64 { return r.CPUUsageP98MC }},
		},
		{
			name: "mem-trend-only",
			cols: &ColumnWindowOpts{MemTrendColumn: ColMemUsageP95KiB},
			fns:  &WindowExtraOpts{MemTrendMetric: func(r DigestRow) int64 { return r.MemUsageP95KiB }},
		},
		{
			name: "idle-never",
			cols: &ColumnWindowOpts{TrendColumn: ColCPUUsageP98MC, MemTrendColumn: ColMemUsageP95KiB, DetectIdle: true, IdleThresholdMC: 1 << 20, IdleThresholdMem: 1 << 30},
			fns:  &WindowExtraOpts{TrendMetric: func(r DigestRow) int64 { return r.CPUUsageP98MC }, MemTrendMetric: func(r DigestRow) int64 { return r.MemUsageP95KiB }, DetectIdle: true, IdleThresholdMC: 1 << 20, IdleThresholdMem: 1 << 30},
		},
		{
			name: "idle-always",
			cols: &ColumnWindowOpts{TrendColumn: ColCPUUsageP98MC, MemTrendColumn: ColMemUsageP95KiB, DetectIdle: true, IdleThresholdMC: 1, IdleThresholdMem: 1},
			fns:  &WindowExtraOpts{TrendMetric: func(r DigestRow) int64 { return r.CPUUsageP98MC }, MemTrendMetric: func(r DigestRow) int64 { return r.MemUsageP95KiB }, DetectIdle: true, IdleThresholdMC: 1, IdleThresholdMem: 1},
		},
		{
			name: "idle-on-mem-only",
			// MemUsageMaxKiB high but CPUUsageMaxMC low, so only the memory
			// threshold can clear the idle verdict.
			cols: &ColumnWindowOpts{DetectIdle: true, IdleThresholdMC: 1, IdleThresholdMem: 1 << 30},
			fns:  &WindowExtraOpts{DetectIdle: true, IdleThresholdMC: 1, IdleThresholdMem: 1 << 30},
		},
		{
			// Thresholds exactly equal to the idle-boundary fixture's largest max
			// columns (200 / 2000), so the ONLY row that can clear the threshold
			// does so by equality. The check is `>=`, so equality must clear the
			// idle verdict; a `>` would leave the window idle. A threshold set to a
			// lower row's value would be masked by that row anyway.
			name: "idle-threshold-equal-to-row-max",
			cols: &ColumnWindowOpts{TrendColumn: ColCPUUsageP98MC, MemTrendColumn: ColMemUsageP95KiB, DetectIdle: true, IdleThresholdMC: 200, IdleThresholdMem: 2000},
			fns:  &WindowExtraOpts{TrendMetric: func(r DigestRow) int64 { return r.CPUUsageP98MC }, MemTrendMetric: func(r DigestRow) int64 { return r.MemUsageP95KiB }, DetectIdle: true, IdleThresholdMC: 200, IdleThresholdMem: 2000},
		},
		{
			// One below the boundary row's maxima: nothing clears the threshold,
			// so the window must stay idle.
			name: "idle-threshold-just-below-row-max",
			cols: &ColumnWindowOpts{TrendColumn: ColCPUUsageP98MC, MemTrendColumn: ColMemUsageP95KiB, DetectIdle: true, IdleThresholdMC: 99, IdleThresholdMem: 999},
			fns:  &WindowExtraOpts{TrendMetric: func(r DigestRow) int64 { return r.CPUUsageP98MC }, MemTrendMetric: func(r DigestRow) int64 { return r.MemUsageP95KiB }, DetectIdle: true, IdleThresholdMC: 99, IdleThresholdMem: 999},
		},
		{
			// One above: clears on the first row alone, which is where a
			// strict-greater comparison would diverge.
			name: "idle-threshold-just-above-row-max",
			cols: &ColumnWindowOpts{TrendColumn: ColCPUUsageP98MC, MemTrendColumn: ColMemUsageP95KiB, DetectIdle: true, IdleThresholdMC: 101, IdleThresholdMem: 1001},
			fns:  &WindowExtraOpts{TrendMetric: func(r DigestRow) int64 { return r.CPUUsageP98MC }, MemTrendMetric: func(r DigestRow) int64 { return r.MemUsageP95KiB }, DetectIdle: true, IdleThresholdMC: 101, IdleThresholdMem: 1001},
		},
		{
			// CPU threshold exactly on the boundary row's CPUUsageMaxMC with the
			// memory threshold unreachable, so only the CPU comparison can clear
			// the verdict. Without this isolation a strict-greater mutation of the
			// CPU comparison is masked by the memory one.
			name: "idle-cpu-threshold-exact-mem-unreachable",
			cols: &ColumnWindowOpts{TrendColumn: ColCPUUsageP98MC, MemTrendColumn: ColMemUsageP95KiB, DetectIdle: true, IdleThresholdMC: 200, IdleThresholdMem: 1 << 40},
			fns:  &WindowExtraOpts{TrendMetric: func(r DigestRow) int64 { return r.CPUUsageP98MC }, MemTrendMetric: func(r DigestRow) int64 { return r.MemUsageP95KiB }, DetectIdle: true, IdleThresholdMC: 200, IdleThresholdMem: 1 << 40},
		},
		{
			// The mirror image: memory threshold exact, CPU unreachable.
			name: "idle-mem-threshold-exact-cpu-unreachable",
			cols: &ColumnWindowOpts{TrendColumn: ColCPUUsageP98MC, MemTrendColumn: ColMemUsageP95KiB, DetectIdle: true, IdleThresholdMC: 1 << 40, IdleThresholdMem: 2000},
			fns:  &WindowExtraOpts{TrendMetric: func(r DigestRow) int64 { return r.CPUUsageP98MC }, MemTrendMetric: func(r DigestRow) int64 { return r.MemUsageP95KiB }, DetectIdle: true, IdleThresholdMC: 1 << 40, IdleThresholdMem: 2000},
		},
	}

	for _, w := range percentileFixtureWindows(now) {
		for _, p := range pairs {
			for _, o := range optShapes {
				gotVals, gotExtras := MultiWeightedPercentileColumns(w.rows, now, w.hl, o.cols, p.columns...)
				wantVals, wantExtras := MultiWeightedPercentileWithExtras(w.rows, now, w.hl, o.fns, p.closure...)

				label := w.name + "/" + p.name + "/" + o.name
				if len(gotVals) != len(wantVals) {
					t.Fatalf("%s: got %d values, want %d", label, len(gotVals), len(wantVals))
				}
				for i := range gotVals {
					if gotVals[i] != wantVals[i] {
						t.Errorf("%s: column %d = %d, closure walk = %d (weighting or column order diverged)",
							label, i, gotVals[i], wantVals[i])
					}
				}
				if gotExtras != wantExtras {
					t.Errorf("%s: extras %+v, closure walk %+v (trend or idle diverged)",
						label, gotExtras, wantExtras)
				}
			}
		}
	}
}

// TestMultiWeightedPercentileColumns_ZeroColumns pins the documented no-op: a
// zero-length column list returns an empty (non-nil) slice and zero extras, so a
// caller that builds an empty list by mistake cannot index a nil slice.
func TestMultiWeightedPercentileColumns_ZeroColumns(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := []DigestRow{{BucketDate: now, CPUUsageP95MC: 5}}

	vals, extras := MultiWeightedPercentileColumns(rows, now, 24, nil)
	if len(vals) != 0 {
		t.Errorf("no columns should yield no values, got %v", vals)
	}
	if vals == nil {
		t.Error("no columns should still return a non-nil empty slice")
	}
	if extras != (WindowExtras{}) {
		t.Errorf("no columns should yield zero extras, got %+v", extras)
	}

	// Rows present but empty list, and empty rows with a list: both no-ops.
	vals, _ = MultiWeightedPercentileColumns(nil, now, 24, nil, ColCPUUsageP95MC)
	if len(vals) != 1 || vals[0] != 0 {
		t.Errorf("empty rows should yield one zero, got %v", vals)
	}
}

// TestMultiWeightedPercentileColumns_DuplicateColumns pins that a repeated column
// yields two independent output slots, which is what the fused path relies on
// when the cost and performance percentiles resolve to the same column.
func TestMultiWeightedPercentileColumns_DuplicateColumns(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now.Add(-2 * 24 * time.Hour), CPUUsageP95MC: 100},
		{BucketDate: now.Add(-24 * time.Hour), CPUUsageP95MC: 200},
		{BucketDate: now, CPUUsageP95MC: 300},
	}

	vals, _ := MultiWeightedPercentileColumns(rows, now, 24, nil,
		ColCPUUsageP95MC, ColCPUUsageP95MC, ColCPUUsageP95MC)
	if len(vals) != 3 {
		t.Fatalf("got %d values, want 3", len(vals))
	}
	for i, v := range vals {
		if v != vals[0] {
			t.Errorf("slot %d = %d, slot 0 = %d: duplicated columns must agree", i, v, vals[0])
		}
	}
}

// TestMultiWeightedPercentileColumns_AllZeroWeights pins the totalWeight == 0
// path: rows so old that every weight is 0 produce zeroed outputs, not a
// division by zero.
func TestMultiWeightedPercentileColumns_AllZeroWeights(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	// Half-life 1 => the table is 2 entries, so ages >= 2h weigh 0.
	rows := []DigestRow{
		{BucketDate: now.Add(-48 * time.Hour), CPUUsageP95MC: 111},
		{BucketDate: now.Add(-72 * time.Hour), CPUUsageP95MC: 222},
	}

	vals, _ := MultiWeightedPercentileColumns(rows, now, 1, nil, ColCPUUsageP95MC)
	if len(vals) != 1 || vals[0] != 0 {
		t.Errorf("all-zero weights should yield 0, got %v", vals)
	}
}

// TestMultiWeightedPercentileColumns_DoesNotMutateRows guards the pointer-based
// row access: the walk takes &rows[i] for speed, so a write through that pointer
// would corrupt the caller's window.
func TestMultiWeightedPercentileColumns_DoesNotMutateRows(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now.Add(-24 * time.Hour), CPUUsageP95MC: 100, MemUsageP95KiB: 200},
		{BucketDate: now, CPUUsageP95MC: 300, MemUsageP95KiB: 400},
	}
	before := make([]DigestRow, len(rows))
	copy(before, rows)

	MultiWeightedPercentileColumns(rows, now, 24,
		&ColumnWindowOpts{TrendColumn: ColCPUUsageP98MC, MemTrendColumn: ColMemUsageP95KiB, DetectIdle: true, IdleThresholdMC: 1, IdleThresholdMem: 1},
		ColCPUUsageP95MC, ColMemUsageP95KiB)

	for i := range rows {
		if rows[i] != before[i] {
			t.Errorf("row %d mutated: %+v, was %+v", i, rows[i], before[i])
		}
	}
}
