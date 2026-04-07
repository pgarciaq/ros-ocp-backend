package engine

import (
	"context"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecommendAllWorkloads_SingleContainer(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedDigestSeries(t, pool, 7, 200, 10, 524288, 1024)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)

	// 3 terms x 2 engines = 6 results for one container
	require.Len(t, results, 6)

	for _, r := range results {
		assert.Equal(t, testutil.TestOrgID, r.OrgID)
		assert.Equal(t, testutil.TestClusterUUID, r.ClusterUUID)
		assert.Equal(t, testutil.TestNamespace, r.Namespace)
		assert.Equal(t, testutil.TestWorkload, r.Workload)
		assert.Equal(t, testutil.TestContainer, r.ContainerName)
		assert.True(t, r.RecCPURequestMC > 0, "CPU request should be positive")
		assert.True(t, r.RecMemRequestKiB > 0, "memory request should be positive")
		assert.True(t, r.RecCPULimitMC >= r.RecCPURequestMC, "CPU limit >= request")
		assert.True(t, r.RecMemLimitKiB >= r.RecMemRequestKiB, "memory limit >= request")
	}

	var termsSeen = map[string]int{}
	for _, r := range results {
		termsSeen[r.Term+"/"+r.Engine]++
	}
	assert.Equal(t, 1, termsSeen["short/cost"])
	assert.Equal(t, 1, termsSeen["short/performance"])
	assert.Equal(t, 1, termsSeen["medium/cost"])
	assert.Equal(t, 1, termsSeen["medium/performance"])
	assert.Equal(t, 1, termsSeen["long/cost"])
	assert.Equal(t, 1, termsSeen["long/performance"])
}

func TestRecommendAllWorkloads_WritesToDB(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedDigestSeries(t, pool, 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)

	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	err = WriteRecommendations(ctx, pool, results)
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1`,
		testutil.TestOrgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 6, count)
}

func TestRecommendAllWorkloads_Empty_WhenNoDigests(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := RecommendAllWorkloads(ctx, pool, "org-empty", testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestRecommendAllWorkloads_InsufficientData_SkipsTerm(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Only 1 day of data: short (minData=1) will produce results,
	// medium (minData=3) and long (minData=7) should be skipped.
	testutil.SeedDigestSeries(t, pool, 1, 200, 0, 524288, 0)
	end := testutil.BaseDate

	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)

	// Should only have short/cost + short/performance = 2
	require.Len(t, results, 2)
	for _, r := range results {
		assert.Equal(t, "short", r.Term)
	}
}

func TestRecommendAllWorkloads_TwoContainers(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedDigestSeries(t, pool, 7, 200, 10, 524288, 1024)

	// Seed a second container
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       testutil.BaseDate.AddDate(0, 0, i),
			OrgID:            testutil.TestOrgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        testutil.TestNamespace,
			Workload:         testutil.TestWorkload,
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    "sidecar",
			CPURequestP50MC:  50,
			CPURequestP95MC:  60,
			CPUUsageP50MC:    40,
			CPUUsageP95MC:    55,
			CPUUsageP98MC:    58,
			CPUUsageP99MC:    59,
			CPUUsageMaxMC:    65,
			CPUThrottleP95MC: 2,
			CPUThrottleMaxMC: 5,
			MemRequestP50KiB: 102400,
			MemRequestP95KiB: 112640,
			MemUsageP50KiB:   100000,
			MemUsageP95KiB:   110000,
			MemUsageMaxKiB:   115000,
			MemRSSP95KiB:     108000,
			MemRSSMaxKiB:     113000,
			OOMCountSum:      0,
			CPUUsageMeanMC:   45,
			MemUsageMeanKiB:  105000,
			SampleCount:      96,
		})
	}

	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)

	// 2 containers x 3 terms x 2 engines = 12
	require.Len(t, results, 12)

	containers := map[string]int{}
	for _, r := range results {
		containers[r.ContainerName]++
	}
	assert.Equal(t, 6, containers["main"])
	assert.Equal(t, 6, containers["sidecar"])
}

func TestWriteRecommendations_Upsert(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	rec := ContainerRec{
		OrgID:            testutil.TestOrgID,
		ClusterUUID:      testutil.TestClusterUUID,
		Namespace:        testutil.TestNamespace,
		Workload:         testutil.TestWorkload,
		WorkloadType:     testutil.TestWorkloadType,
		ContainerName:    testutil.TestContainer,
		Term:             "short",
		Engine:           "cost",
		RecCPURequestMC:  100,
		RecCPULimitMC:    110,
		RecMemRequestKiB: 51200,
		RecMemLimitKiB:   53760,
	}

	err := WriteRecommendations(ctx, pool, []ContainerRec{rec})
	require.NoError(t, err)

	// Update the same row
	rec.RecCPURequestMC = 200
	err = WriteRecommendations(ctx, pool, []ContainerRec{rec})
	require.NoError(t, err)

	var cpuReq int64
	err = pool.QueryRow(ctx,
		`SELECT rec_cpu_request_millicores FROM recommendation_sets
		 WHERE org_id=$1 AND cluster_uuid=$2 AND namespace=$3 AND workload=$4
		   AND container_name=$5 AND term=$6 AND engine=$7`,
		rec.OrgID, rec.ClusterUUID, rec.Namespace, rec.Workload,
		rec.ContainerName, rec.Term, rec.Engine).Scan(&cpuReq)
	require.NoError(t, err)
	assert.Equal(t, int64(200), cpuReq)

	var count int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM recommendation_sets WHERE org_id=$1`, rec.OrgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestRecommendAllWorkloads_ConfidenceLevel(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedDigestSeries(t, pool, 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)

	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)

	for _, r := range results {
		assert.True(t, r.ConfidenceLevel >= 0 && r.ConfidenceLevel <= 1.0,
			"confidence should be 0-1, got %f for %s/%s", r.ConfidenceLevel, r.Term, r.Engine)
	}

	// Short term with 7 data days (minData=1) should have high confidence
	for _, r := range results {
		if r.Term == "short" {
			assert.True(t, r.ConfidenceLevel > 0.5, "short with 7 days should have high confidence")
		}
	}
}

