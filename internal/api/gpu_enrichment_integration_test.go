package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestEnrichWithGPU_ReadsPersistedCrossRefs(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := "org-gpu-enrich-persist"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "enrich-persist-node"

	database.Pool = pool
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.Pool = nil; database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, orgID)
	require.NoError(t, err)
	var tenantID int
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM rh_accounts WHERE org_id = $1`, orgID).Scan(&tenantID))
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2, 'enrich-persist', 'src-ep', now()) ON CONFLICT DO NOTHING`, tenantID, clusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	containers := []struct{ ns, wl, cn string; sm float64 }{
		{"ml-ns", "train-a", "gpu-c1", 0.12},
		{"ml-ns", "train-b", "gpu-c2", 0.08},
		{"ml-ns", "train-c", "gpu-c3", 0.15},
	}
	for _, c := range containers {
		testutil.SeedGPURecommendationSet(t, pool, orgID, clusterUUID, c.ns, c.wl, c.cn, "medium")
		testutil.SeedOrgContainerKey(t, pool, orgID, clusterUUID, c.ns, c.wl, "deployment", c.cn)
		for i := 0; i < 7; i++ {
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				IntervalStart: start.AddDate(0, 0, i), ClusterUUID: clusterUUID,
				Namespace: c.ns, Workload: c.wl, WorkloadType: "deployment",
				ContainerName: c.cn, GPUModelName: "NVIDIA T4", NodeName: nodeName,
				SMActiveAvg: c.sm, DRAMActiveAvg: 0.05, FBUsageMaxMiB: 2000, FBUsageAvgMiB: 1000,
			})
		}
	}
	require.NoError(t, engine.MarkContainersWithGPU(ctx, pool, orgID, clusterUUID))
	require.NoError(t, engine.StoreGPUClassifications(ctx, pool, orgID, clusterUUID, engine.DefaultTermsForPlugin("gpu"), nil))
	require.NoError(t, engine.ComputeAndPersistNodeGPUTimeSlicingRecs(
		ctx, pool, orgID, clusterUUID, engine.DefaultTermsForPlugin("gpu"), nil))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?filter%5Bproject%5D=ml-ns", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var response struct {
		Data []model.DetailResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.NotEmpty(t, response.Data)

	var gpuBlock *model.GPURecommendation
	for _, d := range response.Data {
		if d.Container == "gpu-c2" && d.GPU != nil {
			gpuBlock = d.GPU["medium"]
			break
		}
	}
	require.NotNil(t, gpuBlock, "expected GPU block for gpu-c2")
	require.NotNil(t, gpuBlock.TimeSlicingNode)
	assert.Equal(t, nodeName, *gpuBlock.TimeSlicingNode)
	require.NotNil(t, gpuBlock.TimeSlicingReplicas)
	assert.Greater(t, *gpuBlock.TimeSlicingReplicas, 0)
}

func TestEnrichWithGPU_FallbackWhenCrossRefsEmpty(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := "org-gpu-enrich-fallback"
	clusterUUID := "e2222222-2222-2222-2222-222222222222"
	nodeName := "enrich-fallback-node"

	database.Pool = pool
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.Pool = nil; database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, orgID)
	require.NoError(t, err)
	var tenantID int
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM rh_accounts WHERE org_id = $1`, orgID).Scan(&tenantID))
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2, 'enrich-fallback', 'src-ef', now()) ON CONFLICT DO NOTHING`, tenantID, clusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	containers := []struct{ ns, wl, cn string; sm float64 }{
		{"ml-ns", "train-a", "gpu-c1", 0.12},
		{"ml-ns", "train-b", "gpu-c2", 0.08},
		{"ml-ns", "train-c", "gpu-c3", 0.15},
	}
	for _, c := range containers {
		testutil.SeedGPURecommendationSet(t, pool, orgID, clusterUUID, c.ns, c.wl, c.cn, "medium")
		testutil.SeedOrgContainerKey(t, pool, orgID, clusterUUID, c.ns, c.wl, "deployment", c.cn)
		for i := 0; i < 7; i++ {
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				IntervalStart: start.AddDate(0, 0, i), ClusterUUID: clusterUUID,
				Namespace: c.ns, Workload: c.wl, WorkloadType: "deployment",
				ContainerName: c.cn, GPUModelName: "NVIDIA T4", NodeName: nodeName,
				SMActiveAvg: c.sm, DRAMActiveAvg: 0.05, FBUsageMaxMiB: 2000, FBUsageAvgMiB: 1000,
			})
		}
	}
	require.NoError(t, engine.MarkContainersWithGPU(ctx, pool, orgID, clusterUUID))
	require.NoError(t, engine.StoreGPUClassifications(ctx, pool, orgID, clusterUUID, engine.DefaultTermsForPlugin("gpu"), nil))

	// No ComputeAndPersist — cross-ref columns empty; enrichment falls back to engine.
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?filter%5Bproject%5D=ml-ns", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var response struct {
		Data []model.DetailResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.NotEmpty(t, response.Data)

	found := false
	for _, d := range response.Data {
		if d.Container == "gpu-c1" && d.GPU != nil {
			gpuBlock := d.GPU["medium"]
			if gpuBlock != nil && gpuBlock.TimeSlicingNode != nil {
				found = true
				assert.Equal(t, nodeName, *gpuBlock.TimeSlicingNode)
				require.NotNil(t, gpuBlock.TimeSlicingReplicas)
				assert.Greater(t, *gpuBlock.TimeSlicingReplicas, 0)
			}
		}
	}
	assert.True(t, found, "fallback engine should populate time_slicing cross-ref fields")
}
