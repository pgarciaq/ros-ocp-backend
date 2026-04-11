package engine

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecommendMemory_UsesMaxNotSort(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, MemUsageP50KiB: 2048, MemUsageP95KiB: 3072, MemUsageMaxKiB: 4096, MemUsageMeanKiB: 2500, SampleCount: 96},
	}
	cfg := DefaultMemoryConfig(now, 0)
	rec := RecommendMemory(rows, cfg)
	// Perf model uses max (4096), min margin 1.15 → 4096 * 1.15 = 4710
	assert.GreaterOrEqual(t, rec.PerfRequestKiB, int64(4096))
}

func TestRecommendMemory_AdaptiveMargin_StableWorkload(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	// p95 ≈ p50 ≈ mean → stable → margin ~1.15
	rows := []DigestRow{
		{BucketDate: now, MemUsageP50KiB: 1000, MemUsageP95KiB: 1050, MemUsageMaxKiB: 1100, MemUsageMeanKiB: 1000, SampleCount: 96},
	}
	cfg := DefaultMemoryConfig(now, 0)
	rec := RecommendMemory(rows, cfg)
	// Cost rec from p95 (1050) with margin ~1.15 → ~1208
	assert.InDelta(t, 1208, float64(rec.CostRequestKiB), 50)
}

func TestRecommendMemory_AdaptiveMargin_VariableWorkload(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	// p95 = 3x p50 → high variability → margin clamps to 1.50
	rows := []DigestRow{
		{BucketDate: now, MemUsageP50KiB: 1000, MemUsageP95KiB: 3000, MemUsageMaxKiB: 4000, MemUsageMeanKiB: 1200, SampleCount: 96},
	}
	cfg := DefaultMemoryConfig(now, 0)
	rec := RecommendMemory(rows, cfg)
	// Cost rec from p95 (3000) with margin 1.50 → 4500
	assert.GreaterOrEqual(t, rec.CostRequestKiB, int64(4000))
}

func TestRecommendMemory_CostModel_UsesP95(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, MemUsageP50KiB: 1024, MemUsageP95KiB: 4096, MemUsageMaxKiB: 8192, MemUsageMeanKiB: 2048, SampleCount: 96},
	}
	cfg := DefaultMemoryConfig(now, 0)
	rec := RecommendMemory(rows, cfg)
	// Cost derives from p95 (4096), perf from max (8192)
	assert.Less(t, rec.CostRequestKiB, rec.PerfRequestKiB)
}

func TestRecommendMemory_PerfModel_UsesMax(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, MemUsageP50KiB: 1024, MemUsageP95KiB: 4096, MemUsageMaxKiB: 8192, MemUsageMeanKiB: 2048, SampleCount: 96},
	}
	cfg := DefaultMemoryConfig(now, 0)
	rec := RecommendMemory(rows, cfg)
	// Perf uses max (8192) with margin → must be >= 8192
	assert.GreaterOrEqual(t, rec.PerfRequestKiB, int64(8192))
}

func TestRecommendMemory_SeparateRequestAndLimit(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, MemUsageP50KiB: 2048, MemUsageP95KiB: 4096, MemUsageMaxKiB: 8192, MemUsageMeanKiB: 3000, SampleCount: 96},
	}
	cfg := DefaultMemoryConfig(now, 0)
	rec := RecommendMemory(rows, cfg)
	assert.Greater(t, rec.CostLimitKiB, rec.CostRequestKiB)
	assert.Greater(t, rec.PerfLimitKiB, rec.PerfRequestKiB)
}

func TestRecommendMemory_Empty_ReturnsZero(t *testing.T) {
	cfg := DefaultMemoryConfig(time.Now(), 0)
	rec := RecommendMemory(nil, cfg)
	assert.Equal(t, int64(0), rec.CostRequestKiB)
	assert.Equal(t, int64(0), rec.PerfRequestKiB)
}

func TestRecommendMemory_TrendSlope(t *testing.T) {
	now := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	rows := make([]DigestRow, 7)
	for i := range rows {
		rows[i] = DigestRow{
			BucketDate:      now.AddDate(0, 0, i-6),
			MemUsageP50KiB:  int64(1000 + i*100),
			MemUsageP95KiB:  int64(2000 + i*100),
			MemUsageMaxKiB:  int64(3000 + i*100),
			MemUsageMeanKiB: int64(1500 + i*100),
			SampleCount:     96,
		}
	}
	cfg := DefaultMemoryConfig(now, 0)
	rec := RecommendMemory(rows, cfg)
	assert.Greater(t, rec.TrendSlope, 0.0)
}

// stableRow returns a DigestRow with stable memory profile for OOM bump tests.
func stableRow(now time.Time) DigestRow {
	return DigestRow{
		BucketDate:      now,
		MemUsageP50KiB:  1000,
		MemUsageP95KiB:  1050,
		MemUsageMaxKiB:  1100,
		MemUsageMeanKiB: 1000,
		SampleCount:     96,
	}
}

