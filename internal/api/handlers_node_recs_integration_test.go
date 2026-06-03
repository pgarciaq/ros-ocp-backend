package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// seedGPUNodesForTimeslicing seeds multiple containers on the same node with
// underutilized GPU metrics — the baseline scenario for time-slicing recommendations.
func seedGPUNodesForTimeslicing(t *testing.T, pool *pgxpool.Pool, start time.Time, days int, nodeName string) {
	t.Helper()
	containers := []struct {
		ns, wl, cn string
		smAvg      float64
	}{
		{"ml-team", "training-a", "gpu-worker", 0.12},
		{"ml-team", "training-b", "gpu-worker", 0.08},
		{"ml-team", "inference", "gpu-worker", 0.15},
	}
	for _, c := range containers {
		for i := 0; i < days; i++ {
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				IntervalStart:       start.AddDate(0, 0, i),
				ClusterUUID:         testutil.TestClusterUUID,
				Namespace:           c.ns,
				Workload:            c.wl,
				WorkloadType:        "deployment",
				ContainerName:       c.cn,
				GPUModelName:        "NVIDIA T4",
				NodeName:            nodeName,
				FBUsageMinMiB:       500,
				FBUsageMaxMiB:       2000,
				FBUsageAvgMiB:       1200,
				TensorPipeActiveMin: 0.01,
				TensorPipeActiveMax: 0.10,
				TensorPipeActiveAvg: 0.05,
				DRAMActiveMin:       0.02,
				DRAMActiveMax:       0.08,
				DRAMActiveAvg:       0.05,
				SMActiveMin:         c.smAvg - 0.03,
				SMActiveMax:         c.smAvg + 0.05,
				SMActiveAvg:         c.smAvg,
			})
		}
	}
}

func setupNodeRecsEcho(pool *pgxpool.Pool) *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/gpu/timeslicing", api.GetNodeRecommendations)
	return app
}

// --- TS-02: Integration test for node_name persistence ---

func TestUpsertGPUDigests_StoresNodeName(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	start := testutil.RecentStart()

	testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
		IntervalStart: start,
		ClusterUUID:   testutil.TestClusterUUID,
		Namespace:     "test-ns",
		Workload:      "test-deploy",
		WorkloadType:  "deployment",
		ContainerName: "gpu-container",
		GPUModelName:  "NVIDIA T4",
		NodeName:      "gpu-worker-42",
		FBUsageMinMiB: 100,
		FBUsageMaxMiB: 500,
		FBUsageAvgMiB: 300,
		SMActiveMin:   0.05,
		SMActiveMax:   0.15,
		SMActiveAvg:   0.10,
		DRAMActiveMin: 0.02,
		DRAMActiveMax: 0.08,
		DRAMActiveAvg: 0.05,
	})

	var nodeName string
	err := pool.QueryRow(ctx,
		`SELECT COALESCE(node_name, '') FROM gpu_container_digests
		 WHERE cluster_uuid = $1 AND namespace = 'test-ns' AND container_name = 'gpu-container'`,
		testutil.TestClusterUUID,
	).Scan(&nodeName)
	require.NoError(t, err)
	assert.Equal(t, "gpu-worker-42", nodeName)
}

func TestUpsertGPUDigests_NodeNameEmptyWhenNotProvided(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	start := testutil.RecentStart()

	testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
		IntervalStart: start,
		ClusterUUID:   testutil.TestClusterUUID,
		Namespace:     "test-ns",
		Workload:      "test-deploy",
		WorkloadType:  "deployment",
		ContainerName: "no-node",
		GPUModelName:  "NVIDIA T4",
		SMActiveAvg:   0.10,
	})

	var nodeName string
	err := pool.QueryRow(ctx,
		`SELECT COALESCE(node_name, '') FROM gpu_container_digests
		 WHERE cluster_uuid = $1 AND container_name = 'no-node'`,
		testutil.TestClusterUUID,
	).Scan(&nodeName)
	require.NoError(t, err)
	assert.Equal(t, "", nodeName)
}

// --- TS-17: Integration test for QueryGPURecommendations node map ---

func TestQueryGPURecommendations_ReturnsNodeMap(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	start := testutil.RecentStart()
	end := start.AddDate(0, 0, 6)

	for i := 0; i < 7; i++ {
		testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
			IntervalStart: start.AddDate(0, 0, i),
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     "ml-ns",
			Workload:      "train",
			WorkloadType:  "deployment",
			ContainerName: "gpu-main",
			GPUModelName:  "NVIDIA T4",
			NodeName:      "gpu-node-7",
			FBUsageMinMiB: 100,
			FBUsageMaxMiB: 500,
			FBUsageAvgMiB: 300,
			SMActiveMin:   0.05,
			SMActiveMax:   0.15,
			SMActiveAvg:   0.10,
			DRAMActiveMin: 0.02,
			DRAMActiveMax: 0.08,
			DRAMActiveAvg: 0.05,
		})
	}

	ctx := context.Background()
	terms := engine.DefaultTerms()
	recs, nodeMap, nodeLastSeen, err := engine.QueryGPURecommendations(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, terms, nil)
	require.NoError(t, err)
	require.NotNil(t, recs)
	require.NotNil(t, nodeMap)
	require.NotNil(t, nodeLastSeen)

	assert.Equal(t, "gpu-node-7", nodeMap["ml-ns/train/gpu-main"])

	ls, ok := nodeLastSeen["gpu-node-7"]
	assert.True(t, ok, "nodeLastSeen should contain the node")
	assert.False(t, ls.IsZero())
	expectedLast := start.AddDate(0, 0, 6)
	assert.True(t, ls.Equal(expectedLast) || ls.After(expectedLast.AddDate(0, 0, -1)),
		"nodeLastSeen should be around the last seeded date")
}

