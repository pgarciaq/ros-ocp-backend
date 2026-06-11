package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// BH-PERF-008: p99 GET /recommendations with precomputed business_hours schedule < 100ms.
func TestGETRecommendation_PrecomputedBH_P99Latency(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL (BH-PERF-008)")
	}
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	config.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-bh-perf008-" + t.Name()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	require.NoError(t, engine.UpsertBusinessHoursSchedule(ctx, pool, engine.BusinessHoursSchedule{
		OrgID:       orgID,
		ClusterUUID: engine.OrgClusterSentinelUUID,
		Timezone:    "UTC",
		Days:        []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime:   "00:00",
		EndTime:     "23:59",
		Enabled:     true,
	}))

	start := testutil.RecentStart()
	end := start.AddDate(0, 0, 6)
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    start.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: 400,
			ScheduleType:  "all_hours",
		})
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    start.AddDate(0, 0, i),
			OrgID:         orgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      testutil.TestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: testutil.TestContainer,
			CPUUsageP95MC: 80,
			ScheduleType:  "business_hours",
		})
	}

	recs, err := engine.RecommendAllWorkloads(ctx, pool, orgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recs))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	identityHeader := makeIdentityHeader(orgID)
	const samples = 100
	latencies := make([]time.Duration, samples)

	for i := 0; i < samples; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		begin := time.Now()
		app.ServeHTTP(rec, req)
		latencies[i] = time.Since(begin)
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		var response struct {
			Data []map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.NotEmpty(t, response.Data)
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p99Idx := (samples * 99) / 100
	if p99Idx >= samples {
		p99Idx = samples - 1
	}
	p99 := latencies[p99Idx]
	t.Logf("BH-PERF-008: p99 GET /recommendations = %v (threshold 100ms)", p99)
	require.Less(t, p99, 100*time.Millisecond, "p99 latency exceeds 100ms threshold")
}

// BenchmarkGETRecommendation_PrecomputedBH measures the same path as TestGETRecommendation_PrecomputedBH_P99Latency.
func BenchmarkGETRecommendation_PrecomputedBH(b *testing.B) {
	if testing.Short() {
		b.Skip("requires PostgreSQL (BH-PERF-008); run without -short")
	}
	// Benchmark setup requires *testing.T (testcontainers); use the Test above for the p99 gate.
	b.Skip("use TestGETRecommendation_PrecomputedBH_P99Latency for BH-PERF-008 p99 assertion")
}
