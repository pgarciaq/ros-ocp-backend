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
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
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

	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
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
	results, err := RecommendAllWorkloads(ctx, pool, "org-empty", testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
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

	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
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
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
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

	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
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
		name    string
		current int64
		rec     int64
		want    int32
	}{
		{"no change", 100, 100, 0},
		{"increase 50%", 100, 150, 50},
		{"decrease 50%", 200, 100, -50},
		{"current zero", 0, 100, 0},
		{"rounds up", 3, 4, 33},
		{"rounds down", 3, 5, 67},
		{"negative rounds toward zero", 3, 2, -33},
		{"negative rounds away from zero", 6, 5, -17},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeVariation(tt.current, tt.rec)
			assert.Equal(t, tt.want, got)
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
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
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
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)

	for _, r := range results {
		if r.CurrentCPURequestMC > 0 {
			assert.NotEqual(t, int32(0), r.VariationCPURequestPct,
				"CPU request variation should be non-zero when current != recommended")
			assert.NotEqual(t, int32(0), r.VariationCPULimitPct,
				"CPU limit variation should be non-zero when current != recommended")
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
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)

	// BaseDate is 7 days ago. With only 3 days seeded (offset 0-2), the latest
	// digest is 5 days old, which exceeds the 3-day staleness threshold.
	if len(results) > 0 {
		daysSinceLatest := time.Since(end.Truncate(24 * time.Hour))
		if daysSinceLatest > StalenessThreshold() {
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

func TestIsStaleRecommendation_ClusterLastReportedTakesPrecedence(t *testing.T) {
	threshold := 48 * time.Hour
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	oldDigest := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	recentReport := time.Date(2026, 5, 23, 2, 0, 0, 0, time.UTC)

	assert.False(t, isStaleRecommendation(now, oldDigest, recentReport, threshold),
		"fresh cluster report should keep recommendations non-stale despite old digests")
	assert.True(t, isStaleRecommendation(now, oldDigest, time.Time{}, threshold),
		"without cluster report, stale follows digest age")
	assert.True(t, isStaleRecommendation(now, oldDigest, now.Add(-96*time.Hour), threshold),
		"stale when cluster has not reported within threshold")
}

func TestRecommendAllWorkloads_StaleDetection_RecentClusterReport(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'reship-cluster', 'src-reship', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	// Historical digests ending 5 days ago (older than default 48h threshold).
	testutil.SeedDigestSeries(t, pool, 3, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 2)
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	for _, r := range results {
		assert.False(t, r.Stale, "reship with fresh last_reported_at should not mark recommendations stale")
	}
}

func TestRecommendAllWorkloads_TermWindowScoping(t *testing.T) {
	// T-1.8: Verify that short (1 day), medium (7 days), and long (15 days)
	// actually use only their respective data windows.
	// Strategy: seed 15 days with strongly increasing CPU usage.
	// Short-term (window=1) sees only the last day (high CPU), producing
	// a larger recommendation than long-term (window=15) which averages
	// over the full range including the low early-day values.
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Seed 15 days: CPU starts at 100mc, increases by 100mc/day → day 15 = 1500mc.
	for i := 0; i < 15; i++ {
		cpuVal := int64(100) + int64(i)*100
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:      testutil.BaseDate.AddDate(0, 0, i),
			OrgID:           testutil.TestOrgID,
			ClusterUUID:     testutil.TestClusterUUID,
			Namespace:       testutil.TestNamespace,
			Workload:        testutil.TestWorkload,
			WorkloadType:    testutil.TestWorkloadType,
			ContainerName:   testutil.TestContainer,
			CPURequestP50MC: cpuVal, CPURequestP95MC: cpuVal + 50,
			CPUUsageP50MC: cpuVal - 20, CPUUsageP95MC: cpuVal,
			CPUUsageP98MC: cpuVal + 10, CPUUsageP99MC: cpuVal + 15,
			CPUUsageMaxMC:    cpuVal + 30,
			CPUThrottleP95MC: 5, CPUThrottleMaxMC: 10,
			MemRequestP50KiB: 524288, MemRequestP95KiB: 524800,
			MemUsageP50KiB: 524000, MemUsageP95KiB: 524288,
			MemUsageMaxKiB: 525312,
			MemRSSP95KiB:   524000, MemRSSMaxKiB: 525000,
			OOMCountSum: 0, CPUUsageMeanMC: cpuVal - 10,
			MemUsageMeanKiB: 523000, SampleCount: 96,
		})
	}

	end := testutil.BaseDate.AddDate(0, 0, 14) // day index 14 = 15th day
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, results)

	// Use performance engine (P98) since the test fixtures populate P98
	// but not P60 (which the cost engine uses).
	termCPU := map[string]int64{}
	for _, r := range results {
		if r.Engine == "performance" {
			termCPU[r.Term] = r.RecCPURequestMC
		}
	}

	shortCPU, hasShort := termCPU["short"]
	longCPU, hasLong := termCPU["long"]
	require.True(t, hasShort, "should have short-term performance results")
	require.True(t, hasLong, "should have long-term performance results")

	assert.Greater(t, shortCPU, longCPU,
		"short-term (1 day window, sees only latest high CPU=%d) should recommend more than "+
			"long-term (15 day window, averages in low early days CPU=%d)", shortCPU, longCPU)
}

