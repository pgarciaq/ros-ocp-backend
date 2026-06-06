package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func insertSnapshotRecommendationFull(
	t *testing.T,
	orgID, clusterUUID, namespace, snapshotName, recType string,
	ageDays int,
	restoreSize int64,
	costCents *int64,
) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name,
			recommendation_type, age_days, restore_size_bytes,
			estimated_cost_cents, creation_timestamp, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW() - ($6::int * INTERVAL '1 day'), NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, snapshot_name)
		DO UPDATE SET recommendation_type = EXCLUDED.recommendation_type,
			age_days = EXCLUDED.age_days,
			restore_size_bytes = EXCLUDED.restore_size_bytes,
			estimated_cost_cents = EXCLUDED.estimated_cost_cents,
			updated_at = NOW()`,
		orgID, clusterUUID, namespace, snapshotName, recType, ageDays, restoreSize, costCents,
	)
	require.NoError(t, err)
}

func setupSnapshotSummaryEcho(pool *pgxpool.Pool) *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/snapshots/summary", api.GetSnapshotSummary)
	return app
}

func setupSnapshotSummaryEchoWithRBAC(t *testing.T, perms map[string][]string) *echo.Echo {
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
	v1.GET("/recommendations/openshift/snapshots/summary", api.GetSnapshotSummary)
	return app
}

func TestGetSnapshotSummary_Empty(t *testing.T) {
	orgID := "org-snap-sum-empty-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)

	app := setupSnapshotSummaryEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/snapshots/summary", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp api.SnapshotSummaryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
}

func TestGetSnapshotSummary_MultipleNamespaces(t *testing.T) {
	orgID := "org-snap-sum-ns-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	gib := int64(1024 * 1024 * 1024)
	cost10 := int64(1000)
	cost5 := int64(500)
	insertSnapshotRecommendationFull(t, orgID, testutil.TestClusterUUID, "apps", "snap-apps-1", "orphaned", 30, gib, &cost10)
	insertSnapshotRecommendationFull(t, orgID, testutil.TestClusterUUID, "apps", "snap-apps-2", "stale", 60, gib, &cost10)
	insertSnapshotRecommendationFull(t, orgID, testutil.TestClusterUUID, "data", "snap-data-1", "never_restored", 90, 2*gib, &cost5)
	insertSnapshotRecommendationFull(t, orgID, testutil.TestClusterUUID, "kube-system", "snap-ks-1", "active", 5, gib, nil)
	insertSnapshotRecommendationFull(t, orgID, testutil.TestClusterUUID, "kube-system", "snap-ks-2", "managed", 40, gib, &cost5)

	app := setupSnapshotSummaryEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/snapshots/summary?limit=50", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotSummaryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 3, resp.Meta.Count)
	require.Len(t, resp.Data, 3)

	byNS := map[string]api.SnapshotSummaryResponse{}
	for _, row := range resp.Data {
		byNS[row.Namespace] = row
	}

	apps := byNS["apps"]
	assert.Equal(t, 2, apps.SnapshotCount)
	assert.Equal(t, 2, apps.ActionableSnapshotCount)
	assert.Equal(t, 1, apps.CountsByType["orphaned"])
	assert.Equal(t, 1, apps.CountsByType["stale"])
	assert.InDelta(t, 20.0, apps.ReclaimableMonthlyHoldingCostUSD, 0.01)
	assert.Equal(t, 0, apps.CountsByType["active"])
	assert.Equal(t, 0, apps.CountsByType["managed"])

	data := byNS["data"]
	assert.Equal(t, 1, data.ActionableSnapshotCount)
	assert.Equal(t, 1, data.CountsByType["never_restored"])

	ks := byNS["kube-system"]
	assert.Equal(t, 2, ks.SnapshotCount)
	assert.Equal(t, 0, ks.ActionableSnapshotCount)
	assert.Equal(t, 1, ks.CountsByType["active"])
	assert.Equal(t, 1, ks.CountsByType["managed"])
	assert.Equal(t, float64(0), ks.ReclaimableMonthlyHoldingCostUSD)
}

func TestGetSnapshotSummary_FilterByCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := testutil.TestOrgID
	clusterAllowed := testutil.TestClusterUUID
	clusterOther := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'allowed', 1, NOW()),
		       (1, $2, 'other', 2, NOW())
		ON CONFLICT DO NOTHING`, clusterAllowed, clusterOther)
	require.NoError(t, err)

	insertSnapshotRecommendation(t, orgID, clusterAllowed, "apps", "snap-cl-a", "orphaned", 10)
	insertSnapshotRecommendation(t, orgID, clusterOther, "apps", "snap-cl-b", "orphaned", 10)

	app := setupSnapshotSummaryEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/summary?filter%5Bcluster%5D="+clusterAllowed,
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.SnapshotSummaryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, clusterAllowed, resp.Data[0].ClusterUUID)
}