func TestRecommendMemory_OOMBump(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{stableRow(now)}

	baseline := DefaultMemoryConfig(now, 0)
	baseRec := RecommendMemory(rows, baseline)

	withOOM := DefaultMemoryConfig(now, 0)
	withOOM.OOMCountSum = 1
	oomRec := RecommendMemory(rows, withOOM)

	// 1 OOM with default BaseBump=0.15 → bump = 1 + 0.15*log2(2) = 1.15 → ~15% increase
	expectedBump := 1.0 + 0.15*math.Log2(2)
	assert.InDelta(t, float64(baseRec.CostRequestKiB)*expectedBump, float64(oomRec.CostRequestKiB), 2)
	assert.Greater(t, oomRec.CostRequestKiB, baseRec.CostRequestKiB)
	assert.Greater(t, oomRec.PerfRequestKiB, baseRec.PerfRequestKiB)
}

func TestRecommendMemory_ZeroOOM(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{stableRow(now)}

	noOOM := DefaultMemoryConfig(now, 0)
	noOOM.OOMCountSum = 0
	rec := RecommendMemory(rows, noOOM)

	baseline := DefaultMemoryConfig(now, 0)
	baseRec := RecommendMemory(rows, baseline)

	assert.Equal(t, baseRec.CostRequestKiB, rec.CostRequestKiB)
	assert.Equal(t, baseRec.PerfRequestKiB, rec.PerfRequestKiB)
}

func TestRecommendMemory_OOMLogScale(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{stableRow(now)}

	baseline := DefaultMemoryConfig(now, 0)
	baseRec := RecommendMemory(rows, baseline)
	require.Greater(t, baseRec.CostRequestKiB, int64(0))

	tests := []struct {
		oomCount int64
		wantBump float64
	}{
		{1, 1.0 + 0.15*math.Log2(2)},
		{3, 1.0 + 0.15*math.Log2(4)},
		{7, 1.0 + 0.15*math.Log2(8)},
		{15, 1.0 + 0.15*math.Log2(16)},
	}

	var prevCostReq int64
	for _, tt := range tests {
		t.Run(fmt.Sprintf("oom_count_%d", tt.oomCount), func(t *testing.T) {
			cfg := DefaultMemoryConfig(now, 0)
			cfg.OOMCountSum = tt.oomCount
			rec := RecommendMemory(rows, cfg)

			expectedCost := int64(math.Round(float64(baseRec.CostRequestKiB) * tt.wantBump))
			assert.InDelta(t, expectedCost, rec.CostRequestKiB, 2)

			if prevCostReq > 0 {
				assert.Greater(t, rec.CostRequestKiB, prevCostReq, "bump should increase with more OOMs")
			}
			prevCostReq = rec.CostRequestKiB
		})
	}
}

func TestRecommendMemory_OOMMaxBumpCap(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{stableRow(now)}

	baseline := DefaultMemoryConfig(now, 0)
	baseRec := RecommendMemory(rows, baseline)

	cfg := DefaultMemoryConfig(now, 0)
	cfg.OOMCountSum = 100
	rec := RecommendMemory(rows, cfg)

	// bump = min(1.60, 1 + 0.15*log2(101)) = min(1.60, ~2.0) = 1.60
	maxCost := int64(math.Round(float64(baseRec.CostRequestKiB) * 1.60))
	assert.Equal(t, maxCost, rec.CostRequestKiB)
}

func TestRecommendMemory_OOMCustomParams(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{stableRow(now)}

	baseline := DefaultMemoryConfig(now, 0)
	baseRec := RecommendMemory(rows, baseline)

	cfg := DefaultMemoryConfig(now, 0)
	cfg.OOMCountSum = 3
	cfg.OOMBaseBump = 0.30
	cfg.OOMMaxBump = 2.00
	rec := RecommendMemory(rows, cfg)

	expectedBump := 1.0 + 0.30*math.Log2(4) // 1 + 0.30*2 = 1.60
	expectedCost := int64(math.Round(float64(baseRec.CostRequestKiB) * expectedBump))
	assert.InDelta(t, expectedCost, rec.CostRequestKiB, 2)
}

func TestRecommendMemory_OOMBumpAppliesBeforeLimit(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{stableRow(now)}

	cfg := DefaultMemoryConfig(now, 0)
	cfg.OOMCountSum = 3
	rec := RecommendMemory(rows, cfg)

	// Limit should be request * LimitMultiplier (1.05), post-OOM-bump
	expectedLimit := int64(math.Round(float64(rec.CostRequestKiB) * 1.05))
	assert.Equal(t, expectedLimit, rec.CostLimitKiB)

	expectedPerfLimit := int64(math.Round(float64(rec.PerfRequestKiB) * 1.05))
	assert.Equal(t, expectedPerfLimit, rec.PerfLimitKiB)
}