func TestWriteRecommendations_PKAllowsTermEngineCoexistence(t *testing.T) {
	// T-2.5: The composite PK (org_id, cluster_uuid, namespace, workload,
	// container_name, term, engine) must allow the same container to have
	// rows for different term/engine combinations simultaneously.
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	base := ContainerRec{
		OrgID:            testutil.TestOrgID,
		ClusterUUID:      testutil.TestClusterUUID,
		Namespace:        testutil.TestNamespace,
		Workload:         testutil.TestWorkload,
		WorkloadType:     testutil.TestWorkloadType,
		ContainerName:    testutil.TestContainer,
		RecCPURequestMC:  100,
		RecCPULimitMC:    105,
		RecMemRequestKiB: 51200,
		RecMemLimitKiB:   53760,
	}

	var recs []ContainerRec
	for _, term := range []string{"short", "medium", "long"} {
		for _, eng := range []string{"cost", "performance"} {
			r := base
			r.Term = term
			r.Engine = eng
			recs = append(recs, r)
		}
	}

	err := WriteRecommendations(ctx, pool, recs)
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1 AND container_name = $2`,
		testutil.TestOrgID, testutil.TestContainer).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 6, count, "6 rows (3 terms × 2 engines) must coexist for the same container")

	// Verify specific combinations exist
	for _, term := range []string{"short", "medium", "long"} {
		for _, eng := range []string{"cost", "performance"} {
			var exists bool
			err = pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM recommendation_sets
				 WHERE org_id=$1 AND container_name=$2 AND term=$3 AND engine=$4)`,
				testutil.TestOrgID, testutil.TestContainer, term, eng).Scan(&exists)
			require.NoError(t, err)
			assert.True(t, exists, "row for %s/%s should exist", term, eng)
		}
	}
}

func TestRecommendAllWorkloads_PopulatesNotificationCodes(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Seed data with OOM events so EvaluateNotifications produces codes.
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       testutil.BaseDate.AddDate(0, 0, i),
			OrgID:            testutil.TestOrgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        testutil.TestNamespace,
			Workload:         testutil.TestWorkload,
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    testutil.TestContainer,
			CPURequestP50MC:  200,
			CPURequestP95MC:  210,
			CPUUsageP50MC:    190,
			CPUUsageP95MC:    200,
			CPUUsageP98MC:    205,
			CPUUsageP99MC:    208,
			CPUUsageMaxMC:    215,
			CPUThrottleP95MC: 5,
			CPUThrottleMaxMC: 10,
			MemRequestP50KiB: 524288,
			MemRequestP95KiB: 524800,
			MemUsageP50KiB:   524000,
			MemUsageP95KiB:   524288,
			MemUsageMaxKiB:   525312,
			MemRSSP95KiB:     524000,
			MemRSSMaxKiB:     525000,
			OOMCountSum:      3,
			CPUUsageMeanMC:   195,
			MemUsageMeanKiB:  523000,
			SampleCount:      96,
		})
	}

	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, results)

	oomFound := false
	for _, r := range results {
		for _, code := range r.NotificationCodes {
			if code == NotifOOMDetected {
				oomFound = true
			}
		}
	}
	assert.True(t, oomFound, "at least one recommendation should have OOM notification code")
}

