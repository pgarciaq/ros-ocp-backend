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
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestGetMachineSetRecommendations_EmptyWhenNoMachineSetData(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'ms-empty-cluster', 'src-mse', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, category, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES
			($1, $2::uuid, 'bare-node', 'medium', 'cost',
			 0.1, 0.2, 0.15, 0.25, 1.0, 'underutilized', 'active', NULL, 5, 0, '{}')`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/machinesets", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.MachineSetRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
}

func TestGetMachineSetRecommendations_AggregatesByMachineSet(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'production', 'src-msa', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, category, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes,
			machineset_name, instance_type, node_count_reduction, estimated_savings_cents
		) VALUES
			($1, $2::uuid, 'worker-0', 'medium', 'cost',
			 0.1, 0.40, 0.15, 0.60, 1.0, 'underutilized', 'active', NULL, 5, 0, '{}',
			 'worker-us-east-1a', 'm5.xlarge', 1, 120000),
			($1, $2::uuid, 'worker-1', 'medium', 'cost',
			 0.1, 0.44, 0.15, 0.64, 1.0, 'underutilized', 'active', NULL, 5, 0, '{}',
			 'worker-us-east-1a', 'm5.xlarge', 1, 120000),
			($1, $2::uuid, 'worker-2', 'medium', 'cost',
			 0.5, 0.55, 0.5, 0.55, 1.0, 'optimized', 'active', NULL, 20, 0, '{}',
			 'worker-us-east-1b', 'm5.xlarge', 0, 50000)`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/machinesets", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp model.MachineSetRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 2, resp.Meta.Count)
	require.Len(t, resp.Data, 2)

	msA := resp.Data[0]
	assert.Equal(t, "worker-us-east-1a", msA.MachineSetName)
	assert.Equal(t, testutil.TestClusterUUID, msA.ClusterUUID)
	assert.Equal(t, "production", msA.ClusterAlias)
	assert.Equal(t, "m5.xlarge", msA.InstanceType)
	assert.Equal(t, 2, msA.CurrentNodeCount)
	assert.Equal(t, 2, msA.ExcessNodes)
	assert.Equal(t, 0, msA.RecommendedNodeCount)
	require.NotNil(t, msA.TotalMonthlySavings)
	assert.Equal(t, "2400.00", msA.TotalMonthlySavings.Value)
	assert.Equal(t, "USD", msA.TotalMonthlySavings.Units)
	assert.Equal(t, "USD", resp.Meta.Currency)
	assert.InDelta(t, 0.42, msA.AvgCPUUtilization, 0.01)
	assert.InDelta(t, 0.62, msA.AvgMemoryUtilization, 0.01)
	assert.ElementsMatch(t, []string{"worker-0", "worker-1"}, msA.Nodes)
}

func TestGetMachineSetRecommendations_Pagination(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'pag-ms', 'src-msp', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	for i, name := range []string{"ms-a", "ms-b", "ms-c"} {
		_, err = pool.Exec(ctx, `
			INSERT INTO node_recommendations (
				org_id, cluster_uuid, node, term, engine,
				cpu_util_p95, mem_util_p95, machineset_name, estimated_savings_cents
			) VALUES ($1, $2::uuid, $3, 'medium', 'cost', 0.1, 0.1, $4, $5)`,
			testutil.TestOrgID, testutil.TestClusterUUID, "node-"+name, name, int64((3-i)*10000))
		require.NoError(t, err)
	}

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/machinesets?limit=1&offset=1", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.MachineSetRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 3, resp.Meta.Count)
	assert.Equal(t, 1, resp.Meta.Limit)
	assert.Equal(t, 1, resp.Meta.Offset)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "ms-b", resp.Data[0].MachineSetName)
}

