package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
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

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultPVCThresholdSettings())

	assert.Equal(t, PVCRecTypeOrphaned, rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, NotifPVCOrphaned)
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
			CapacityBytes: 100 << 30, // 100 GiB
			UsageBytesMin: 5 << 30,   // 5 GiB
			UsageBytesMax: 10 << 30,  // 10 GiB (10% usage)
			UsageBytesAvg: 7 << 30,
			SampleCount:   24,
		}
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultPVCThresholdSettings())

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

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultPVCThresholdSettings())

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

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultPVCThresholdSettings())

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

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultPVCThresholdSettings())

	assert.InDelta(t, float64(1<<30), float64(rec.GrowthBytesPerDay), float64(1<<28))
	assert.NotNil(t, rec.DaysToFull)
	assert.InDelta(t, 31, *rec.DaysToFull, 5)
	assert.Equal(t, "medium", rec.Term)
}

func TestComputePVCRecommendation_InsufficientData(t *testing.T) {
	// Only 1 day of zero usage — below min classify threshold (2 days)
	digests := []PVCDigestRow{
		{
			BucketDate:    time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Namespace:     "test",
			PVC:           "new-pvc",
			CapacityBytes: 10 << 30,
			UsageBytesMax: 0,
		},
	}

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultPVCThresholdSettings())

	assert.Equal(t, PVCRecTypeHealthy, rec.RecommendationType)
	assert.InDelta(t, 1.0/3.0, float64(rec.ConfidenceLevel), 0.01)
}

func TestComputePVCRecommendation_LowConfidenceOrphaned(t *testing.T) {
	mediumTerm := TermConfig{Name: "medium", WindowDays: 30, MinDataDays: 14, DecayHalfLifeHours: 0}
	// 2 days of zero usage — classifies as orphaned with proportional confidence (2/14)
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

	rec := computePVCRecommendation(digests, "org123", "cluster-uuid", mediumTerm, DefaultPVCThresholdSettings())

	assert.Equal(t, PVCRecTypeOrphaned, rec.RecommendationType)
	assert.InDelta(t, 2.0/14.0, float64(rec.ConfidenceLevel), 0.01)
	assert.Contains(t, rec.NotificationCodes, NotifLowConfidence)
}

func TestEvaluatePVCNotifications_SparseData(t *testing.T) {
	th := NotificationThresholdsFromSizing(defaultContainerSizingThresholds)
	rec := PVCRec{DataDays: 1, ConfidenceLevel: 1.0}
	codes := EvaluatePVCNotifications(rec, th)
	assert.Contains(t, codes, NotifSparseData)
}

func TestEvaluatePVCNotifications_SparseData_ExactThreshold(t *testing.T) {
	th := NotificationThresholdsFromSizing(defaultContainerSizingThresholds)
	rec := PVCRec{DataDays: 2, ConfidenceLevel: 1.0}
	codes := EvaluatePVCNotifications(rec, th)
	assert.Contains(t, codes, NotifSparseData, "data_days == threshold should fire")
}

func TestEvaluatePVCNotifications_SparseData_AboveThreshold(t *testing.T) {
	th := NotificationThresholdsFromSizing(defaultContainerSizingThresholds)
	rec := PVCRec{DataDays: 3, ConfidenceLevel: 1.0}
	codes := EvaluatePVCNotifications(rec, th)
	assert.NotContains(t, codes, NotifSparseData, "data_days above threshold should not fire")
}

func TestEvaluatePVCNotifications_SparseData_ZeroDays(t *testing.T) {
	th := NotificationThresholdsFromSizing(defaultContainerSizingThresholds)
	rec := PVCRec{DataDays: 0, ConfidenceLevel: 0.0}
	codes := EvaluatePVCNotifications(rec, th)
	assert.NotContains(t, codes, NotifSparseData, "zero data days should not fire SPARSE_DATA")
}

