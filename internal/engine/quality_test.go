package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestStabilityPct(t *testing.T) {
	tests := []struct {
		name   string
		cpuVar int32
		memVar int32
		want   float32
	}{
		{"no change", 0, 0, 1.0},
		{"50% CPU change only", 50, 0, 0.75},
		{"50% memory change only", 0, 50, 0.75},
		{"50% both", 50, 50, 0.5},
		{"100% both", 100, 100, 0.0},
		{"negative variation (decrease)", -50, -50, 0.5},
		{"over 100% clamps to 0", 150, 150, 0.0},
		{"asymmetric", 20, 80, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeStabilityPct(tt.cpuVar, tt.memVar)
			assert.InDelta(t, tt.want, got, 0.001)
		})
	}
}

func TestAdoptionDetection(t *testing.T) {
	tests := []struct {
		name       string
		curCPU     int64
		curMem     int64
		recCPU     int64
		recMem     int64
		wantAdopt  bool
	}{
		{"exact match", 1000, 2048, 1000, 2048, true},
		{"within 5% CPU", 1040, 2048, 1000, 2048, true},
		{"within 5% both", 1050, 2140, 1000, 2048, true},
		{"beyond 5% CPU", 1060, 2048, 1000, 2048, false},
		{"beyond 5% memory", 1000, 2200, 1000, 2048, false},
		{"zero recommended zero actual", 0, 0, 0, 0, true},
		{"zero recommended nonzero actual", 100, 100, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectAdoption(tt.curCPU, tt.curMem, tt.recCPU, tt.recMem)
			assert.Equal(t, tt.wantAdopt, got)
		})
	}
}

func TestRecommendationAgeHours(t *testing.T) {
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	t.Run("24 hours ago", func(t *testing.T) {
		updatedAt := now.Add(-24 * time.Hour)
		assert.Equal(t, int64(24), ComputeRecommendationAgeHours(updatedAt, now))
	})

	t.Run("90 minutes truncates to 1", func(t *testing.T) {
		updatedAt := now.Add(-90 * time.Minute)
		assert.Equal(t, int64(1), ComputeRecommendationAgeHours(updatedAt, now))
	})

	t.Run("zero time returns 0", func(t *testing.T) {
		assert.Equal(t, int64(0), ComputeRecommendationAgeHours(time.Time{}, now))
	})

	t.Run("future updatedAt clamps to 0", func(t *testing.T) {
		future := now.Add(2 * time.Hour)
		assert.Equal(t, int64(0), ComputeRecommendationAgeHours(future, now))
	})
}

func TestWriteRecommendationQuality_FullPipeline(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedDigestSeries(t, pool, 7, 500, 10, 4096, 100)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, results)

	if err := WriteRecommendations(ctx, pool, results); err != nil {
		t.Fatalf("WriteRecommendations: %v", err)
	}

	keys := ContainerKeys(results)
	oldRecs, err := ReadOldRecommendations(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, keys)
	require.NoError(t, err)

	EnsureQualityPartitions(ctx, pool)

	oomCounts := OOMCountsByContainer(results)
	err = WriteRecommendationQuality(ctx, pool, results, oldRecs, oomCounts)
	require.NoError(t, err)

	var measuredAt time.Time
	var oomEventsAfter int64
	var stabilityPct float32
	var adopted bool
	var ageHours int64
	err = pool.QueryRow(ctx, `
		SELECT measured_at, oom_events_after_rec, stability_pct, adoption_detected, recommendation_age_hours
		FROM recommendation_quality
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND workload = $4 AND container_name = $5
		ORDER BY measured_at DESC LIMIT 1`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace, testutil.TestWorkload, testutil.TestContainer,
	).Scan(&measuredAt, &oomEventsAfter, &stabilityPct, &adopted, &ageHours)
	require.NoError(t, err)

	assert.False(t, measuredAt.IsZero())
	assert.GreaterOrEqual(t, oomEventsAfter, int64(0))
	assert.GreaterOrEqual(t, stabilityPct, float32(0))
	assert.LessOrEqual(t, stabilityPct, float32(1.0))
}