func TestGetMachineSetRecommendations_FilterByCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	otherCluster := "a1111111-1111-1111-1111-111111111111"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'cluster-a', 'src-a', now()),
		        (1, $2, 'cluster-b', 'src-b', now())
		 ON CONFLICT DO NOTHING`, testutil.TestClusterUUID, otherCluster)
	require.NoError(t, err)
	for _, cl := range []struct{ uuid, ms string }{
		{testutil.TestClusterUUID, "ms-on-a"},
		{otherCluster, "ms-on-b"},
	} {
		_, err = pool.Exec(ctx, `
			INSERT INTO node_recommendations (
				org_id, cluster_uuid, node, term, engine, cpu_util_p95, mem_util_p95, machineset_name
			) VALUES ($1, $2::uuid, 'n1', 'medium', 'cost', 0.1, 0.1, $3)`,
			testutil.TestOrgID, cl.uuid, cl.ms)
		require.NoError(t, err)
	}

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/machinesets?filter[cluster]="+testutil.TestClusterUUID, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.MachineSetRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "ms-on-a", resp.Data[0].MachineSetName)
}

func TestGetMachineSetRecommendations_RBAC_FiltersByCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	cluster1 := testutil.TestClusterUUID
	cluster2 := "b2222222-2222-2222-2222-222222222222"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'rbac-ms-1', 'src-r1', now()),
		        (1, $2, 'rbac-ms-2', 'src-r2', now())
		 ON CONFLICT DO NOTHING`, cluster1, cluster2)
	require.NoError(t, err)
	for _, cl := range []struct{ uuid, node string }{
		{cluster1, "allowed-node"},
		{cluster2, "denied-node"},
	} {
		_, err = pool.Exec(ctx, `
			INSERT INTO node_recommendations (
				org_id, cluster_uuid, node, term, engine, cpu_util_p95, mem_util_p95,
				machineset_name, estimated_savings_cents
			) VALUES ($1, $2::uuid, $3, 'medium', 'cost', 0.1, 0.1, 'shared-ms', 10000)`,
			testutil.TestOrgID, cl.uuid, cl.node)
		require.NoError(t, err)
	}

	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = true
	t.Cleanup(func() { cfg.RBACEnabled = orig })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user.permissions", map[string][]string{
				"openshift.cluster": {cluster1},
				"openshift.node":    {"*"},
			})
			return next(c)
		}
	})
	v1.GET("/recommendations/openshift/machinesets", api.GetMachineSetRecommendations)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/machinesets", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.MachineSetRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, cluster1, resp.Data[0].ClusterUUID)
	assert.Equal(t, []string{"allowed-node"}, resp.Data[0].Nodes)
}

func TestGetMachineSetRecommendations_RBAC_FiltersByNode(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'rbac-node-ms', 'src-rn', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	for _, node := range []string{"allowed-node", "other-node"} {
		_, err = pool.Exec(ctx, `
			INSERT INTO node_recommendations (
				org_id, cluster_uuid, node, term, engine, cpu_util_p95, mem_util_p95,
				machineset_name, node_count_reduction
			) VALUES ($1, $2::uuid, $3, 'medium', 'cost', 0.1, 0.1, 'worker-ms', 1)`,
			testutil.TestOrgID, testutil.TestClusterUUID, node)
		require.NoError(t, err)
	}

	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = true
	t.Cleanup(func() { cfg.RBACEnabled = orig })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user.permissions", map[string][]string{
				"openshift.cluster": {"*"},
				"openshift.node":    {"allowed-node"},
			})
			return next(c)
		}
	})
	v1.GET("/recommendations/openshift/machinesets", api.GetMachineSetRecommendations)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/machinesets", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.MachineSetRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, 1, resp.Data[0].CurrentNodeCount)
	assert.Equal(t, []string{"allowed-node"}, resp.Data[0].Nodes)
}

// Same cluster UUID registered under two tenants (e.g. a cloned cluster
// re-registered under a different account). #525: the clusters alias join
// must predicate c.org_id so orgA never sees orgB's alias. Pre-fix the join
// fans out to both tenants' rows (doubling aggregates) and the alias is
// whichever row Postgres happens to return.
func TestGetMachineSetRecommendations_CollidingClusterUUIDUsesOwnOrgAlias(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	const (
		otherOrgID = "org-machineset-collide-b"
		sharedUUID = "22222222-2222-2222-2222-222222222222"
	)

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1), (2, $2) ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, otherOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, org_id, last_reported_at)
		VALUES (1, $1::uuid, 'alias-tenant-a', 'src-collide-a', $2, now()),
		       (2, $1::uuid, 'alias-tenant-b', 'src-collide-b', $3, now())
		ON CONFLICT DO NOTHING`, sharedUUID, testutil.TestOrgID, otherOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, category, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes,
			machineset_name, instance_type, node_count_reduction, estimated_savings_cents
		) VALUES
			($1, $2::uuid, 'worker-0', 'medium', 'cost',
			 0.1, 0.40, 0.15, 0.60, 1.0, 'underutilized', 'active', NULL, 5, 0, '{}',
			 'worker-us-east-1a', 'm5.xlarge', 1, 120000)`,
		testutil.TestOrgID, sharedUUID)
	require.NoError(t, err)

	app := setupNativeRecommendationRoutesEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/machinesets", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp model.MachineSetRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, sharedUUID, resp.Data[0].ClusterUUID)
	assert.Equal(t, "alias-tenant-a", resp.Data[0].ClusterAlias)
	assert.Equal(t, 1, resp.Data[0].CurrentNodeCount)
}
