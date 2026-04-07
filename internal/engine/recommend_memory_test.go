package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