func TestWriteRecommendations_DeferredOrgMetadataRefresh(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	rec := func(container string, cpu int64) ContainerRec {
		return ContainerRec{
			OrgID:            testutil.TestOrgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        testutil.TestNamespace,
			Workload:         testutil.TestWorkload,
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    container,
			Term:             "short",
			Engine:           "cost",
			RecCPURequestMC:  cpu,
			RecCPULimitMC:    cpu + 10,
			RecMemRequestKiB: 51200,
			RecMemLimitKiB:   53760,
		}
	}

	require.NoError(t, WriteRecommendations(ctx, pool, []ContainerRec{rec("main", 100)}))
	require.NoError(t, WriteRecommendations(ctx, pool, []ContainerRec{rec("sidecar", 200)}))

	var keyCount int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_container_keys WHERE org_id = $1`, testutil.TestOrgID).Scan(&keyCount)
	require.NoError(t, err)
	assert.Equal(t, 0, keyCount, "WriteRecommendations must not refresh org metadata per batch")

	require.NoError(t, RefreshOrgMetadata(ctx, pool, testutil.TestOrgID))

	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_container_keys WHERE org_id = $1`, testutil.TestOrgID).Scan(&keyCount)
	require.NoError(t, err)
	assert.Equal(t, 2, keyCount, "single RefreshOrgMetadata should populate all container keys")
}

