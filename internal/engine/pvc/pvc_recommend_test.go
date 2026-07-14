package pvc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

var defaultMediumTerm = core.TermConfig{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168}

var testNotifThresholds = core.NotificationThresholds{
	LowConfidenceThreshold: 0.30,
	SparseDataThreshold:    2,
	MemTrendSlopeThreshold: 0.0,
}

func TestComputePVCRecommendation_Orphaned(t *testing.T) {
	digests := make([]PVCDigestRow, 5)
	for i := range digests {
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "production",
			PVC:           "old-data-pvc",
			PV:            "pv-001",
			StorageClass:  "gp3",
			CapacityBytes: 100 << 30,
			UsageBytesMin: 0,
			UsageBytesMax: 0,
			UsageBytesAvg: 0,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultThresholdSettings(), testNotifThresholds)

	assert.Equal(t, PVCRecTypeOrphaned, rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, core.NotifPVCOrphaned)
	require.NotNil(t, rec.IdleSince)
	assert.Equal(t, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), rec.IdleSince.UTC())
	assert.Greater(t, rec.IdleDurationDays, 0)
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
			CapacityBytes: 100 << 30,
			UsageBytesMin: 5 << 30,
			UsageBytesMax: 10 << 30,
			UsageBytesAvg: 7 << 30,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultThresholdSettings(), testNotifThresholds)

	assert.Equal(t, PVCRecTypeOversized, rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, core.NotifPVCOversized)
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
			CapacityBytes: 50 << 30,
			UsageBytesMin: 40 << 30,
			UsageBytesMax: 45 << 30,
			UsageBytesAvg: 42 << 30,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultThresholdSettings(), testNotifThresholds)

	assert.Equal(t, PVCRecTypeNearFull, rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, core.NotifPVCNearFull)
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
			CapacityBytes: 100 << 30,
			UsageBytesMin: 30 << 30,
			UsageBytesMax: 50 << 30,
			UsageBytesAvg: 40 << 30,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultThresholdSettings(), testNotifThresholds)

	assert.Equal(t, PVCRecTypeHealthy, rec.RecommendationType)
	assert.Empty(t, rec.NotificationCodes)
	assert.Equal(t, "medium", rec.Term)
}