func TestEvaluatePVCNotifications_SparseData_OrthogonalToLowConfidence(t *testing.T) {
	th := NotificationThresholdsFromSizing(defaultContainerSizingThresholds)
	rec := PVCRec{DataDays: 1, ConfidenceLevel: 1.0}
	codes := EvaluatePVCNotifications(rec, th)
	assert.Contains(t, codes, NotifSparseData, "sparse data should fire even with high confidence")
	assert.NotContains(t, codes, NotifLowConfidence, "low confidence should NOT fire with confidence=1.0")
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
	recShort := computePVCRecommendation(shortWindow, "org123", "cluster-uuid", shortTerm, DefaultPVCThresholdSettings())
	assert.Equal(t, PVCRecTypeNearFull, recShort.RecommendationType)
	assert.Equal(t, "short", recShort.Term)

	// Long term: sees all 15 days — max is still 90 GiB (near_full),
	// but has growth trend data over the full window
	longWindow := windowDigests(digests, longTerm.WindowDays)
	recLong := computePVCRecommendation(longWindow, "org123", "cluster-uuid", longTerm, DefaultPVCThresholdSettings())
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
	recShort := computePVCRecommendation(shortWindow, "org123", "cluster-uuid", shortTerm, DefaultPVCThresholdSettings())
	assert.Equal(t, PVCRecTypeOrphaned, recShort.RecommendationType)

	// Medium term with min_data=3 but 2 days meets min classify threshold → orphaned with low confidence
	recMedium := computePVCRecommendation(digests, "org123", "cluster-uuid", defaultMediumTerm, DefaultPVCThresholdSettings())
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

func TestWritePVCRecommendations_BatchUpsert(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	rec := PVCRec{
		OrgID:              testutil.TestOrgID,
		ClusterUUID:        testutil.TestClusterUUID,
		Namespace:          "pvc-batch",
		PVC:                "data-vol",
		StorageClass:       "gp3",
		CapacityBytes:      100 << 30,
		UsageBytesMax:      10 << 30,
		UsageRatio:         0.10,
		RecommendationType: PVCRecTypeOversized,
		DataDays:           7,
		Term:               "medium",
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM pvc_recommendation_sets WHERE org_id = $1 AND namespace = $2`, rec.OrgID, rec.Namespace)
	})

	require.NoError(t, WritePVCRecommendations(ctx, pool, []PVCRec{rec}, []string{"medium"}))

	var usageRatio float64
	err := pool.QueryRow(ctx, `
		SELECT usage_ratio FROM pvc_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND persistentvolumeclaim = $4 AND term = $5`,
		rec.OrgID, rec.ClusterUUID, rec.Namespace, rec.PVC, rec.Term,
	).Scan(&usageRatio)
	require.NoError(t, err)
	assert.InDelta(t, 0.10, usageRatio, 0.001)

	rec.UsageRatio = 0.15
	rec.UsageBytesMax = 15 << 30
	require.NoError(t, WritePVCRecommendations(ctx, pool, []PVCRec{rec}, []string{"medium"}))

	var count int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM pvc_recommendation_sets
		WHERE org_id = $1 AND namespace = $2 AND persistentvolumeclaim = $3`,
		rec.OrgID, rec.Namespace, rec.PVC,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "batch upsert should update, not duplicate")

	err = pool.QueryRow(ctx, `
		SELECT usage_ratio FROM pvc_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND persistentvolumeclaim = $4 AND term = $5`,
		rec.OrgID, rec.ClusterUUID, rec.Namespace, rec.PVC, rec.Term,
	).Scan(&usageRatio)
	require.NoError(t, err)
	assert.InDelta(t, 0.15, usageRatio, 0.001)
}

func TestWritePVCRecommendations_CleanupStaleTerms(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID
	namespace := "pvc-stale-terms"
	pvcName := "data-vol"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM pvc_recommendation_sets WHERE org_id = $1 AND namespace = $2`, orgID, namespace)
	})

	base := PVCRec{
		OrgID:              orgID,
		ClusterUUID:        clusterUUID,
		Namespace:          namespace,
		PVC:                pvcName,
		RecommendationType: PVCRecTypeHealthy,
		DataDays:           7,
	}
	short := base
	short.Term = "short"
	long := base
	long.Term = "long"

	require.NoError(t, WritePVCRecommendations(ctx, pool, []PVCRec{short, long}, []string{"short", "long"}))

	var termCount int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pvc_recommendation_sets
		WHERE org_id = $1 AND namespace = $2 AND persistentvolumeclaim = $3`,
		orgID, namespace, pvcName,
	).Scan(&termCount)
	require.NoError(t, err)
	assert.Equal(t, 2, termCount)

	require.NoError(t, WritePVCRecommendations(ctx, pool, []PVCRec{short}, []string{"short"}))

	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM pvc_recommendation_sets
		WHERE org_id = $1 AND namespace = $2 AND persistentvolumeclaim = $3`,
		orgID, namespace, pvcName,
	).Scan(&termCount)
	require.NoError(t, err)
	assert.Equal(t, 1, termCount, "stale term rows should be deleted after batch write")

	var remainingTerm string
	err = pool.QueryRow(ctx, `
		SELECT term FROM pvc_recommendation_sets
		WHERE org_id = $1 AND namespace = $2 AND persistentvolumeclaim = $3`,
		orgID, namespace, pvcName,
	).Scan(&remainingTerm)
	require.NoError(t, err)
	assert.Equal(t, "short", remainingTerm)
}