func TestWriteRecommendations_PersistsNotificationCodes(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	rec := ContainerRec{
		OrgID:             testutil.TestOrgID,
		ClusterUUID:       testutil.TestClusterUUID,
		Namespace:         testutil.TestNamespace,
		Workload:          testutil.TestWorkload,
		WorkloadType:      testutil.TestWorkloadType,
		ContainerName:     testutil.TestContainer,
		Term:              "short",
		Engine:            "cost",
		RecCPURequestMC:   100,
		RecCPULimitMC:     110,
		RecMemRequestKiB:  51200,
		RecMemLimitKiB:    53760,
		ConfidenceLevel:   0.3,
		DataDays:          2,
		NotificationCodes: []int16{NotifLowConfidence, NotifOOMDetected},
	}

	err := WriteRecommendations(ctx, pool, []ContainerRec{rec})
	require.NoError(t, err)

	var codes []int16
	err = pool.QueryRow(ctx,
		`SELECT notification_codes FROM recommendation_sets
		 WHERE org_id=$1 AND term=$2 AND engine=$3`,
		rec.OrgID, rec.Term, rec.Engine).Scan(&codes)
	require.NoError(t, err)
	assert.Contains(t, codes, NotifLowConfidence)
	assert.Contains(t, codes, NotifOOMDetected)
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

func TestOOMMaxBumpClamp(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Seed identical data under two org_ids: one with OOMs, one without.
	orgOOM := "org-oom-clamp"
	orgNoOOM := "org-no-oom-clamp"
	for i := 0; i < 7; i++ {
		base := testutil.ContainerDigestRow{
			BucketDate:      testutil.BaseDate.AddDate(0, 0, i),
			ClusterUUID:     testutil.TestClusterUUID,
			Namespace:       testutil.TestNamespace,
			Workload:        testutil.TestWorkload,
			WorkloadType:    testutil.TestWorkloadType,
			ContainerName:   testutil.TestContainer,
			CPURequestP50MC: 200, CPURequestP95MC: 210,
			CPUUsageP50MC: 180, CPUUsageP95MC: 200, CPUUsageP98MC: 205,
			CPUUsageP99MC: 208, CPUUsageMaxMC: 210,
			CPUThrottleP95MC: 5, CPUThrottleMaxMC: 10,
			MemRequestP50KiB: 524288, MemRequestP95KiB: 524800,
			MemUsageP50KiB: 524000, MemUsageP95KiB: 524288, MemUsageMaxKiB: 525312,
			MemRSSP95KiB: 524000, MemRSSMaxKiB: 525000,
			CPUUsageMeanMC: 195, MemUsageMeanKiB: 523000,
			SampleCount: 96,
		}
		withOOM := base
		withOOM.OrgID = orgOOM
		withOOM.OOMCountSum = 10
		testutil.SeedContainerDigest(t, pool, withOOM)

		noOOM := base
		noOOM.OrgID = orgNoOOM
		noOOM.OOMCountSum = 0
		testutil.SeedContainerDigest(t, pool, noOOM)
	}

	end := testutil.BaseDate.AddDate(0, 0, 6)

	// MaxBump=0.5 should be clamped to 1.0, so OOM data produces no bump at all
	clampedCfg := OOMConfig{BaseBump: 0.15, MaxBump: 0.5}
	resultsClamped, err := RecommendAllWorkloads(ctx, pool, orgOOM, testutil.TestClusterUUID, testutil.BaseDate, end, clampedCfg)
	require.NoError(t, err)

	// No-OOM baseline: same data without OOM events, default config
	resultsBaseline, err := RecommendAllWorkloads(ctx, pool, orgNoOOM, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{})
	require.NoError(t, err)

	// With MaxBump clamped to 1.0, memory recommendations should equal the no-OOM baseline
	// (the clamp ensures OOMs never shrink memory, and bump=min(1.0, ...)=1.0 means no increase)
	for _, rc := range resultsClamped {
		if rc.Term != "short" || rc.Engine != "cost" {
			continue
		}
		for _, rb := range resultsBaseline {
			if rb.Term == rc.Term && rb.Engine == rc.Engine && rb.ContainerName == rc.ContainerName {
				assert.Equal(t, rb.RecMemRequestKiB, rc.RecMemRequestKiB,
					"MaxBump clamped to 1.0 should produce same memory rec as no-OOM baseline")
			}
		}
	}
}

func TestRecommendWorkloadsStreaming_EmitsBatches(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedDigestSeries(t, pool, 7, 200, 10, 524288, 1024)

	// Seed a second container
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:      testutil.BaseDate.AddDate(0, 0, i),
			OrgID:           testutil.TestOrgID,
			ClusterUUID:     testutil.TestClusterUUID,
			Namespace:       testutil.TestNamespace,
			Workload:        testutil.TestWorkload,
			WorkloadType:    testutil.TestWorkloadType,
			ContainerName:   "sidecar",
			CPURequestP50MC: 50, CPURequestP95MC: 60,
			CPUUsageP50MC: 40, CPUUsageP95MC: 55,
			CPUUsageP98MC: 58, CPUUsageP99MC: 59, CPUUsageMaxMC: 65,
			CPUThrottleP95MC: 2, CPUThrottleMaxMC: 5,
			MemRequestP50KiB: 102400, MemRequestP95KiB: 112640,
			MemUsageP50KiB: 100000, MemUsageP95KiB: 110000,
			MemUsageMaxKiB: 115000, MemRSSP95KiB: 108000, MemRSSMaxKiB: 113000,
			OOMCountSum: 0, CPUUsageMeanMC: 45, MemUsageMeanKiB: 105000,
			SampleCount: 96,
		})
	}

	end := testutil.BaseDate.AddDate(0, 0, 6)

	var batchCount int
	var totalRecs int
	err := RecommendWorkloadsStreaming(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end, OOMConfig{}, func(batch []ContainerRec) error {
		batchCount++
		totalRecs += len(batch)
		assert.NotEmpty(t, batch, "batch should not be empty")
		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, 1, batchCount, "2 containers < streamBatchSize, so one batch expected")
	assert.Equal(t, 12, totalRecs, "2 containers × 3 terms × 2 engines = 12")
}

func TestRecommendAllWorkloads_ShortTermWithFutureEnd(t *testing.T) {
	// Simulates the real-world scenario: data was ingested yesterday but the
	// engine runs today (end = now > latest digest). Before the fix, short-term
	// (WindowDays=1) would find 0 rows because it anchored to end=today, not
	// to the latest digest date. Now it anchors to the latest digest date.
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedDigestSeries(t, pool, 3, 200, 10, 524288, 1024)

	// end is 5 days after the latest digest (BaseDate+2), simulating
	// "engine runs well after last data upload"
	latestDigestDate := testutil.BaseDate.AddDate(0, 0, 2)
	futureEnd := latestDigestDate.AddDate(0, 0, 5)

	results, err := RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, futureEnd, OOMConfig{})
	require.NoError(t, err)

	termsSeen := map[string]bool{}
	for _, r := range results {
		termsSeen[r.Term] = true
	}

	assert.True(t, termsSeen["short"],
		"short-term must be produced even when end is days after the latest digest")
	assert.True(t, termsSeen["medium"] || termsSeen["long"],
		"medium or long term should also be produced with 3 days of data")
}