func TestQueryGPURecommendations_NodeLastSeenTracksMax(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	start := testutil.RecentStart()
	end := start.AddDate(0, 0, 6)

	// Two containers on the same node with different last-seen dates
	for i := 0; i < 3; i++ {
		testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
			IntervalStart: start.AddDate(0, 0, i),
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     "ns", Workload: "wl1", WorkloadType: "deployment",
			ContainerName: "c1", GPUModelName: "T4", NodeName: "shared-node",
			SMActiveAvg: 0.10, DRAMActiveAvg: 0.05,
		})
	}
	for i := 0; i < 7; i++ {
		testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
			IntervalStart: start.AddDate(0, 0, i),
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     "ns", Workload: "wl2", WorkloadType: "deployment",
			ContainerName: "c2", GPUModelName: "T4", NodeName: "shared-node",
			SMActiveAvg: 0.10, DRAMActiveAvg: 0.05,
		})
	}

	ctx := context.Background()
	terms := engine.DefaultTerms()
	_, _, nodeLastSeen, err := engine.QueryGPURecommendations(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, terms, nil)
	require.NoError(t, err)

	ls := nodeLastSeen["shared-node"]
	latestDate := start.AddDate(0, 0, 6)
	assert.True(t, ls.Equal(latestDate) || ls.After(latestDate.AddDate(0, 0, -1)),
		"nodeLastSeen should track the max across all containers on that node")
}

// --- TS-19: Integration test for empty node recommendations ---

func TestGetNodeRecommendations_Empty(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'empty-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNodeRecsEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var response model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Equal(t, 0, response.Meta.Count)
	assert.Empty(t, response.Data)
}

func TestGetNodeRecommendations_Unauthorized(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupNodeRecsEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// --- TS-20: Integration test with seeded GPU data ---

func TestGetNodeRecommendations_WithData(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'gpu-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	seedGPUNodesForTimeslicing(t, pool, start, 7, "gpu-t4-worker-1")

	app := setupNodeRecsEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var response model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.NotEmpty(t, response.Data, "expected node recommendations in response data")
	require.Greater(t, response.Meta.Count, 0, "should have at least one node recommendation")

	nodeRec := response.Data[0]
	assert.Equal(t, "gpu-t4-worker-1", nodeRec.NodeName)
	assert.Equal(t, "gpu_time_slicing", nodeRec.RecommendationType)
	assert.Contains(t, nodeRec.GPUModel, "T4")
	assert.GreaterOrEqual(t, nodeRec.RecommendedReplicas, 2)
	assert.LessOrEqual(t, nodeRec.RecommendedReplicas, 8)
	assert.Greater(t, nodeRec.Confidence, float32(0))
	assert.NotEmpty(t, nodeRec.CandidateContainers)
	assert.Contains(t, nodeRec.NotificationCodes, engine.NotifGPUTimeSharingCandidate)
}

func TestGetNodeRecommendations_OrgIsolation(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgA := "orgAAAA1111"
	orgB := "orgBBBB2222"
	clusterA := "aaaa1111-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	clusterB := "bbbb2222-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (100, $1), (200, $2) ON CONFLICT DO NOTHING`, orgA, orgB)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (100, $1, 'cluster-a', 'src-a', now()), (200, $2, 'cluster-b', 'src-b', now()) ON CONFLICT DO NOTHING`, clusterA, clusterB)
	require.NoError(t, err)

	start := testutil.RecentStart()
	// Seed GPU data for cluster A
	for i := 0; i < 7; i++ {
		for _, c := range []string{"c1", "c2", "c3"} {
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				IntervalStart: start.AddDate(0, 0, i), ClusterUUID: clusterA,
				Namespace: "ns-a", Workload: "wl-" + c, WorkloadType: "deployment",
				ContainerName: c, GPUModelName: "T4", NodeName: "node-a",
				SMActiveAvg: 0.10, DRAMActiveAvg: 0.05,
				FBUsageMaxMiB: 2000, FBUsageAvgMiB: 1000,
			})
		}
	}

	app := setupNodeRecsEcho(pool)

	t.Run("orgA sees its cluster data", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgA))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp model.NodeRecommendationListResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Greater(t, resp.Meta.Count, 0)
		for _, r := range resp.Data {
			assert.Equal(t, clusterA, r.ClusterUUID)
		}
	})

	t.Run("orgB sees nothing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgB))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp model.NodeRecommendationListResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, 0, resp.Meta.Count)
	})
}