func TestComputePVCRecommendation_GrowthTrend(t *testing.T) {
	digests := make([]PVCDigestRow, 10)
	for i := range digests {
		usage := int64((10 + i) << 30)
		digests[i] = PVCDigestRow{
			BucketDate:    time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
			Namespace:     "production",
			PVC:           "growing-pvc",
			PV:            "pv-005",
			StorageClass:  "gp3",
			CapacityBytes: 50 << 30,
			UsageBytesMin: usage - (1 << 29),
			UsageBytesMax: usage,
			UsageBytesAvg: usage,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultThresholdSettings(), testNotifThresholds)

	assert.InDelta(t, float64(1<<30), float64(rec.GrowthBytesPerDay), float64(1<<28))
	assert.NotNil(t, rec.DaysToFull)
	assert.InDelta(t, 31, *rec.DaysToFull, 5)
	assert.Equal(t, "medium", rec.Term)
}

func TestComputePVCRecommendation_InsufficientData(t *testing.T) {
	digests := []PVCDigestRow{
		{
			BucketDate:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Namespace:     "test",
			PVC:           "new-pvc",
			CapacityBytes: 10 << 30,
			UsageBytesMax: 0,
		},
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultThresholdSettings(), testNotifThresholds)

	assert.Equal(t, PVCRecTypeHealthy, rec.RecommendationType)
	assert.InDelta(t, 1.0/3.0, float64(rec.ConfidenceLevel), 0.01)
}

func TestComputePVCRecommendation_LowConfidenceOrphaned(t *testing.T) {
	mediumTerm := core.TermConfig{Name: "medium", WindowDays: 30, MinDataDays: 14, DecayHalfLifeHours: 0}
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

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", mediumTerm, DefaultThresholdSettings(), testNotifThresholds)

	assert.Equal(t, PVCRecTypeOrphaned, rec.RecommendationType)
	assert.InDelta(t, 2.0/14.0, float64(rec.ConfidenceLevel), 0.01)
	assert.Contains(t, rec.NotificationCodes, core.NotifLowConfidence)
}

func TestEvaluatePVCNotifications_SparseData(t *testing.T) {
	th := testNotifThresholds
	rec := PVCRec{DataDays: 1, ConfidenceLevel: 1.0}
	codes := EvaluatePVCNotifications(rec, th)
	assert.Contains(t, codes, core.NotifSparseData)
}

func TestEvaluatePVCNotifications_SparseData_ExactThreshold(t *testing.T) {
	th := testNotifThresholds
	rec := PVCRec{DataDays: 2, ConfidenceLevel: 1.0}
	codes := EvaluatePVCNotifications(rec, th)
	assert.Contains(t, codes, core.NotifSparseData, "data_days == threshold should fire")
}

func TestEvaluatePVCNotifications_SparseData_AboveThreshold(t *testing.T) {
	th := testNotifThresholds
	rec := PVCRec{DataDays: 3, ConfidenceLevel: 1.0}
	codes := EvaluatePVCNotifications(rec, th)
	assert.NotContains(t, codes, core.NotifSparseData, "data_days above threshold should not fire")
}

func TestEvaluatePVCNotifications_SparseData_ZeroDays(t *testing.T) {
	th := testNotifThresholds
	rec := PVCRec{DataDays: 0, ConfidenceLevel: 0.0}
	codes := EvaluatePVCNotifications(rec, th)
	assert.NotContains(t, codes, core.NotifSparseData, "zero data days should not fire SPARSE_DATA")
}

func TestEvaluatePVCNotifications_SparseData_OrthogonalToLowConfidence(t *testing.T) {
	th := testNotifThresholds
	rec := PVCRec{DataDays: 1, ConfidenceLevel: 1.0}
	codes := EvaluatePVCNotifications(rec, th)
	assert.Contains(t, codes, core.NotifSparseData, "sparse data should fire even with high confidence")
	assert.NotContains(t, codes, core.NotifLowConfidence, "low confidence should NOT fire with confidence=1.0")
}

func TestComputePVCRecommendation_ShortTermSeesBurst(t *testing.T) {
	shortTerm := core.TermConfig{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0}
	longTerm := core.TermConfig{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360}

	digests := make([]PVCDigestRow, 15)
	for i := range digests {
		usage := int64(30 << 30)
		if i == 14 {
			usage = int64(90 << 30)
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

	shortWindow := windowDigests(digests, shortTerm.WindowDays)
	recShort := computePVCRecommendation(shortWindow, "org123", "cluster-uuid", shortTerm, DefaultThresholdSettings(), testNotifThresholds)
	assert.Equal(t, PVCRecTypeNearFull, recShort.RecommendationType)
	assert.Equal(t, "short", recShort.Term)

	longWindow := windowDigests(digests, longTerm.WindowDays)
	recLong := computePVCRecommendation(longWindow, "org123", "cluster-uuid", longTerm, DefaultThresholdSettings(), testNotifThresholds)
	assert.Equal(t, PVCRecTypeNearFull, recLong.RecommendationType)
	assert.Equal(t, "long", recLong.Term)
	assert.True(t, recLong.DataDays >= 15)
}

func TestComputePVCRecommendation_ShortTermInsufficientButLongTermClassifies(t *testing.T) {
	shortTerm := core.TermConfig{Name: "short", WindowDays: 2, MinDataDays: 1, DecayHalfLifeHours: 0}

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

	shortWindow := windowDigests(digests, shortTerm.WindowDays)
	recShort := computePVCRecommendation(shortWindow, "org123", "cluster-uuid", shortTerm, DefaultThresholdSettings(), testNotifThresholds)
	assert.Equal(t, PVCRecTypeOrphaned, recShort.RecommendationType)

	recMedium := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultThresholdSettings(), testNotifThresholds)
	assert.Equal(t, PVCRecTypeOrphaned, recMedium.RecommendationType)
	assert.InDelta(t, 2.0/3.0, float64(recMedium.ConfidenceLevel), 0.01)
}

func TestWindowDigests(t *testing.T) {
	digests := make([]PVCDigestRow, 30)
	for i := range digests {
		digests[i] = PVCDigestRow{
			BucketDate: time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
		}
	}
	w7 := windowDigests(digests, 7)
	assert.Equal(t, 8, len(w7))
	assert.Equal(t, time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC), w7[0].BucketDate)

	w1 := windowDigests(digests, 1)
	assert.Equal(t, 2, len(w1))
	assert.Equal(t, time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC), w1[0].BucketDate)

	w15 := windowDigests(digests, 15)
	assert.Equal(t, 16, len(w15))
}

func TestComputePVCGrowthSlope(t *testing.T) {
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
	digests := make([]PVCDigestRow, 15)
	digests[0] = PVCDigestRow{UsageBytesAvg: 5000}
	for i := 1; i < 15; i++ {
		digests[i] = PVCDigestRow{UsageBytesAvg: int64(100 + (i-1)*10)}
	}

	slopeOLS := computePVCGrowthSlope(digests, 0)
	slopeWLS := computePVCGrowthSlope(digests, 24)

	assert.True(t, slopeOLS < 0, "OLS slope should be negative due to day-0 spike: %f", slopeOLS)
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
	slopeWLS := computePVCGrowthSlope(digests, 100_000)
	assert.InDelta(t, slopeOLS, slopeWLS, 0.01)
}

