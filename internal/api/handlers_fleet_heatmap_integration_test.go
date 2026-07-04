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
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func seedHeatmapData(t *testing.T, ctx context.Context) {
	t.Helper()
	pool := database.GetPool()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'heatmap-cluster', 'src-hm', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM node_recommendations WHERE org_id = $1`, testutil.TestOrgID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine,
			cpu_util_p95, mem_util_p95, idle_state, machineset_name, instance_type,
			node_count_reduction, estimated_savings_cents, updated_at)
		VALUES
			($1, $2, 'node-idle',   'medium', 'cost', 0.03, 0.05, 'idle',   'ms-infra',   'm5.large',  1, 5000, now()),
			($1, $2, 'node-low',    'medium', 'cost', 0.20, 0.15, 'active', 'ms-infra',   'm5.large',  0, 1000, now()),
			($1, $2, 'node-mod',    'medium', 'cost', 0.50, 0.55, 'active', 'ms-workers', 'm5.xlarge', 0, 0,    now()),
			($1, $2, 'node-healthy','medium', 'cost', 0.75, 0.70, 'active', 'ms-workers', 'm5.xlarge', 0, 0,    now()),
			($1, $2, 'node-hot',    'medium', 'cost', 0.92, 0.88, 'active', '',           'm5.2xlarge',0, 0,    now()),
			($1, $2, 'node-zombie', 'medium', 'cost', 0.40, 0.30, 'zombie', 'ms-infra',   'm5.large',  1, 3000, now())
	`, testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)
}

func heatmapApp() *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/fleet-heatmap", api.GetFleetHeatmap)
	return app
}

func TestGetFleetHeatmap_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
		fleetheatmap.ResetForTest()
	})

	seedHeatmapData(t, ctx)

	app := heatmapApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp api.FleetHeatmapResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, 6, resp.Meta.Count)
	assert.Equal(t, "cpu", resp.Meta.Metric)
	assert.Equal(t, "medium", resp.Meta.Term)
	assert.Equal(t, "cost", resp.Meta.Engine)
	assert.NotEmpty(t, resp.Meta.LatestUpdate)
	assert.Contains(t, resp.Meta.DataWindow, "7 days")

	bandMap := make(map[string]string)
	for _, n := range resp.Data {
		bandMap[n.Node] = n.UtilizationBand
	}
	assert.Equal(t, "idle", bandMap["node-idle"])
	assert.Equal(t, "low", bandMap["node-low"])
	assert.Equal(t, "moderate", bandMap["node-mod"])
	assert.Equal(t, "healthy", bandMap["node-healthy"])
	assert.Equal(t, "hot", bandMap["node-hot"])
	assert.Equal(t, "idle", bandMap["node-zombie"], "zombie nodes should be 'idle' band")
}

func TestGetFleetHeatmap_MetricParam(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
		fleetheatmap.ResetForTest()
	})

	seedHeatmapData(t, ctx)

	app := heatmapApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap?metric=memory", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.FleetHeatmapResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, "memory", resp.Meta.Metric)

	bandMap := make(map[string]string)
	for _, n := range resp.Data {
		bandMap[n.Node] = n.UtilizationBand
	}
	assert.Equal(t, "idle", bandMap["node-idle"], "mem_util_p95=0.05 with idle state")
	assert.Equal(t, "low", bandMap["node-low"], "mem_util_p95=0.15 with active state → low")

	// node-low: mem_util_p95=0.15, idle_state=active → band is "low"
	assert.Equal(t, "low", bandMap["node-low"])
}

func TestGetFleetHeatmap_EmptyState(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
		fleetheatmap.ResetForTest()
	})

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'empty-cluster', 'src-empty', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM node_recommendations WHERE org_id = $1`, testutil.TestOrgID)
	require.NoError(t, err)

	app := heatmapApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.FleetHeatmapResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
}

func TestGetFleetHeatmap_InvalidMetric(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := heatmapApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap?metric=disk", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetFleetHeatmap_InvalidEngine(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := heatmapApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap?filter[engine]=invalid", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetFleetHeatmap_InvalidTerm(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := heatmapApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap?filter[term]=bogus", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetFleetHeatmap_ValidTerms(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := heatmapApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	for _, term := range []string{"short", "medium", "long"} {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap?filter[term]="+term, nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "term=%s should be accepted", term)
	}
}

func TestGetFleetHeatmap_CacheHit(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
		fleetheatmap.ResetForTest()
	})

	seedHeatmapData(t, ctx)

	app := heatmapApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	// First request populates cache
	req1 := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap", nil)
	req1.Header.Set("X-Rh-Identity", identityHeader)
	rec1 := httptest.NewRecorder()
	app.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	// Delete data from DB
	_, err := pool.Exec(ctx, `DELETE FROM node_recommendations WHERE org_id = $1`, testutil.TestOrgID)
	require.NoError(t, err)

	// Second request should return cached response (still 6 nodes)
	req2 := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap", nil)
	req2.Header.Set("X-Rh-Identity", identityHeader)
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp api.FleetHeatmapResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp))
	assert.Equal(t, 6, resp.Meta.Count, "cached response should still have 6 nodes")

	// Invalidation should cause a cache miss
	fleetheatmap.InvalidateOrg(testutil.TestOrgID)
	req3 := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap", nil)
	req3.Header.Set("X-Rh-Identity", identityHeader)
	rec3 := httptest.NewRecorder()
	app.ServeHTTP(rec3, req3)
	require.Equal(t, http.StatusOK, rec3.Code)

	var resp3 api.FleetHeatmapResponse
	require.NoError(t, json.Unmarshal(rec3.Body.Bytes(), &resp3))
	assert.Equal(t, 0, resp3.Meta.Count, "after invalidation, should query DB (no data)")
}

func TestGetFleetHeatmap_ClusterAlias(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
		fleetheatmap.ResetForTest()
	})

	seedHeatmapData(t, ctx)

	app := heatmapApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.FleetHeatmapResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	for _, n := range resp.Data {
		assert.Equal(t, "heatmap-cluster", n.ClusterAlias, "cluster_alias should be resolved from clusters table")
		break
	}
}