// --- TS-21: Integration test for API filters ---

func TestGetNodeRecommendations_FilterByNodeName(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'filter-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	seedGPUNodesForTimeslicing(t, pool, start, 7, "target-node")

	// Seed a second node's containers on the same cluster
	for i := 0; i < 7; i++ {
		for _, c := range []string{"cx", "cy", "cz"} {
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				IntervalStart: start.AddDate(0, 0, i), ClusterUUID: testutil.TestClusterUUID,
				Namespace: "other-ns", Workload: "wl-" + c, WorkloadType: "deployment",
				ContainerName: c, GPUModelName: "NVIDIA T4", NodeName: "other-node",
				SMActiveAvg: 0.10, DRAMActiveAvg: 0.05,
				FBUsageMaxMiB: 2000, FBUsageAvgMiB: 1000,
			})
		}
	}

	app := setupNodeRecsEcho(pool)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing?filter%5Bnode%5D=target-node", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var response model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Greater(t, response.Meta.Count, 0)
	for _, r := range response.Data {
		assert.Equal(t, "target-node", r.NodeName)
	}
}

func TestGetNodeRecommendations_FilterByGPUModel(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'model-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	// Seed T4 containers
	seedGPUNodesForTimeslicing(t, pool, start, 7, "multi-gpu-node")

	// Seed L4 containers on same node (different GPU model)
	for i := 0; i < 7; i++ {
		for _, c := range []string{"la", "lb", "lc"} {
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				IntervalStart: start.AddDate(0, 0, i), ClusterUUID: testutil.TestClusterUUID,
				Namespace: "l4-ns", Workload: "wl-" + c, WorkloadType: "deployment",
				ContainerName: c, GPUModelName: "NVIDIA L4", NodeName: "multi-gpu-node",
				SMActiveAvg: 0.10, DRAMActiveAvg: 0.05,
				FBUsageMaxMiB: 2000, FBUsageAvgMiB: 1000,
			})
		}
	}

	app := setupNodeRecsEcho(pool)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing?filter%5Bgpu_model%5D=L4", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var response model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Greater(t, response.Meta.Count, 0)
	for _, r := range response.Data {
		assert.Contains(t, r.GPUModel, "L4")
	}
}

func TestGetNodeRecommendations_FilterByCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	cluster1, cluster2 := seedTwoClustersWithGPUData(t, pool)
	app := setupNodeRecsEcho(pool)

	reqAll := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	reqAll.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recAll := httptest.NewRecorder()
	app.ServeHTTP(recAll, reqAll)
	require.Equal(t, http.StatusOK, recAll.Code)

	var allResp model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(recAll.Body.Bytes(), &allResp))
	require.Greater(t, allResp.Meta.Count, 0, "expected time-slicing recs across seeded clusters")

	clustersSeen := map[string]bool{}
	for _, r := range allResp.Data {
		clustersSeen[r.ClusterUUID] = true
	}
	require.True(t, clustersSeen[cluster1] || clustersSeen[cluster2],
		"unfiltered response should include seeded cluster data")

	reqFiltered := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing?filter%5Bcluster%5D="+cluster1, nil)
	reqFiltered.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recFiltered := httptest.NewRecorder()
	app.ServeHTTP(recFiltered, reqFiltered)
	require.Equal(t, http.StatusOK, recFiltered.Code)

	var filteredResp model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(recFiltered.Body.Bytes(), &filteredResp))
	require.Greater(t, filteredResp.Meta.Count, 0)
	for _, r := range filteredResp.Data {
		assert.Equal(t, cluster1, r.ClusterUUID)
	}

	reqUnknown := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing?cluster_uuid=00000000-0000-0000-0000-000000000099", nil)
	reqUnknown.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recUnknown := httptest.NewRecorder()
	app.ServeHTTP(recUnknown, reqUnknown)
	require.Equal(t, http.StatusOK, recUnknown.Code)

	var unknownResp model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(recUnknown.Body.Bytes(), &unknownResp))
	assert.Equal(t, 0, unknownResp.Meta.Count)
	assert.Empty(t, unknownResp.Data)
}

// --- RBAC integration tests for GPU time-slicing (/gpu/timeslicing) endpoint ---

// setupNodeRecsEchoWithRBAC creates an Echo app with the Identity middleware
// and a custom middleware that injects the given RBAC permissions. RBAC is
// enabled for the duration of the test.
func setupNodeRecsEchoWithRBAC(t *testing.T, pool *pgxpool.Pool, perms map[string][]string) *echo.Echo {
	t.Helper()
	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = true
	t.Cleanup(func() { cfg.RBACEnabled = orig })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user.permissions", perms)
			return next(c)
		}
	})
	v1.GET("/recommendations/openshift/gpu/timeslicing", api.GetNodeRecommendations)
	return app
}

