package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var defaultMediumTerm = TermConfig{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168}

func TestComputePVCRecommendation_Orphaned(t *testing.T) {
	digests := make([]PVCDigestRow, 5)
	for i := range digests {
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "production",
			PVC:           "old-data-pvc",
			PV:            "pv-001",
			StorageClass:  "gp3",
			CapacityBytes: 100 << 30, // 100 GiB
			UsageBytesMin: 0,
			UsageBytesMax: 0,
			UsageBytesAvg: 0,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm)

	assert.Equal(t, PVCRecTypeOrphaned, rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, NotifPVCOrphaned)
	assert.Equal(t, int64(0), rec.UsageBytesMax)
	assert.Equal(t, float64(0), rec.UsageRatio)
	assert.Equal(t, 5, rec.DataDays)
	assert.Equal(t, "medium", rec.Term)
}

func TestComputePVCRecommendation_Oversized(t *testing.T) {
	digests := make([]PVCDigestRow, 7)
	for i := range digests {
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "staging",
			PVC:           "app-logs",
			PV:            "pv-002",
			StorageClass:  "gp3",
			CapacityBytes: 100 << 30, // 100 GiB
			UsageBytesMin: 5 << 30,   // 5 GiB
			UsageBytesMax: 10 << 30,  // 10 GiB (10% usage)
			UsageBytesAvg: 7 << 30,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm)

	assert.Equal(t, PVCRecTypeOversized, rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, NotifPVCOversized)
	assert.NotNil(t, rec.RecommendedBytes)
	assert.Equal(t, int64(20<<30), *rec.RecommendedBytes)
	assert.InDelta(t, 0.10, rec.UsageRatio, 0.01)
	assert.Equal(t, "medium", rec.Term)
}

func TestComputePVCRecommendation_NearFull(t *testing.T) {
	digests := make([]PVCDigestRow, 3)
	for i := range digests {
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "production",
			PVC:           "db-data",
			PV:            "pv-003",
			StorageClass:  "io2",
			CapacityBytes: 50 << 30, // 50 GiB
			UsageBytesMin: 40 << 30,
			UsageBytesMax: 45 << 30, // 90% usage
			UsageBytesAvg: 42 << 30,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm)

	assert.Equal(t, PVCRecTypeNearFull, rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, NotifPVCNearFull)
	assert.NotNil(t, rec.RecommendedBytes)
	assert.InDelta(t, 0.90, rec.UsageRatio, 0.01)
	assert.Equal(t, "medium", rec.Term)
}

func TestComputePVCRecommendation_Healthy(t *testing.T) {
	digests := make([]PVCDigestRow, 5)
	for i := range digests {
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "production",
			PVC:           "app-data",
			PV:            "pv-004",
			StorageClass:  "gp3",
			CapacityBytes: 100 << 30, // 100 GiB
			UsageBytesMin: 30 << 30,
			UsageBytesMax: 50 << 30, // 50% usage — healthy
			UsageBytesAvg: 40 << 30,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm)

	assert.Equal(t, PVCRecTypeHealthy, rec.RecommendationType)
	assert.Empty(t, rec.NotificationCodes)
	assert.Equal(t, "medium", rec.Term)
}

func TestComputePVCRecommendation_GrowthTrend(t *testing.T) {
	// Simulate linear growth: 10 GiB at day 0, growing 1 GiB/day
	digests := make([]PVCDigestRow, 10)
	for i := range digests {
		usage := int64((10 + i) << 30)
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "production",
			PVC:           "growing-pvc",
			PV:            "pv-005",
			StorageClass:  "gp3",
			CapacityBytes: 50 << 30, // 50 GiB
			UsageBytesMin: usage - (1 << 29),
			UsageBytesMax: usage,
			UsageBytesAvg: usage,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm)

	assert.InDelta(t, float64(1<<30), float64(rec.GrowthBytesPerDay), float64(1<<28))
	assert.NotNil(t, rec.DaysToFull)
	assert.InDelta(t, 31, *rec.DaysToFull, 5)
	assert.Equal(t, "medium", rec.Term)
}

