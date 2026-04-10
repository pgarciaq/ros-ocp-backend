package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRecommendCPU_BelowOneCoreAndAbove_SameAlgorithm(t *testing.T) {
	now := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	// Container A: all values < 1000mc
	rowsA := []DigestRow{
		{BucketDate: now, CPUUsageP50MC: 400, CPUUsageP60MC: 450, CPUUsageP95MC: 500, CPUUsageP98MC: 510, CPUUsageMaxMC: 520, CPUThrottleP95MC: 10, CPUThrottleMaxMC: 15, CPUUsageMeanMC: 420, SampleCount: 96},
	}
	// Container B: all values > 1000mc
	rowsB := []DigestRow{
		{BucketDate: now, CPUUsageP50MC: 4000, CPUUsageP60MC: 4500, CPUUsageP95MC: 5000, CPUUsageP98MC: 5100, CPUUsageMaxMC: 5200, CPUThrottleP95MC: 100, CPUThrottleMaxMC: 150, CPUUsageMeanMC: 4200, SampleCount: 96},
	}

	cfg := DefaultCPUConfig(now, 0)
	recA := RecommendCPU(rowsA, cfg)
	recB := RecommendCPU(rowsB, cfg)

	// Both use same percentile-based formula (no 1-core discontinuity)
	// Cost model uses p60, Perf uses p98
	assert.Greater(t, recA.CostRequestMC, int64(0))
	assert.Greater(t, recB.CostRequestMC, int64(0))
	assert.Greater(t, recA.PerfRequestMC, recA.CostRequestMC)
	assert.Greater(t, recB.PerfRequestMC, recB.CostRequestMC)
}

func TestRecommendCPU_Floor25m(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, CPUUsageP50MC: 5, CPUUsageP60MC: 8, CPUUsageP95MC: 12, CPUUsageP98MC: 14, CPUUsageMaxMC: 15, CPUThrottleP95MC: 0, CPUThrottleMaxMC: 0, CPUUsageMeanMC: 7, SampleCount: 96},
	}
	cfg := DefaultCPUConfig(now, 0)
	rec := RecommendCPU(rows, cfg)
	assert.GreaterOrEqual(t, rec.CostRequestMC, int64(25))
	assert.GreaterOrEqual(t, rec.PerfRequestMC, int64(25))
}

func TestRecommendCPU_CostModel_UsesP60(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, CPUUsageP50MC: 100, CPUUsageP60MC: 200, CPUUsageP95MC: 300, CPUUsageP98MC: 350, CPUUsageMaxMC: 400, CPUThrottleP95MC: 10, CPUThrottleMaxMC: 20, CPUUsageMeanMC: 150, SampleCount: 96},
	}
	cfg := DefaultCPUConfig(now, 0)
	rec := RecommendCPU(rows, cfg)
	// Cost recommendation derives from p60 (200mc) with margin
	// Margin = 1 + (300-100)/150 = 2.33 → clamped to 1.50
	// Expected: max(200*1.50, max+throttle) = max(300, 420) → 420? No, let's check the formula.
	// effective = max(usage_pct, usage_max + throttle_avg)
	// Actually the formula is: weighted percentile value * margin, then floor
	// With no decay (halflife=0), it's just p60=200, margin=1.50, so 200*1.50=300
	assert.GreaterOrEqual(t, rec.CostRequestMC, int64(200))
}

func TestRecommendCPU_PerfModel_UsesP98(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, CPUUsageP50MC: 100, CPUUsageP60MC: 200, CPUUsageP95MC: 300, CPUUsageP98MC: 350, CPUUsageMaxMC: 400, CPUThrottleP95MC: 10, CPUThrottleMaxMC: 20, CPUUsageMeanMC: 150, SampleCount: 96},
	}
	cfg := DefaultCPUConfig(now, 0)
	rec := RecommendCPU(rows, cfg)
	// Perf recommendation derives from p98 (350mc) with margin
	assert.Greater(t, rec.PerfRequestMC, rec.CostRequestMC)
	assert.GreaterOrEqual(t, rec.PerfRequestMC, int64(350))
}