// seedTwoClustersWithGPUData creates two clusters in the same org, each with
// GPU containers on a distinct node, and returns both cluster UUIDs.
func seedTwoClustersWithGPUData(t *testing.T, pool *pgxpool.Pool) (cluster1, cluster2 string) {
	t.Helper()
	ctx := context.Background()
	cluster1 = "c1111111-1111-1111-1111-111111111111"
	cluster2 = "c2222222-2222-2222-2222-222222222222"

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'rbac-gpu-cluster-1', 'src-r1', now()),
		        (1, $2, 'rbac-gpu-cluster-2', 'src-r2', now())
		 ON CONFLICT DO NOTHING`, cluster1, cluster2)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for _, cl := range []struct {
		uuid, node string
	}{
		{cluster1, "gpu-node-c1"},
		{cluster2, "gpu-node-c2"},
	} {
		for i := 0; i < 7; i++ {
			for _, c := range []string{"a", "b", "c"} {
				testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
					IntervalStart: start.AddDate(0, 0, i), ClusterUUID: cl.uuid,
					Namespace: "ml-ns", Workload: "wl-" + c, WorkloadType: "deployment",
					ContainerName: c, GPUModelName: "NVIDIA T4", NodeName: cl.node,
					SMActiveAvg: 0.10, DRAMActiveAvg: 0.05,
					FBUsageMaxMiB: 2000, FBUsageAvgMiB: 1000,
				})
			}
		}
	}
	return cluster1, cluster2
}

func TestGetNodeRecommendations_RBAC_FiltersByCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	cluster1, _ := seedTwoClustersWithGPUData(t, pool)

	app := setupNodeRecsEchoWithRBAC(t, pool, map[string][]string{
		"openshift.cluster": {cluster1},
		"openshift.node":    {"*"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Greater(t, resp.Meta.Count, 0, "should have results for permitted cluster")
	for _, r := range resp.Data {
		assert.Equal(t, cluster1, r.ClusterUUID,
			"RBAC cluster filter should restrict to cluster1 only")
	}
}

func TestGetNodeRecommendations_RBAC_FiltersByNode(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)

	clusterUUID := "c3333333-3333-3333-3333-333333333333"
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'rbac-node-cluster', 'src-rn', now()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for _, nd := range []struct{ node string }{{"allowed-node"}, {"denied-node"}} {
		for i := 0; i < 7; i++ {
			for _, c := range []string{"x", "y", "z"} {
				testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
					IntervalStart: start.AddDate(0, 0, i), ClusterUUID: clusterUUID,
					Namespace: "ns-" + nd.node, Workload: "wl-" + c, WorkloadType: "deployment",
					ContainerName: c, GPUModelName: "NVIDIA T4", NodeName: nd.node,
					SMActiveAvg: 0.10, DRAMActiveAvg: 0.05,
					FBUsageMaxMiB: 2000, FBUsageAvgMiB: 1000,
				})
			}
		}
	}

	app := setupNodeRecsEchoWithRBAC(t, pool, map[string][]string{
		"openshift.cluster": {"*"},
		"openshift.node":    {"allowed-node"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Greater(t, resp.Meta.Count, 0, "should have results for allowed node")
	for _, r := range resp.Data {
		assert.Equal(t, "allowed-node", r.NodeName,
			"RBAC node filter should restrict to allowed-node only")
	}
}

func TestGetNodeRecommendations_RBAC_ClusterAndNodeCombined(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	cluster1, cluster2 := seedTwoClustersWithGPUData(t, pool)

	// User has access to cluster1 only, and only gpu-node-c1
	app := setupNodeRecsEchoWithRBAC(t, pool, map[string][]string{
		"openshift.cluster": {cluster1},
		"openshift.node":    {"gpu-node-c1"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Greater(t, resp.Meta.Count, 0)
	for _, r := range resp.Data {
		assert.Equal(t, cluster1, r.ClusterUUID)
		assert.Equal(t, "gpu-node-c1", r.NodeName)
	}

	// Verify cluster2 data exists but is filtered out
	_ = cluster2
}

func TestGetNodeRecommendations_RBAC_GlobalWildcard(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, cluster2 := seedTwoClustersWithGPUData(t, pool)

	app := setupNodeRecsEchoWithRBAC(t, pool, map[string][]string{
		"*": {},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Greater(t, resp.Meta.Count, 1, "global wildcard should see all clusters")

	clusters := map[string]bool{}
	for _, r := range resp.Data {
		clusters[r.ClusterUUID] = true
	}
	assert.True(t, len(clusters) >= 2 || clusters[cluster2],
		"global wildcard should include both clusters")
}

// --- Pagination integration tests ---

func TestGetNodeRecommendations_PaginationMeta(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'pag-cluster', 'src-p1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	seedGPUNodesForTimeslicing(t, pool, start, 7, "pag-node-1")

	app := setupNodeRecsEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing?limit=5&offset=0", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 5, resp.Meta.Limit)
	assert.Equal(t, 0, resp.Meta.Offset)
	assert.NotEmpty(t, resp.Links.First)
}

func TestGetNodeRecommendations_OrderByConfidence(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)

	cluster1 := "d1111111-1111-1111-1111-111111111111"
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'order-cluster', 'src-o1', now()) ON CONFLICT DO NOTHING`, cluster1)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for _, nd := range []string{"node-alpha", "node-beta"} {
		for i := 0; i < 7; i++ {
			for _, c := range []string{"a", "b", "c"} {
				sm := 0.10
				if nd == "node-beta" {
					sm = 0.05
				}
				testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
					IntervalStart: start.AddDate(0, 0, i), ClusterUUID: cluster1,
					Namespace: "ns-" + nd, Workload: "wl-" + c, WorkloadType: "deployment",
					ContainerName: c, GPUModelName: "NVIDIA T4", NodeName: nd,
					SMActiveAvg: sm, DRAMActiveAvg: 0.05,
					FBUsageMaxMiB: 2000, FBUsageAvgMiB: 1000,
				})
			}
		}
	}

	app := setupNodeRecsEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing?order_by%5Bconfidence%5D=asc", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.GreaterOrEqual(t, len(resp.Data), 2)

	for i := 1; i < len(resp.Data); i++ {
		assert.LessOrEqual(t, resp.Data[i-1].Confidence, resp.Data[i].Confidence,
			"results should be sorted by confidence ascending")
	}
}

