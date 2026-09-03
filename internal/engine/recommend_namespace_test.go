package engine

import (
	"context"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateNamespaceNotifications_NewWorkload(t *testing.T) {
	rec := NamespaceRec{
		DataDays:        0,
		ConfidenceLevel: 0.0,
	}
	codes := EvaluateNamespaceNotifications(rec)

	found := false
	for _, c := range codes {
		if c == NotifNewWorkload {
			found = true
		}
	}
	if !found {
		t.Errorf("expected NotifNewWorkload (%d) in codes %v", NotifNewWorkload, codes)
	}
}

func TestEvaluateNamespaceNotifications_LowConfidence(t *testing.T) {
	rec := NamespaceRec{
		DataDays:        3,
		ConfidenceLevel: 0.3,
	}
	codes := EvaluateNamespaceNotifications(rec)

	found := false
	for _, c := range codes {
		if c == NotifLowConfidence {
			found = true
		}
	}
	if !found {
		t.Errorf("expected NotifLowConfidence (%d) in codes %v", NotifLowConfidence, codes)
	}
}

func TestEvaluateNamespaceNotifications_NoNotifications(t *testing.T) {
	rec := NamespaceRec{
		DataDays:        10,
		ConfidenceLevel: 0.8,
	}
	codes := EvaluateNamespaceNotifications(rec)

	if len(codes) != 0 {
		t.Errorf("expected no notification codes, got %v", codes)
	}
}

func TestEvaluateNamespaceNotifications_NoOOMOrIdle(t *testing.T) {
	// Even with zero days and low confidence, only NewWorkload should appear
	// (not OOM or idle, which are container-only).
	rec := NamespaceRec{
		DataDays:        0,
		ConfidenceLevel: 0.0,
	}
	codes := EvaluateNamespaceNotifications(rec)

	for _, c := range codes {
		if c == NotifOOMDetected {
			t.Error("namespace notifications should never include OOM")
		}
		if c == NotifIdleWorkload {
			t.Error("namespace notifications should never include idle workload")
		}
	}
}

func TestEvaluateNamespaceNotifications_BothNewAndLowConfidence(t *testing.T) {
	// DataDays=0 triggers NewWorkload, but LowConfidence requires DataDays>0.
	rec := NamespaceRec{
		DataDays:        0,
		ConfidenceLevel: 0.2,
	}
	codes := EvaluateNamespaceNotifications(rec)

	hasNew := false
	hasLow := false
	for _, c := range codes {
		if c == NotifNewWorkload {
			hasNew = true
		}
		if c == NotifLowConfidence {
			hasLow = true
		}
	}
	if !hasNew {
		t.Error("expected NotifNewWorkload for DataDays=0")
	}
	if hasLow {
		t.Error("LowConfidence should not fire when DataDays=0")
	}
}

func TestEvaluateNamespaceNotifications_StaleData(t *testing.T) {
	rec := NamespaceRec{
		DataDays:        10,
		ConfidenceLevel: 0.8,
		Stale:           true,
	}
	codes := EvaluateNamespaceNotifications(rec)
	assert.Contains(t, codes, NotifStaleData)
}

func TestEvaluateNamespaceNotifications_NotStale_NoStaleDataCode(t *testing.T) {
	rec := NamespaceRec{
		DataDays:        10,
		ConfidenceLevel: 0.8,
		Stale:           false,
	}
	codes := EvaluateNamespaceNotifications(rec)
	assert.NotContains(t, codes, NotifStaleData)
}

func TestEvaluateNamespaceNotifications_MemoryTrendingUp(t *testing.T) {
	rec := NamespaceRec{
		DataDays:        10,
		ConfidenceLevel: 0.8,
		MemTrendSlope:   600.0, // above namespace threshold (500 KiB/day)
	}
	codes := EvaluateNamespaceNotifications(rec)

	found := false
	for _, c := range codes {
		if c == NotifMemoryTrendingUp {
			found = true
		}
	}
	if !found {
		t.Errorf("expected NotifMemoryTrendingUp (%d) in codes %v", NotifMemoryTrendingUp, codes)
	}
}

func TestEvaluateNamespaceNotifications_MemoryTrendBelowThreshold(t *testing.T) {
	rec := NamespaceRec{
		DataDays:        10,
		ConfidenceLevel: 0.8,
		MemTrendSlope:   200.0, // below namespace threshold (500 KiB/day)
	}
	codes := EvaluateNamespaceNotifications(rec)

	for _, c := range codes {
		if c == NotifMemoryTrendingUp {
			t.Error("MemoryTrendingUp should not fire when slope is below namespace threshold")
		}
	}
}

func TestEvaluateNamespaceNotifications_StillNoOOMOrIdle(t *testing.T) {
	rec := NamespaceRec{
		DataDays:        10,
		ConfidenceLevel: 0.8,
		MemTrendSlope:   1000.0,
	}
	codes := EvaluateNamespaceNotifications(rec)

	for _, c := range codes {
		if c == NotifOOMDetected {
			t.Error("namespace notifications should never include OOM")
		}
		if c == NotifIdleWorkload {
			t.Error("namespace notifications should never include idle workload")
		}
	}
}

func TestEvaluateNamespaceNotificationsWithThresholds_SparseData(t *testing.T) {
	th := NotificationThresholdsFromSizing(defaultNamespaceSizingThresholds)
	rec := NamespaceRec{DataDays: 1, ConfidenceLevel: 1.0}
	codes := EvaluateNamespaceNotificationsWithThresholds(rec, th)
	assert.Contains(t, codes, NotifSparseData)
}

func TestEvaluateNamespaceNotificationsWithThresholds_SparseData_ExactThreshold(t *testing.T) {
	th := NotificationThresholdsFromSizing(defaultNamespaceSizingThresholds)
	rec := NamespaceRec{DataDays: 2, ConfidenceLevel: 1.0}
	codes := EvaluateNamespaceNotificationsWithThresholds(rec, th)
	assert.Contains(t, codes, NotifSparseData, "data_days == threshold should fire")
}

func TestEvaluateNamespaceNotificationsWithThresholds_SparseData_AboveThreshold(t *testing.T) {
	th := NotificationThresholdsFromSizing(defaultNamespaceSizingThresholds)
	rec := NamespaceRec{DataDays: 3, ConfidenceLevel: 1.0}
	codes := EvaluateNamespaceNotificationsWithThresholds(rec, th)
	assert.NotContains(t, codes, NotifSparseData, "data_days above threshold should not fire")
}

func TestEvaluateNamespaceNotificationsWithThresholds_SparseData_ZeroDays(t *testing.T) {
	th := NotificationThresholdsFromSizing(defaultNamespaceSizingThresholds)
	rec := NamespaceRec{DataDays: 0, ConfidenceLevel: 0.0}
	codes := EvaluateNamespaceNotificationsWithThresholds(rec, th)
	assert.NotContains(t, codes, NotifSparseData, "zero data days should not fire SPARSE_DATA (NEW_WORKLOAD fires instead)")
	assert.Contains(t, codes, NotifNewWorkload)
}

func TestEvaluateNamespaceNotificationsWithThresholds_SparseData_OrthogonalToLowConfidence(t *testing.T) {
	th := NotificationThresholdsFromSizing(defaultNamespaceSizingThresholds)
	rec := NamespaceRec{DataDays: 1, ConfidenceLevel: 1.0}
	codes := EvaluateNamespaceNotificationsWithThresholds(rec, th)
	assert.Contains(t, codes, NotifSparseData, "sparse data should fire even with high confidence")
	assert.NotContains(t, codes, NotifLowConfidence, "low confidence should NOT fire with confidence=1.0")
}

// --- Integration tests (testcontainers-go) ---

func TestRecommendAllNamespaces_SingleNamespace(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, testutil.TestNamespace, 7, 200, 10, 524288, 1024)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)

	// 3 terms x 2 engines = 6
	require.Len(t, results, 6)

	for _, r := range results {
		assert.Equal(t, testutil.TestOrgID, r.OrgID)
		assert.Equal(t, testutil.TestClusterUUID, r.ClusterUUID)
		assert.Equal(t, testutil.TestNamespace, r.Namespace)
		assert.True(t, r.RecCPURequestMC > 0, "CPU request should be positive")
		assert.True(t, r.RecMemRequestKiB > 0, "memory request should be positive")
		assert.True(t, r.RecCPULimitMC >= r.RecCPURequestMC, "CPU limit >= request")
		assert.True(t, r.RecMemLimitKiB >= r.RecMemRequestKiB, "memory limit >= request")
		assert.False(t, r.MonitoringStartTime.IsZero(), "MonitoringStartTime should be set")
		assert.False(t, r.MonitoringEndTime.IsZero(), "MonitoringEndTime should be set")
	}

	termsSeen := map[string]int{}
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