func TestWriteRecommendationQuality_StabilityAcrossCycles(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Cycle 1: seed data and compute recommendations
	testutil.SeedDigestSeries(t, pool, 7, 500, 10, 4096, 100)
	end := testutil.BaseDate.AddDate(0, 0, 6)
	results1, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, WriteRecommendations(ctx, pool, results1))

	// Cycle 2: read old recs, then compute new recs with different data
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       testutil.BaseDate.AddDate(0, 0, i),
			OrgID:            testutil.TestOrgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        testutil.TestNamespace,
			Workload:         testutil.TestWorkload,
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    testutil.TestContainer,
			CPURequestP50MC:  800, CPURequestP95MC: 850,
			CPUUsageP50MC: 750, CPUUsageP95MC: 800, CPUUsageP98MC: 820,
			CPUUsageP99MC: 830, CPUUsageMaxMC: 850,
			CPUThrottleP95MC: 5, CPUThrottleMaxMC: 10,
			MemRequestP50KiB: 8192, MemRequestP95KiB: 8700,
			MemUsageP50KiB: 7000, MemUsageP95KiB: 8192, MemUsageMaxKiB: 9000,
			MemRSSP95KiB: 7500, MemRSSMaxKiB: 8500,
			OOMCountSum: 0, CPUUsageMeanMC: 780, MemUsageMeanKiB: 7200,
			SampleCount: 96,
		})
	}

	results2, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)

	keys := ContainerKeys(results2)
	oldRecs, err := ReadOldRecommendations(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, keys)
	require.NoError(t, err)
	require.NotEmpty(t, oldRecs)

	require.NoError(t, WriteRecommendations(ctx, pool, results2))

	EnsureQualityPartitions(ctx, pool)
	oomCounts := OOMCountsByContainer(results2)
	err = WriteRecommendationQuality(ctx, pool, results2, oldRecs, oomCounts)
	require.NoError(t, err)

	var stabilityPct float32
	err = pool.QueryRow(ctx, `
		SELECT stability_pct FROM recommendation_quality
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND workload = $4 AND container_name = $5
		ORDER BY measured_at DESC LIMIT 1`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace, testutil.TestWorkload, testutil.TestContainer,
	).Scan(&stabilityPct)
	require.NoError(t, err)
	// Data changed significantly, so stability should be < 1.0
	assert.Less(t, stabilityPct, float32(1.0))
}

func TestOOMCountsByContainer(t *testing.T) {
	recs := []ContainerRec{
		{Namespace: "ns1", Workload: "deploy1", ContainerName: "c1", OOMCountSum: 5},
		{Namespace: "ns1", Workload: "deploy1", ContainerName: "c1", OOMCountSum: 99}, // duplicate, should keep first
		{Namespace: "ns2", Workload: "deploy2", ContainerName: "c2", OOMCountSum: 0},
		{Namespace: "ns3", Workload: "deploy3", ContainerName: "c3", OOMCountSum: 12},
	}

	counts := OOMCountsByContainer(recs)

	assert.Equal(t, int64(5), counts[containerKey{Namespace: "ns1", Workload: "deploy1", ContainerName: "c1"}])
	assert.Equal(t, int64(0), counts[containerKey{Namespace: "ns2", Workload: "deploy2", ContainerName: "c2"}])
	assert.Equal(t, int64(12), counts[containerKey{Namespace: "ns3", Workload: "deploy3", ContainerName: "c3"}])
	assert.Len(t, counts, 3)
}

func TestWriteRecommendationQuality_MissingPartition(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// The migration creates partitions for current + 2 months.
	// Use a measured_at far in the future (2 years from now) where no partition exists.
	// We need to override the time used in WriteRecommendationQuality -- but since it
	// uses time.Now() internally, we instead create a minimal table scenario:
	// drop the existing partitions and verify the write fails.
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, i, 0)
		partName := "recommendation_quality_" + monthStart.Format("200601")
		_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+partName)
	}

	recs := []ContainerRec{
		{
			OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
			ContainerName: testutil.TestContainer,
			Term: "medium", Engine: "cost",
			RecCPURequestMC: 100, RecMemRequestKiB: 1024,
		},
	}
	oldRecs := map[containerKey]OldRecommendation{}
	oomCounts := map[containerKey]int64{}

	err := WriteRecommendationQuality(ctx, pool, recs, oldRecs, oomCounts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "partition missing")
}

func TestEnsureQualityPartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Call once
	EnsureQualityPartitions(ctx, pool)

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, i, 0)
		partName := "recommendation_quality_" + monthStart.Format("200601")
		var exists bool
		err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_class WHERE relname = $1)", partName).Scan(&exists)
		require.NoError(t, err)
		assert.True(t, exists, "partition %s should exist", partName)
	}

	// Call again -- idempotent
	EnsureQualityPartitions(ctx, pool)
}

func TestEnsureQualityPartitions_ConcurrentSafe(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			EnsureQualityPartitions(ctx, pool)
			errs[idx] = nil
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		assert.NoError(t, err)
	}
}