func TestGetNodeRecommendations_InvalidOrderBy(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupNodeRecsEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing?order_by%5Binvalid_field%5D=asc", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetNodeRecommendations_OffsetBeyondResults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'off-cluster', 'src-off', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	seedGPUNodesForTimeslicing(t, pool, start, 7, "off-node-1")

	app := setupNodeRecsEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing?offset=1000", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Greater(t, resp.Meta.Count, 0, "count should reflect total, not paged data")
	assert.Empty(t, resp.Data, "data should be empty when offset exceeds total")
}

func setupNativeRecommendationRoutesEcho() *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/gpu/timeslicing", api.GetNodeRecommendations)
	v1.GET("/recommendations/openshift/gpu/mig", api.GetGPUMIGRecommendations)
	v1.GET("/recommendations/openshift/nodes", api.GetNodeUtilizationRecs)
	v1.GET("/recommendations/openshift/nodes/utilization", api.GetNodeUtilizationRecsLegacyPath)
	v1.GET("/recommendations/openshift/nodes/:node", api.GetNodeUtilizationDetail)
	v1.GET("/recommendations/openshift/machinesets", api.GetMachineSetRecommendations)
	return app
}

func TestRecommendationRoutes_Unauthorized(t *testing.T) {
	paths := []string{
		"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing",
		"/api/cost-management/v1/recommendations/openshift/gpu/mig",
		"/api/cost-management/v1/recommendations/openshift/nodes",
		"/api/cost-management/v1/recommendations/openshift/nodes/utilization",
		"/api/cost-management/v1/recommendations/openshift/nodes/worker-1",
		"/api/cost-management/v1/recommendations/openshift/machinesets",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			app := setupNativeRecommendationRoutesEcho()
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

func TestGetNodeUtilization_CanonicalPath_ReturnsCPURecommendationType(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'n-cluster', 'src-nu', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES ($1, $2::uuid, 'worker-1', 'medium', 'cost',
			0.1, 0.2, 0.15, 0.25, 1.1, true, false, NULL, 5, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/nodes", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	assert.Equal(t, "cpu_memory_utilization", resp.Data[0].RecommendationType)
	require.NotNil(t, resp.Data[0].RecommendationTerms["medium_term"].RecommendationEngines)
	require.NotNil(t, resp.Data[0].RecommendationTerms["medium_term"].RecommendationEngines.Cost)
	assert.Empty(t, rec.Header().Get("Deprecation"))
}

func TestGetNodeUtilization_DeprecatedAlias_WarningAndDeprecationHeader(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'n-cluster-d', 'src-nud', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES ($1, $2::uuid, 'worker-dep', 'medium', 'cost',
			0.05, 0.1, 0.1, 0.15, 1.0, false, false, NULL, 2, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/nodes/utilization", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "true", rec.Header().Get("Deprecation"))
	assert.Contains(t, rec.Header().Get("Link"), "/recommendations/openshift/nodes")

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Warnings)
	assert.Contains(t, resp.Warnings[0], "deprecated")
	require.NotEmpty(t, resp.Data)
	assert.Equal(t, "cpu_memory_utilization", resp.Data[0].RecommendationType)
}

func TestGetNodeUtilization_NestedBothEngines_SingleNode(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'n-cluster-2', 'src-nu2', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted,
			stranded_resource, pod_count, trend_slope, notification_codes,
			recommended_cpu_cores, recommended_memory_gib, node_count_reduction,
			estimated_monthly_savings_usd
		) VALUES
			($1, $2::uuid, 'worker-2', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.1, true, false, NULL, 5, 0, '{}', 4, 16, 1, 45000),
			($1, $2::uuid, 'worker-2', 'medium', 'performance',
			 0.1, 0.2, 0.15, 0.25, 1.1, true, false, NULL, 5, 0, '{}', 7, 28, 0, 12000)`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/nodes", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)

	medium := resp.Data[0].RecommendationTerms["medium_term"]
	require.NotNil(t, medium.RecommendationEngines)
	require.NotNil(t, medium.RecommendationEngines.Cost)
	require.NotNil(t, medium.RecommendationEngines.Performance)
	require.NotNil(t, medium.RecommendationEngines.Cost.EstimatedMonthlySavings)
	assert.Equal(t, "450.000000", medium.RecommendationEngines.Cost.EstimatedMonthlySavings.Value)
	require.NotNil(t, medium.RecommendationEngines.Performance.EstimatedMonthlySavings)
	assert.Equal(t, "120.000000", medium.RecommendationEngines.Performance.EstimatedMonthlySavings.Value)
}

func TestGetNodeUtilization_FilterEngine(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'n-engine-filter', 'src-nef', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted,
			stranded_resource, pod_count, trend_slope, notification_codes,
			recommended_cpu_cores, recommended_memory_gib, node_count_reduction,
			estimated_monthly_savings_usd
		) VALUES
			($1, $2::uuid, 'worker-filter', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.1, true, false, NULL, 5, 0, '{}', 4, 16, 1, 45000),
			($1, $2::uuid, 'worker-filter', 'medium', 'performance',
			 0.1, 0.2, 0.15, 0.25, 1.1, true, false, NULL, 5, 0, '{}', 7, 28, 0, 12000)`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()

	for _, tc := range []struct {
		name       string
		query      string
		wantEngine string
		wantCPU    float64
	}{
		{"filter engine cost", "?filter%5Bengine%5D=cost&limit=10", "cost", 4},
		{"filter engine performance", "?filter%5Bengine%5D=performance&limit=10", "performance", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/nodes"+tc.query, nil)
			req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var resp model.NodeUtilizationListResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			require.NotEmpty(t, resp.Data)

			medium := resp.Data[0].RecommendationTerms["medium_term"]
			require.NotNil(t, medium.RecommendationEngines)
			if tc.wantEngine == "cost" {
				require.NotNil(t, medium.RecommendationEngines.Cost)
				assert.Nil(t, medium.RecommendationEngines.Performance)
				assert.InDelta(t, tc.wantCPU, float64(medium.RecommendationEngines.Cost.RecommendedCPUCores), 0.001)
			} else {
				require.NotNil(t, medium.RecommendationEngines.Performance)
				assert.Nil(t, medium.RecommendationEngines.Cost)
				assert.InDelta(t, tc.wantCPU, float64(medium.RecommendationEngines.Performance.RecommendedCPUCores), 0.001)
			}
		})
	}
}

func TestGetNodeUtilization_FilterIdleState(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'idle-filter-cluster', 'src-idf', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES
			($1, $2::uuid, 'zombie-worker', 'medium', 'cost',
			 0.01, 0.02, 0.01, 0.02, 1.0, true, false, 'zombie', NULL, 2, 0, '{15}'),
			($1, $2::uuid, 'active-worker', 'medium', 'cost',
			 0.5, 0.6, 0.5, 0.6, 1.0, false, false, 'active', NULL, 20, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?filter[idle_state]=zombie", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "zombie-worker", resp.Data[0].Node)
	assert.Equal(t, "zombie", resp.Data[0].Classification.IdleState)

	medium := resp.Data[0].RecommendationTerms["medium_term"]
	require.NotNil(t, medium.RecommendationEngines)
	require.NotNil(t, medium.RecommendationEngines.Cost)
	notif, ok := medium.RecommendationEngines.Cost.Notifications["15"]
	require.True(t, ok, "idle/zombie nodes should emit notification code 15")
	assert.Contains(t, notif.Message, "idle")
	assert.NotContains(t, notif.Message, "MachineAutoscaler")
}

func TestGetNodeUtilization_PaginationAndSort(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'pagination-cluster', 'src-pag', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes,
			recommended_cpu_cores, recommended_memory_gib, node_count_reduction,
			estimated_monthly_savings_usd
		) VALUES
			($1, $2::uuid, 'alpha-node', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', NULL, 5, 0, '{}', 4, 16, 1, 10000),
			($1, $2::uuid, 'zulu-node', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', NULL, 5, 0, '{}', 4, 16, 1, 90000)`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?limit=1&offset=0&order_by=node&order_how=asc", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "alpha-node", resp.Data[0].Node)
	require.NotNil(t, resp.Meta)
	assert.Equal(t, 2, resp.Meta.Count)
	assert.NotEmpty(t, resp.Links.Next)
}

func TestGetNodeUtilization_InstanceTypeInResponse(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'instance-type-cluster', 'src-it', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes,
			recommended_cpu_cores, recommended_memory_gib, node_count_reduction,
			estimated_monthly_savings_usd, instance_type
		) VALUES
			($1, $2::uuid, 'worker-m5', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.1, true, false, 'active', NULL, 5, 0, '{}', 4, 16, 1, 45000, 'm5.xlarge')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/nodes", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	assert.Equal(t, "m5.xlarge", resp.Data[0].InstanceType)
}

func setupNodeUtilizationEchoWithRBAC(t *testing.T, pool *pgxpool.Pool, perms map[string][]string) *echo.Echo {
	t.Helper()
	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = true
	t.Cleanup(func() { cfg.RBACEnabled = orig })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user.permissions", perms)
			return next(c)
		}
	})
	v1.GET("/recommendations/openshift/nodes", api.GetNodeUtilizationRecs)
	v1.GET("/recommendations/openshift/nodes/:node", api.GetNodeUtilizationDetail)
	return app
}