func TestRecommendAllNamespaces_InsufficientData(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// 1 day of data: short (minData=1) should produce results,
	// medium (minData=3) and long (minData=7) should be skipped.
	testutil.SeedNamespaceDigestSeries(t, pool, "ns-insufficient", 1, 200, 0, 524288, 0)
	end := testutil.BaseDate

	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)

	// Only short/cost + short/performance = 2
	require.Len(t, results, 2)
	for _, r := range results {
		assert.Equal(t, "short", r.Term)
	}
}

func TestRecommendAllNamespaces_TermSkipping(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// No digests at all for this org
	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := RecommendAllNamespaces(ctx, pool, "org-ns-empty", testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestWriteNamespaceRecommendations_Roundtrip(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-write-rt", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)

	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	err = WriteNamespaceRecommendations(ctx, pool, results)
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM namespace_recommendation_sets
		 WHERE org_id = $1 AND namespace_name = $2 AND term IS NOT NULL`,
		testutil.TestOrgID, "ns-write-rt").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 6, count, "should have 6 native rows (3 terms x 2 engines)")

	// Read back a specific row and verify columns
	var cpuReq int64
	var monEnd interface{}
	err = pool.QueryRow(ctx,
		`SELECT rec_cpu_request_millicores, monitoring_end_time
		 FROM namespace_recommendation_sets
		 WHERE org_id=$1 AND namespace_name=$2 AND term='short' AND engine='cost'`,
		testutil.TestOrgID, "ns-write-rt").Scan(&cpuReq, &monEnd)
	require.NoError(t, err)
	assert.True(t, cpuReq > 0, "CPU request should be positive")
	assert.NotNil(t, monEnd, "monitoring_end_time should be set")
}

func TestWriteNamespaceRecommendations_RejectsBusinessHours(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	recs := []NamespaceRec{{
		OrgID:        testutil.TestOrgID,
		ClusterUUID:  testutil.TestClusterUUID,
		Namespace:    "ns-bh-refuse",
		Term:         "short",
		Engine:       "cost",
		ScheduleType: digestScheduleBusinessHours,
	}}
	err := WriteNamespaceRecommendations(ctx, pool, recs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "business_hours")

	err = WriteNamespaceRecommendationHistory(ctx, pool, recs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "business_hours")
}

func TestWriteNamespaceRecommendationHistory_Roundtrip(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-hist-rt", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)

	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	err = WriteNamespaceRecommendationHistory(ctx, pool, results)
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM historical_namespace_recommendation_sets
		 WHERE org_id = $1 AND namespace_name = $2 AND term IS NOT NULL`,
		testutil.TestOrgID, "ns-hist-rt").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 6, count, "should have 6 history rows (3 terms x 2 engines)")

	// Verify monitoring_start_time is populated
	var monStart interface{}
	err = pool.QueryRow(ctx,
		`SELECT monitoring_start_time FROM historical_namespace_recommendation_sets
		 WHERE org_id=$1 AND namespace_name=$2 AND term='short' AND engine='cost'`,
		testutil.TestOrgID, "ns-hist-rt").Scan(&monStart)
	require.NoError(t, err)
	assert.NotNil(t, monStart, "monitoring_start_time should be set")
}