func TestMarkUnreportedContainersStale(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := testutil.TestOrgID
	cluster := testutil.TestClusterUUID

	// Insert two recommendation rows: one "old" (simulating a workload_type that changed)
	// and one "current" (the new composite key).
	oldUpdated := time.Now().UTC().Add(-1 * time.Hour)
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine, stale, updated_at
		) VALUES ($1, $2, 'ns', 'api', 'deployment', 'web', 'medium', 'cost', false, $3)`,
		orgID, cluster, oldUpdated,
	)
	require.NoError(t, err)

	// The "current" row was just upserted by the reconcile cycle (updated_at ≈ now).
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine, stale, updated_at
		) VALUES ($1, $2, 'ns', 'api', 'statefulset', 'web', 'medium', 'cost', false, now())`,
		orgID, cluster,
	)
	require.NoError(t, err)

	cycleStart := time.Now().UTC().Add(-30 * time.Second)
	marked, err := MarkUnreportedContainersStale(ctx, pool, orgID, cluster, cycleStart)
	require.NoError(t, err)
	assert.Equal(t, int64(1), marked, "should mark the old deployment row stale")

	// Verify: deployment row is stale, statefulset row is not.
	var deploymentStale, statefulsetStale bool
	err = pool.QueryRow(ctx,
		`SELECT stale FROM recommendation_sets WHERE org_id=$1 AND cluster_uuid=$2
		 AND workload_type='deployment' AND container_name='web'`,
		orgID, cluster,
	).Scan(&deploymentStale)
	require.NoError(t, err)
	assert.True(t, deploymentStale, "old deployment rec should be stale")

	err = pool.QueryRow(ctx,
		`SELECT stale FROM recommendation_sets WHERE org_id=$1 AND cluster_uuid=$2
		 AND workload_type='statefulset' AND container_name='web'`,
		orgID, cluster,
	).Scan(&statefulsetStale)
	require.NoError(t, err)
	assert.False(t, statefulsetStale, "current statefulset rec should remain active")
}

func TestMarkUnreportedContainersStale_NoFalsePositivesOnEmptyCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := "org-no-fp"
	cluster := "aaaaaaaa-1111-2222-3333-444444444444"

	// Insert a recommendation that was just updated.
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine, stale, updated_at
		) VALUES ($1, $2, 'ns', 'app', 'deployment', 'main', 'short', 'cost', false, now())`,
		orgID, cluster,
	)
	require.NoError(t, err)

	cycleStart := time.Now().UTC().Add(-5 * time.Second)
	marked, err := MarkUnreportedContainersStale(ctx, pool, orgID, cluster, cycleStart)
	require.NoError(t, err)
	assert.Equal(t, int64(0), marked, "recently-updated rows should not be marked stale")
}

func TestMarkUnreportedContainersStale_DoesNotAffectOtherClusters(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := "org-cross-cluster"
	clusterA := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	clusterB := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	oldTime := time.Now().UTC().Add(-2 * time.Hour)
	// Old row in cluster A (should be marked stale).
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine, stale, updated_at
		) VALUES ($1, $2, 'ns', 'app', 'deployment', 'main', 'short', 'cost', false, $3)`,
		orgID, clusterA, oldTime,
	)
	require.NoError(t, err)

	// Old row in cluster B (should NOT be affected by cluster A's reconcile).
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine, stale, updated_at
		) VALUES ($1, $2, 'ns', 'app', 'deployment', 'main', 'short', 'cost', false, $3)`,
		orgID, clusterB, oldTime,
	)
	require.NoError(t, err)

	cycleStart := time.Now().UTC().Add(-30 * time.Second)
	marked, err := MarkUnreportedContainersStale(ctx, pool, orgID, clusterA, cycleStart)
	require.NoError(t, err)
	assert.Equal(t, int64(1), marked)

	// Verify cluster B's row is untouched.
	var clusterBStale bool
	err = pool.QueryRow(ctx,
		`SELECT stale FROM recommendation_sets WHERE org_id=$1 AND cluster_uuid=$2`,
		orgID, clusterB,
	).Scan(&clusterBStale)
	require.NoError(t, err)
	assert.False(t, clusterBStale, "cluster B recommendations should be unaffected")
}