func TestGetNodeUtilization_RBAC_FiltersByNode(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'rbac-node-util-cluster', 'src-rnu', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES
			($1, $2::uuid, 'allowed-node', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', NULL, 5, 0, '{}'),
			($1, $2::uuid, 'denied-node', 'medium', 'cost',
			 0.2, 0.3, 0.25, 0.35, 1.0, true, false, 'active', NULL, 8, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNodeUtilizationEchoWithRBAC(t, pool, map[string][]string{
		"openshift.cluster": {"*"},
		"openshift.node":    {"allowed-node"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/nodes", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "allowed-node", resp.Data[0].Node)
}

func TestGetNodeUtilization_CSVExport(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'csv-cluster', 'src-csv', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes,
			recommended_cpu_cores, recommended_memory_gib, estimated_monthly_savings_usd,
			instance_type, machineset_name
		) VALUES
			($1, $2::uuid, 'csv-worker', 'medium', 'cost',
			 0.1, 0.42, 0.15, 0.51, 1.1, true, false, 'active', 'cpu', 5, 0, '{}', 4, 16, 12500, 'm5.large', 'worker-ms'),
			($1, $2::uuid, 'other-worker', 'medium', 'cost',
			 0.5, 0.6, 0.5, 0.6, 1.0, false, false, 'active', NULL, 20, 0, '{}', 8, 32, 50000, 'm5.2xlarge', 'other-ms')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?format=csv&filter[stranded_resource]=cpu", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), ".csv")

	body := rec.Body.String()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "expected header and at least one data row")
	assert.Contains(t, lines[0], "node")
	assert.Contains(t, lines[0], "estimated_monthly_savings")
	assert.Contains(t, body, "csv-worker")
	assert.NotContains(t, body, "other-worker")
}

func TestGetNodeUtilization_FilterStrandedResource(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'stranded-filter-cluster', 'src-sf', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES
			($1, $2::uuid, 'stranded-cpu', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', 'cpu', 5, 0, '{}'),
			($1, $2::uuid, 'no-stranded', 'medium', 'cost',
			 0.5, 0.6, 0.5, 0.6, 1.0, false, false, 'active', NULL, 20, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?filter[stranded_resource]=none", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "no-stranded", resp.Data[0].Node)
}

func TestGetNodeUtilization_FilterInstanceType(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'instance-filter-cluster', 'src-if', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes, instance_type
		) VALUES
			($1, $2::uuid, 'm5-node', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', NULL, 5, 0, '{}', 'm5.xlarge'),
			($1, $2::uuid, 'r5-node', 'medium', 'cost',
			 0.5, 0.6, 0.5, 0.6, 1.0, false, false, 'active', NULL, 20, 0, '{}', 'r5.xlarge')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?filter[instance_type]=m5.xlarge", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "m5-node", resp.Data[0].Node)
	assert.Equal(t, "m5.xlarge", resp.Data[0].InstanceType)
}

func TestGetNodeUtilization_FilterMachinesetName(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'ms-filter-cluster', 'src-msf', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes, machineset_name
		) VALUES
			($1, $2::uuid, 'ms-a-node', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', NULL, 5, 0, '{}', 'worker-a'),
			($1, $2::uuid, 'ms-b-node', 'medium', 'cost',
			 0.5, 0.6, 0.5, 0.6, 1.0, false, false, 'active', NULL, 20, 0, '{}', 'worker-b')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?filter[machineset_name]=worker-a", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "ms-a-node", resp.Data[0].Node)
}

func TestGetNodeUtilizationDetail_Returns200WithNestedTerms(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'detail-cluster', 'src-nd', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, pod_capacity, trend_slope, notification_codes,
			instance_type, machineset_name, suggested_instance_type, instance_type_reason
		) VALUES
			($1, $2::uuid, 'worker-detail', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', 'cpu', 85, 110, 0, '{12}',
			 'm5.xlarge', 'worker-us-east-1a', 'c5.xlarge', 'CPU-stranded node; c5.xlarge in same cluster'),
			($1, $2::uuid, 'worker-detail', 'short', 'cost',
			 0.2, 0.3, 0.2, 0.3, 1.0, false, false, 'active', NULL, 85, 110, 0, '{}',
			 'm5.xlarge', 'worker-us-east-1a', NULL, NULL)`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes/worker-detail", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var detail model.NodeUtilizationDetailRec
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	assert.Equal(t, "worker-detail", detail.Node)
	assert.Equal(t, "m5.xlarge", detail.InstanceType)
	assert.Equal(t, "worker-us-east-1a", detail.MachineSetName)
	assert.Equal(t, "c5.xlarge", detail.SuggestedInstanceType)
	require.NotNil(t, detail.PodCapacity)
	assert.EqualValues(t, 110, *detail.PodCapacity)
	require.NotNil(t, detail.RecommendationTerms["medium_term"].RecommendationEngines)
	require.NotNil(t, detail.RecommendationTerms["short_term"].RecommendationEngines)
}

func TestGetNodeUtilizationDetail_UnknownNode404(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'detail-miss', 'src-ndm', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes/no-such-node", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetNodeUtilizationDetail_RBAC_DeniedNode404(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'detail-rbac', 'src-ndr', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES ($1, $2::uuid, 'secret-node', 'medium', 'cost',
			0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', NULL, 5, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNodeUtilizationEchoWithRBAC(t, pool, map[string][]string{
		"openshift.cluster": {"*"},
		"openshift.node":    {"allowed-node"},
	})
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes/secret-node", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetNodeList_FilterIsUnderutilized(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'underutil-filter-cluster', 'src-uf', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES
			($1, $2::uuid, 'underutil-yes', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', NULL, 5, 0, '{}'),
			($1, $2::uuid, 'underutil-no', 'medium', 'cost',
			 0.5, 0.6, 0.5, 0.6, 1.0, false, false, 'active', NULL, 20, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?filter[is_underutilized]=true", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "underutil-yes", resp.Data[0].Node)
	assert.True(t, resp.Data[0].Classification.IsUnderutilized)
}

func TestGetNodeList_FilterIsOvercommitted(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'overcommit-filter-cluster', 'src-of', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES
			($1, $2::uuid, 'overcommit-yes', 'medium', 'cost',
			 0.5, 0.6, 0.5, 0.6, 2.0, false, true, 'active', NULL, 20, 0, '{}'),
			($1, $2::uuid, 'overcommit-no', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', NULL, 5, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?filter[is_overcommitted]=true", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "overcommit-yes", resp.Data[0].Node)
	assert.True(t, resp.Data[0].Classification.IsOvercommitted)
}

func TestGetNodeList_FilterTag(t *testing.T) {
	withTagsEnabled(t)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'tag-filter-cluster', 'src-tf', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
		VALUES ($1, $2, $3, 'w1', 'Deployment', 'c1', '{"environment":"production"}'::jsonb)
		ON CONFLICT (org_id, namespace, workload, container_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, is_underutilized, is_overcommitted,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95, cpu_overcommit_ratio, pod_count, trend_slope,
			notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES ($1, $2, 'node-prod', 'medium', 'cost', true, false, 10, 20, 10, 20, 1, 1, 0, '{}', 30000, now()),
		       ($1, $2, 'node-other', 'medium', 'cost', false, false, 50, 60, 50, 60, 1, 5, 0, '{}', 10000, now())
		ON CONFLICT DO NOTHING`, testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?filter%5Btag%3Aenvironment%5D=production", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "node-prod", resp.Data[0].Node)
}

func TestGetNodeList_FilterByNodeName(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'node-name-filter-cluster', 'src-nnf', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES
			($1, $2::uuid, 'target-node-filter', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', NULL, 5, 0, '{}'),
			($1, $2::uuid, 'other-node-filter', 'medium', 'cost',
			 0.5, 0.6, 0.5, 0.6, 1.0, false, false, 'active', NULL, 20, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?filter%5Bnode%5D=target-node-filter", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "target-node-filter", resp.Data[0].Node)
}

func TestGetNodeList_FilterTermShort(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'term-short-filter-cluster', 'src-tsf', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES
			($1, $2::uuid, 'term-node', 'short', 'cost',
			 0.55, 0.6, 0.5, 0.6, 1.0, false, false, 'active', NULL, 20, 0, '{}'),
			($1, $2::uuid, 'term-node', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', NULL, 5, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?filter%5Bterm%5D=short", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "term-node", resp.Data[0].Node)
	assert.False(t, resp.Data[0].Classification.IsUnderutilized)
	require.NotNil(t, resp.Data[0].RecommendationTerms["short_term"])
	require.NotNil(t, resp.Data[0].RecommendationTerms["short_term"].RecommendationEngines.Cost)
	assert.NotContains(t, resp.Data[0].RecommendationTerms, "medium_term")
}

func TestGetNodeList_FilterTermLong(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'term-long-filter-cluster', 'src-tlf', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES
			($1, $2::uuid, 'term-node-long', 'long', 'cost',
			 0.45, 0.5, 0.4, 0.45, 1.2, false, true, 'active', NULL, 15, 0, '{}'),
			($1, $2::uuid, 'term-node-long', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, true, false, 'active', NULL, 5, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/nodes?filter%5Bterm%5D=long", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "term-node-long", resp.Data[0].Node)
	assert.True(t, resp.Data[0].Classification.IsOvercommitted)
	require.NotNil(t, resp.Data[0].RecommendationTerms["long_term"])
	require.NotNil(t, resp.Data[0].RecommendationTerms["long_term"].RecommendationEngines.Cost)
	assert.NotContains(t, resp.Data[0].RecommendationTerms, "medium_term")
}