func TestRecommendAllNamespaces_Confidence(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-conf", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)

	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	for _, r := range results {
		assert.True(t, r.ConfidenceLevel >= 0 && r.ConfidenceLevel <= 1.0,
			"confidence should be 0-1, got %f for %s/%s", r.ConfidenceLevel, r.Term, r.Engine)

		// confidence = dataDays / windowDays
		// short: 7/1 capped at 1.0, medium: 7/7 = 1.0, long: 7/15 ≈ 0.47
		if r.Term == "short" {
			assert.InDelta(t, 1.0, r.ConfidenceLevel, 0.01, "short with 7 days should have max confidence")
		}
		if r.Term == "long" {
			assert.InDelta(t, float64(r.DataDays)/15.0, float64(r.ConfidenceLevel), 0.15,
				"long-term confidence should approximate dataDays/windowDays")
		}
	}
}

func TestRecommendAllNamespaces_MultipleNamespaces(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-alpha", 7, 200, 10, 524288, 1024)
	testutil.SeedNamespaceDigestSeries(t, pool, "ns-beta", 7, 300, 5, 262144, 512)
	testutil.SeedNamespaceDigestSeries(t, pool, "ns-gamma", 7, 100, 20, 1048576, 2048)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)

	// 3 namespaces x 3 terms x 2 engines = 18
	require.Len(t, results, 18)

	nsCount := map[string]int{}
	for _, r := range results {
		nsCount[r.Namespace]++
	}
	assert.Equal(t, 6, nsCount["ns-alpha"])
	assert.Equal(t, 6, nsCount["ns-beta"])
	assert.Equal(t, 6, nsCount["ns-gamma"])

	// Verify different namespaces produce different memory recommendations.
	// Memory is unaffected by the CPU floor, so different base values
	// should always produce different results.
	nsMem := map[string]int64{}
	for _, r := range results {
		if r.Term == "short" && r.Engine == "cost" {
			nsMem[r.Namespace] = r.RecMemRequestKiB
		}
	}
	assert.NotEqual(t, nsMem["ns-alpha"], nsMem["ns-gamma"],
		"different input data should produce different memory recommendations")
}