func TestComputePVCRecommendation_InsufficientData(t *testing.T) {
	// Only 2 days of zero usage — should NOT be classified as orphaned (medium needs 3)
	digests := []PVCDigestRow{
		{
			BucketDate:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Namespace:     "test",
			PVC:           "new-pvc",
			CapacityBytes: 10 << 30,
			UsageBytesMax: 0,
		},
		{
			BucketDate:    time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
			Namespace:     "test",
			PVC:           "new-pvc",
			CapacityBytes: 10 << 30,
			UsageBytesMax: 0,
		},
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm)

	assert.Equal(t, PVCRecTypeHealthy, rec.RecommendationType)
}

func TestComputePVCRecommendation_ShortTermSeesBurst(t *testing.T) {
	// 15 days of data: stable at 30 GiB for first 14 days, then spike to 90 GiB on day 15.
	// Short term (1 day window) sees the spike at near_full, long term has enough data.
	shortTerm := TermConfig{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0}
	longTerm := TermConfig{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360}

	digests := make([]PVCDigestRow, 15)
	for i := range digests {
		usage := int64(30 << 30) // 30 GiB
		if i == 14 {
			usage = int64(90 << 30) // spike on last day
		}
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "analytics",
			PVC:           "spark-scratch",
			PV:            "pv-006",
			StorageClass:  "gp3",
			CapacityBytes: 100 << 30,
			UsageBytesMin: usage - (2 << 30),
			UsageBytesMax: usage,
			UsageBytesAvg: usage,
			SampleCount:   24,
		}
	}

	// Short term (window=1): sees day 14 + 15 (cutoff = latest - 1 day).
	// Max usage is 90 GiB → near_full
	shortWindow := windowDigests(digests, shortTerm.WindowDays)
	recShort := computePVCRecommendation(shortWindow, "org123", "cluster-uuid", shortTerm)
	assert.Equal(t, PVCRecTypeNearFull, recShort.RecommendationType)
	assert.Equal(t, "short", recShort.Term)

	// Long term: sees all 15 days — max is still 90 GiB (near_full),
	// but has growth trend data over the full window
	longWindow := windowDigests(digests, longTerm.WindowDays)
	recLong := computePVCRecommendation(longWindow, "org123", "cluster-uuid", longTerm)
	assert.Equal(t, PVCRecTypeNearFull, recLong.RecommendationType)
	assert.Equal(t, "long", recLong.Term)
	assert.True(t, recLong.DataDays >= 15)
}

func TestComputePVCRecommendation_ShortTermInsufficientButLongTermClassifies(t *testing.T) {
	// 2 days of zero usage with short term (min_data=1) and medium term (min_data=3).
	// Short term with 2 days of data can classify (>=1), medium needs >=3.
	shortTerm := TermConfig{Name: "short", WindowDays: 2, MinDataDays: 1, DecayHalfLifeHours: 0}

	digests := []PVCDigestRow{
		{
			BucketDate:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Namespace:     "test",
			PVC:           "maybe-orphan",
			CapacityBytes: 10 << 30,
			UsageBytesMax: 0,
			UsageBytesAvg: 0,
		},
		{
			BucketDate:    time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
			Namespace:     "test",
			PVC:           "maybe-orphan",
			CapacityBytes: 10 << 30,
			UsageBytesMax: 0,
			UsageBytesAvg: 0,
		},
	}

	// Short term sees 2 days, min_data=1 → classifies as orphaned
	shortWindow := windowDigests(digests, shortTerm.WindowDays)
	recShort := computePVCRecommendation(shortWindow, "org123", "cluster-uuid", shortTerm)
	assert.Equal(t, PVCRecTypeOrphaned, recShort.RecommendationType)

	// Medium term with min_data=3 → insufficient data → healthy
	recMedium := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm)
	assert.Equal(t, PVCRecTypeHealthy, recMedium.RecommendationType)
}