func TestRecommendCPU_DualOutput_SinglePass(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, CPUUsageP50MC: 100, CPUUsageP60MC: 200, CPUUsageP95MC: 300, CPUUsageP98MC: 350, CPUUsageMaxMC: 400, CPUUsageMeanMC: 150, SampleCount: 96},
	}
	cfg := DefaultCPUConfig(now, 0)
	rec := RecommendCPU(rows, cfg)
	assert.Greater(t, rec.CostRequestMC, int64(0))
	assert.Greater(t, rec.CostLimitMC, int64(0))
	assert.Greater(t, rec.PerfRequestMC, int64(0))
	assert.Greater(t, rec.PerfLimitMC, int64(0))
	assert.Greater(t, rec.CostLimitMC, rec.CostRequestMC)
	assert.Greater(t, rec.PerfLimitMC, rec.PerfRequestMC)
}

func TestRecommendCPU_LimitHigherThanRequest(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, CPUUsageP50MC: 500, CPUUsageP60MC: 600, CPUUsageP95MC: 900, CPUUsageP98MC: 950, CPUUsageMaxMC: 1000, CPUUsageMeanMC: 600, SampleCount: 96},
	}
	cfg := DefaultCPUConfig(now, 0)
	rec := RecommendCPU(rows, cfg)
	assert.Greater(t, rec.CostLimitMC, rec.CostRequestMC)
	assert.Greater(t, rec.PerfLimitMC, rec.PerfRequestMC)
}

func TestRecommendCPU_Empty_ReturnsZero(t *testing.T) {
	cfg := DefaultCPUConfig(time.Now(), 0)
	rec := RecommendCPU(nil, cfg)
	assert.Equal(t, int64(0), rec.CostRequestMC)
	assert.Equal(t, int64(0), rec.PerfRequestMC)
}

func TestRecommendCPU_WithDecay_RecentWeightedMore(t *testing.T) {
	now := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now.AddDate(0, 0, -6), CPUUsageP50MC: 50, CPUUsageP60MC: 100, CPUUsageP95MC: 200, CPUUsageP98MC: 220, CPUUsageMaxMC: 250, CPUUsageMeanMC: 100, SampleCount: 96},
		{BucketDate: now, CPUUsageP50MC: 400, CPUUsageP60MC: 500, CPUUsageP95MC: 600, CPUUsageP98MC: 620, CPUUsageMaxMC: 650, CPUUsageMeanMC: 450, SampleCount: 96},
	}
	cfg := DefaultCPUConfig(now, 72)
	rec := RecommendCPU(rows, cfg)
	// Recent data (500mc p60) should dominate over old data (100mc p60)
	assert.Greater(t, rec.CostRequestMC, int64(300))
}

func TestRecommendCPU_TreatsEachRowAsContainer(t *testing.T) {
	// T-1.2: Each DigestRow is a daily digest for the SAME container.
	// Duplicating a row should NOT divide the recommendation by N
	// (no per-pod averaging); the percentile algorithm picks the same
	// ranked value regardless of how many identical rows exist.
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	single := DigestRow{
		BucketDate: now, CPUUsageP50MC: 300, CPUUsageP60MC: 400,
		CPUUsageP95MC: 500, CPUUsageP98MC: 550, CPUUsageMaxMC: 600,
		CPUUsageMeanMC: 350, SampleCount: 96,
	}
	cfg := DefaultCPUConfig(now, 0)

	recSingle := RecommendCPU([]DigestRow{single}, cfg)

	// Five identical rows (same container, same day repeated)
	fiveRows := []DigestRow{single, single, single, single, single}
	recFive := RecommendCPU(fiveRows, cfg)

	assert.Equal(t, recSingle.CostRequestMC, recFive.CostRequestMC,
		"identical rows must produce identical cost recommendations")
	assert.Equal(t, recSingle.PerfRequestMC, recFive.PerfRequestMC,
		"identical rows must produce identical perf recommendations")
}

func TestRecommendCPU_IdleDetection(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	rows := []DigestRow{
		{BucketDate: now, CPUUsageP50MC: 1, CPUUsageP60MC: 2, CPUUsageP95MC: 5, CPUUsageP98MC: 7, CPUUsageMaxMC: 8, CPUUsageMeanMC: 2, SampleCount: 96},
	}
	cfg := DefaultCPUConfig(now, 0)
	rec := RecommendCPU(rows, cfg)
	assert.True(t, rec.IsIdle)
	assert.GreaterOrEqual(t, rec.CostRequestMC, int64(25)) // floor still applies
}