func TestWriteNamespaceRecommendations_Upsert(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-upsert", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)

	// First write
	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	err = WriteNamespaceRecommendations(ctx, pool, results)
	require.NoError(t, err)

	var firstCPU int64
	err = pool.QueryRow(ctx,
		`SELECT rec_cpu_request_millicores FROM namespace_recommendation_sets
		 WHERE org_id=$1 AND namespace_name=$2 AND term='short' AND engine='cost'`,
		testutil.TestOrgID, "ns-upsert").Scan(&firstCPU)
	require.NoError(t, err)

	// Mutate one result and write again
	for i := range results {
		results[i].RecCPURequestMC += 999
	}
	err = WriteNamespaceRecommendations(ctx, pool, results)
	require.NoError(t, err)

	// Verify updated, not duplicated
	var secondCPU int64
	err = pool.QueryRow(ctx,
		`SELECT rec_cpu_request_millicores FROM namespace_recommendation_sets
		 WHERE org_id=$1 AND namespace_name=$2 AND term='short' AND engine='cost'`,
		testutil.TestOrgID, "ns-upsert").Scan(&secondCPU)
	require.NoError(t, err)
	assert.Equal(t, firstCPU+999, secondCPU, "upsert should update, not duplicate")

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM namespace_recommendation_sets
		 WHERE org_id=$1 AND namespace_name=$2 AND term IS NOT NULL`,
		testutil.TestOrgID, "ns-upsert").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 6, count, "should still have 6 rows after upsert")
}

func TestRecommendAllNamespaces_P60P98P99Percentiles(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// SeedFull populates P60/P98/P99 columns that SeedNamespaceDigestSeries leaves NULL.
	// CPU cost engine uses P60 (CostPercentile=0.60).
	// CPU perf engine uses P98 (PerfPercentile=0.98).
	// Memory cost uses P95, memory perf uses max — but SelectMemUsagePercentile
	// should return correct values for any percentile.
	testutil.SeedNamespaceDigestSeriesFull(t, pool, "ns-pct-full", 7, 300, 10, 524288, 1024)

	// Also seed without P60/P98/P99 (NULL → COALESCE to 0) for comparison.
	testutil.SeedNamespaceDigestSeries(t, pool, "ns-pct-sparse", 7, 300, 10, 524288, 1024)

	end := testutil.BaseDate.AddDate(0, 0, 6)

	resultsFull, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)

	fullRecs := map[string]NamespaceRec{}
	sparseRecs := map[string]NamespaceRec{}
	for _, r := range resultsFull {
		key := r.Namespace + "/" + r.Term + "/" + r.Engine
		if r.Namespace == "ns-pct-full" {
			fullRecs[key] = r
		} else if r.Namespace == "ns-pct-sparse" {
			sparseRecs[key] = r
		}
	}

	// CPU cost uses P60 → "full" seeds distinct P60 values, "sparse" has 0 (NULL→COALESCE).
	// This means full-seeded namespace should produce a higher CPU cost recommendation
	// than the sparse one (where P60 falls back to 0).
	fullCostCPU := fullRecs["ns-pct-full/short/cost"].RecCPURequestMC
	sparseCostCPU := sparseRecs["ns-pct-sparse/short/cost"].RecCPURequestMC
	assert.Greater(t, fullCostCPU, sparseCostCPU,
		"full P60 data (%d) should produce higher CPU cost rec than sparse/zero P60 (%d)",
		fullCostCPU, sparseCostCPU)

	// CPU perf uses P98 → same pattern: full has distinct P98, sparse has 0.
	fullPerfCPU := fullRecs["ns-pct-full/short/performance"].RecCPURequestMC
	sparsePerfCPU := sparseRecs["ns-pct-sparse/short/performance"].RecCPURequestMC
	assert.Greater(t, fullPerfCPU, sparsePerfCPU,
		"full P98 data (%d) should produce higher CPU perf rec than sparse/zero P98 (%d)",
		fullPerfCPU, sparsePerfCPU)
}

func TestNamespaceRec_VariationFields(t *testing.T) {
	rec := NamespaceRec{
		OrgID:                  "org1",
		ClusterUUID:            "cluster-1",
		Namespace:              "default",
		Term:                   "short",
		Engine:                 "cost",
		RecCPURequestMC:        200,
		RecCPULimitMC:          400,
		RecMemRequestKiB:       4096,
		RecMemLimitKiB:         8192,
		CurrentCPURequestMC:    100,
		CurrentCPULimitMC:      200,
		CurrentMemRequestKiB:   2048,
		CurrentMemLimitKiB:     4096,
		VariationCPURequestPct: 100.0,
		VariationMemRequestPct: 100.0,
		ConfidenceLevel:        0.9,
		DataDays:               14,
		Stale:                  false,
	}

	// Verify struct fields are set correctly.
	if rec.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", rec.Namespace)
	}
	if rec.VariationCPURequestPct != 100 {
		t.Errorf("expected variation 100, got %d", rec.VariationCPURequestPct)
	}
}

func TestRecommendAllNamespaces_ShortTermWithFutureEnd(t *testing.T) {
	// Simulates real-world: data from 3 days ago, engine runs today (end > latest digest).
	// Short-term must still produce results because the window is anchored to the
	// latest digest date, not to end.
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, "test-ns", 3, 200, 10, 524288, 1024)

	latestDigestDate := testutil.BaseDate.AddDate(0, 0, 2)
	futureEnd := latestDigestDate.AddDate(0, 0, 5)

	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, futureEnd)
	require.NoError(t, err)

	termsSeen := map[string]bool{}
	for _, r := range results {
		termsSeen[r.Term] = true
	}

	assert.True(t, termsSeen["short"],
		"short-term must be produced even when end is days after the latest digest")
}