func TestWindowDigests(t *testing.T) {
	digests := make([]PVCDigestRow, 30)
	for i := range digests {
		digests[i] = PVCDigestRow{
			BucketDate: time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
		}
	}
	// Latest is May 30. Cutoff for 7 days = May 30 - 7 = May 23.
	// Days >= May 23: May 23,24,25,26,27,28,29,30 = 8 days.
	w7 := windowDigests(digests, 7)
	assert.Equal(t, 8, len(w7))
	assert.Equal(t, time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC), w7[0].BucketDate)

	// Cutoff for 1 day = May 30 - 1 = May 29. Days >= May 29: May 29, May 30 = 2.
	w1 := windowDigests(digests, 1)
	assert.Equal(t, 2, len(w1))
	assert.Equal(t, time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC), w1[0].BucketDate)

	// Cutoff for 15 days = May 30 - 15 = May 15. Days >= May 15: May 15..30 = 16.
	w15 := windowDigests(digests, 15)
	assert.Equal(t, 16, len(w15))
}

func TestComputePVCGrowthSlope(t *testing.T) {
	// Perfect linear growth: 0, 10, 20, 30, 40 bytes per day
	digests := []PVCDigestRow{
		{UsageBytesAvg: 0},
		{UsageBytesAvg: 10},
		{UsageBytesAvg: 20},
		{UsageBytesAvg: 30},
		{UsageBytesAvg: 40},
	}

	slope := computePVCGrowthSlope(digests, 0)
	assert.InDelta(t, 10.0, slope, 0.001)
}

func TestComputePVCGrowthSlope_Flat(t *testing.T) {
	digests := []PVCDigestRow{
		{UsageBytesAvg: 100},
		{UsageBytesAvg: 100},
		{UsageBytesAvg: 100},
	}

	slope := computePVCGrowthSlope(digests, 0)
	assert.InDelta(t, 0.0, slope, 0.001)
}

func TestComputePVCGrowthSlope_InsufficientData(t *testing.T) {
	digests := []PVCDigestRow{{UsageBytesAvg: 50}}
	slope := computePVCGrowthSlope(digests, 0)
	assert.Equal(t, 0.0, slope)
}

func TestComputePVCGrowthSlope_WeightedLeastSquares(t *testing.T) {
	// Data with a spike early (day 0) and steady growth (10/day) for 14 days.
	// The spike is large enough to pull OLS negative, but WLS with a short
	// halflife should effectively ignore it and see only the recent trend.
	digests := make([]PVCDigestRow, 15)
	digests[0] = PVCDigestRow{UsageBytesAvg: 5000} // spike on oldest day
	for i := 1; i < 15; i++ {
		digests[i] = PVCDigestRow{UsageBytesAvg: int64(100 + (i-1)*10)}
	}

	slopeOLS := computePVCGrowthSlope(digests, 0)
	slopeWLS := computePVCGrowthSlope(digests, 24) // 1-day halflife

	// OLS is pulled negative by day-0 (5000 at x=0 vs ~230 at x=14).
	assert.True(t, slopeOLS < 0, "OLS slope should be negative due to day-0 spike: %f", slopeOLS)
	// WLS with 1-day halflife: day-0 weight ≈ exp(-ln2*14*24/24) ≈ 6e-5, effectively zero.
	assert.True(t, slopeWLS > 0, "WLS slope should be positive, emphasizing recent trend: %f", slopeWLS)
	assert.InDelta(t, 10.0, slopeWLS, 2.0, "WLS slope should be close to actual recent trend of 10/day")
}

func TestComputePVCGrowthSlope_WLS_EqualsOLS_WhenHalflifeVeryLarge(t *testing.T) {
	digests := []PVCDigestRow{
		{UsageBytesAvg: 10},
		{UsageBytesAvg: 20},
		{UsageBytesAvg: 30},
		{UsageBytesAvg: 40},
	}

	slopeOLS := computePVCGrowthSlope(digests, 0)
	slopeWLS := computePVCGrowthSlope(digests, 1e9) // huge halflife → all weights ~equal
	assert.InDelta(t, slopeOLS, slopeWLS, 0.01)
}