func TestComputeConfidence(t *testing.T) {
	tests := []struct {
		name     string
		dataDays int
		minData  int
		window   int
		wantMin  float32
		wantMax  float32
	}{
		{"at minimum", 3, 3, 7, 0.3, 0.5},
		{"full window", 7, 3, 7, 0.9, 1.0},
		{"exceeds window", 10, 3, 7, 0.9, 1.0},
		{"below minimum", 1, 3, 7, 0.0, 0.2},
		{"zero data", 0, 3, 7, 0.0, 0.01},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeConfidence(tt.dataDays, tt.minData, tt.window)
			assert.True(t, got >= tt.wantMin && got <= tt.wantMax,
				"confidence=%f expected between %f and %f", got, tt.wantMin, tt.wantMax)
		})
	}
}

func TestComputeVariation(t *testing.T) {
	tests := []struct {
		name     string
		current  int64
		rec      int64
		wantSign float32
	}{
		{"no change", 100, 100, 0},
		{"increase 50%", 100, 150, 50},
		{"decrease 50%", 200, 100, -50},
		{"current zero", 0, 100, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeVariation(tt.current, tt.rec)
			if tt.current == 0 {
				assert.Equal(t, float32(0), got)
			} else {
				assert.InDelta(t, float64(tt.wantSign), float64(got), 0.1)
			}
		})
	}
}

func TestRecommendAllWorkloads_PopulatesCurrentValues(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	cpuReqMC := int64(200)
	memReqKiB := int64(524288)
	testutil.SeedDigestSeries(t, pool, 7, cpuReqMC, 10, memReqKiB, 1024)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	for _, r := range results {
		assert.True(t, r.CurrentCPURequestMC > 0, "current CPU request should be populated")
		assert.True(t, r.CurrentMemRequestKiB > 0, "current mem request should be populated")
	}
}

func TestRecommendAllWorkloads_VariationIsComputed(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedDigestSeries(t, pool, 7, 200, 10, 524288, 1024)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)

	for _, r := range results {
		if r.CurrentCPURequestMC > 0 {
			assert.NotEqual(t, float32(0), r.VariationCPURequestPct,
				"variation should be non-zero when current != recommended")
		}
	}
}

func TestRecommendAllWorkloads_StaleDetection(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Seed data from 10 days ago -- well within staleness threshold when
	// end is also old, but the check is now() vs latest digest, not end.
	testutil.SeedDigestSeries(t, pool, 3, 200, 10, 524288, 1024)

	end := testutil.BaseDate.AddDate(0, 0, 2)
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)

	// BaseDate is first-of-current-month. If it's >3 days ago, results should be stale.
	// If today is day 1-3 of the month, they won't be stale. Either way, verify consistency.
	if len(results) > 0 {
		daysSinceLatest := time.Since(end.Truncate(24 * time.Hour))
		if daysSinceLatest > stalenessThreshold {
			for _, r := range results {
				assert.True(t, r.Stale, "recommendations from old data should be stale")
			}
		} else {
			for _, r := range results {
				assert.False(t, r.Stale, "recent data should not be stale")
			}
		}
	}
}

func TestLatestDigest(t *testing.T) {
	d1 := DigestRow{BucketDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), CPURequestP50MC: 100}
	d2 := DigestRow{BucketDate: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), CPURequestP50MC: 200}
	d3 := DigestRow{BucketDate: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), CPURequestP50MC: 150}

	got := latestDigest([]DigestRow{d1, d2, d3})
	assert.Equal(t, d2.CPURequestP50MC, got.CPURequestP50MC)
	assert.Equal(t, d2.BucketDate, got.BucketDate)
}

func TestLatestDigest_Empty(t *testing.T) {
	got := latestDigest(nil)
	assert.True(t, got.BucketDate.IsZero())
}