func TestGetSnapshotSummary_FilterByRecommendationType(t *testing.T) {
	orgID := "org-snap-sum-type-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "apps", "snap-stale-1", "stale", 120)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "apps", "snap-active-1", "active", 1)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "data", "snap-orphan-1", "orphaned", 45)

	app := setupSnapshotSummaryEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/summary?filter%5Brecommendation_type%5D=stale&limit=50",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotSummaryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "apps", resp.Data[0].Namespace)
	assert.Equal(t, 1, resp.Data[0].SnapshotCount)
	assert.Equal(t, 1, resp.Data[0].ActionableSnapshotCount)
	assert.Equal(t, 1, resp.Data[0].CountsByType["stale"])
	assert.Equal(t, 0, resp.Data[0].CountsByType["active"])
	assert.Equal(t, 0, resp.Data[0].CountsByType["orphaned"])
}

func TestGetSnapshotSummary_FilterByProject(t *testing.T) {
	orgID := "org-snap-sum-proj-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "payments-prod", "snap-exact", "orphaned", 10)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "payments-staging", "snap-wild", "stale", 20)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "frontend", "snap-fe", "orphaned", 15)

	app := setupSnapshotSummaryEcho(pool)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/summary?filter%5Bproject%5D=payments-prod",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var exact api.SnapshotSummaryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &exact))
	require.Equal(t, 1, exact.Meta.Count)
	assert.Equal(t, "payments-prod", exact.Data[0].Namespace)

	req2 := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/summary?filter%5Bproject%5D=payments-*",
		nil,
	)
	req2.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var wild api.SnapshotSummaryListResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &wild))
	require.Equal(t, 2, wild.Meta.Count)
}

func TestGetSnapshotSummary_Pagination(t *testing.T) {
	orgID := "org-snap-sum-page-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	cost := int64(100)
	for i, ns := range []string{"aaa", "bbb", "ccc"} {
		insertSnapshotRecommendationFull(t, orgID, testutil.TestClusterUUID, ns, "snap-"+ns, "orphaned", 10+i, 1024, &cost)
	}

	app := setupSnapshotSummaryEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/summary?limit=2&offset=0&order_by=snapshot_count&order_how=asc",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var page1 api.SnapshotSummaryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page1))
	assert.Equal(t, 3, page1.Meta.Count)
	assert.Len(t, page1.Data, 2)
}

func TestGetSnapshotSummary_OrderByReclaimableCost(t *testing.T) {
	orgID := "org-snap-sum-ord-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	low := int64(100)
	high := int64(10000)
	insertSnapshotRecommendationFull(t, orgID, testutil.TestClusterUUID, "low-ns", "snap-low", "orphaned", 10, 1024, &low)
	insertSnapshotRecommendationFull(t, orgID, testutil.TestClusterUUID, "high-ns", "snap-high", "stale", 10, 1024, &high)

	app := setupSnapshotSummaryEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/summary?order_by=reclaimable_monthly_holding_cost_usd&order_how=desc",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.SnapshotSummaryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 2)
	assert.Equal(t, "high-ns", resp.Data[0].Namespace)
	assert.Equal(t, "low-ns", resp.Data[1].Namespace)
}

func TestGetSnapshotSummary_RBAC_FiltersByCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := testutil.TestOrgID
	clusterAllowed := testutil.TestClusterUUID
	clusterDenied := "dddddddd-dddd-dddd-dddd-dddddddddddd"

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'allowed', 1, NOW()),
		       (1, $2, 'denied', 2, NOW())
		ON CONFLICT DO NOTHING`, clusterAllowed, clusterDenied)
	require.NoError(t, err)

	insertSnapshotRecommendation(t, orgID, clusterAllowed, "apps", "snap-rbac-a", "orphaned", 14)
	insertSnapshotRecommendation(t, orgID, clusterDenied, "apps", "snap-rbac-b", "orphaned", 14)

	app := setupSnapshotSummaryEchoWithRBAC(t, map[string][]string{
		"openshift.cluster": {clusterAllowed},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/snapshots/summary", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.SnapshotSummaryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, clusterAllowed, resp.Data[0].ClusterUUID)
}

func TestGetSnapshotSummary_GroupByNamespaceAlias(t *testing.T) {
	orgID := "org-snap-sum-ns-alias-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "apps", "snap-ns-alias", "orphaned", 10)

	app := setupSnapshotSummaryEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/summary?group_by=namespace",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotSummaryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	assert.Equal(t, "apps", resp.Data[0].Namespace)
	assert.Equal(t, 1, resp.Data[0].SnapshotCount)
}

func TestGetSnapshotSummary_GroupByCluster(t *testing.T) {
	orgID := "org-snap-sum-grp-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "apps", "snap-g1", "orphaned", 10)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "data", "snap-g2", "stale", 20)

	app := setupSnapshotSummaryEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/summary?group_by=cluster",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.SnapshotSummaryListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	assert.Empty(t, resp.Data[0].Namespace)
	assert.Equal(t, 2, resp.Data[0].SnapshotCount)
	assert.Equal(t, 2, resp.Data[0].ActionableSnapshotCount)
}

func TestGetSnapshotSummary_Unauthorized(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupSnapshotSummaryEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/snapshots/summary", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
