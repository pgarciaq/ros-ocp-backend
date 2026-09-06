package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
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

// Colliding-UUID tests for #552: the same cluster_uuid registered under two
// tenants must resolve each org to its own cluster_alias (and must not fan
// out joined rows). Pre-fix the inner joins fan out deterministically — every
// logical row appears once per matching clusters row — so count assertions
// fail pre-fix regardless of which alias Postgres happens to return first.

// seedCollidingTenant registers clusterUUID under a second tenant. The
// primary tenant rows are seeded by each test's existing setup.
func seedCollidingTenant(t *testing.T, pool *pgxpool.Pool, rhID int, orgID, clusterUUID, alias string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, rhID, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, org_id, last_reported_at)
		VALUES ($1, $2::uuid, $3, $4, $5, now()) ON CONFLICT DO NOTHING`,
		rhID, clusterUUID, alias, fmt.Sprintf("src-collide-%d", rhID), orgID)
	require.NoError(t, err)
}

func assertNoDuplicateContainers(t *testing.T, rows []model.DetailResponse) {
	t.Helper()
	seen := map[string]bool{}
	for _, r := range rows {
		key := r.ClusterUUID + "/" + r.Project + "/" + r.Workload + "/" + r.Container
		assert.False(t, seen[key], "duplicate row from join fan-out: %s (alias %s)", key, r.ClusterAlias)
		seen[key] = true
	}
}

func TestAliasScoping_CollidingClusterUUID_ContainerList(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	const orgB = "org-collide-ctr-b"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, org_id, last_reported_at)
		VALUES (1, $1, 'collide-alias-a', 'src-1', $2, now()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID, testutil.TestOrgID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	seedCollidingTenant(t, pool, 2, orgB, testutil.TestClusterUUID, "collide-alias-b")

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Data []model.DetailResponse `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	assert.Equal(t, len(resp.Data), resp.Meta.Count)
	for _, row := range resp.Data {
		assert.Equal(t, "collide-alias-a", row.ClusterAlias)
	}
	assertNoDuplicateContainers(t, resp.Data)
}

func TestAliasScoping_CollidingClusterUUID_ContainerDetail(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	const orgB = "org-collide-ctrd-b"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, org_id, last_reported_at)
		VALUES (1, $1, 'collide-alias-a', 'src-1', $2, now()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID, testutil.TestOrgID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	seedCollidingTenant(t, pool, 2, orgB, testutil.TestClusterUUID, "collide-alias-b")

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)
	v1.GET("/recommendations/openshift/:recommendation-id", api.GetNativeRecommendationSet)

	listReq := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift", nil)
	listReq.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	listRec := httptest.NewRecorder()
	app.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code)

	var listResp struct {
		Data []model.DetailResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &listResp))
	require.NotEmpty(t, listResp.Data)

	detailReq := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/"+listResp.Data[0].ID, nil)
	detailReq.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	detailRec := httptest.NewRecorder()
	app.ServeHTTP(detailRec, detailReq)
	require.Equal(t, http.StatusOK, detailRec.Code, detailRec.Body.String())

	var detail model.DetailResponse
	require.NoError(t, json.Unmarshal(detailRec.Body.Bytes(), &detail))
	assert.Equal(t, listResp.Data[0].ID, detail.ID)
	assert.Equal(t, "collide-alias-a", detail.ClusterAlias)
}

func TestAliasScoping_CollidingClusterUUID_NamespaceList(t *testing.T) {
	const orgB = "org-collide-ns-b"
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	initNamespaceTestGORM(t, pool)
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(context.Background(), `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, org_id, last_reported_at)
		VALUES (1, $1, 'collide-alias-a', 'src-1', $2, now()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID, testutil.TestOrgID)
	require.NoError(t, err)

	insertNativeNamespaceRec(t, testutil.TestOrgID, "collide-ns", false)
	require.NoError(t, model.RefreshOrgNamespaceKeys(context.Background(), pool, testutil.TestOrgID))

	seedCollidingTenant(t, pool, 2, orgB, testutil.TestClusterUUID, "collide-alias-b")

	app := setupNamespaceListEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/namespaces?limit=50", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Data []model.NativeNamespaceResult `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "collide-ns", resp.Data[0].Project)
	assert.Equal(t, "collide-alias-a", resp.Data[0].ClusterAlias)
}

func TestAliasScoping_CollidingClusterUUID_History(t *testing.T) {
	const orgB = "org-collide-hist-b"
	app, identityHeader := setupHistoryTest(t)
	// setupHistoryTest seeds via the shared pool (registered with the test
	// override); fetch it without re-running SetupTestDB, which truncates.
	pool := database.GetPool()
	require.NotNil(t, pool, "shared test pool must be registered")

	getCount := func() (int, []model.HistoryRow) {
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history", nil)
		req.Header.Set("X-Rh-Identity", identityHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp struct {
			Data []model.HistoryRow `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp.Meta.Count, resp.Data
	}

	beforeCount, _ := getCount()
	require.Greater(t, beforeCount, 0, "baseline history must be non-empty")

	seedCollidingTenant(t, pool, 2, orgB, testutil.TestClusterUUID, "collide-alias-b")

	afterCount, rows := getCount()
	assert.Equal(t, beforeCount, afterCount, "join fan-out must not duplicate history rows")
	for _, row := range rows {
		assert.NotEqual(t, "collide-alias-b", row.ClusterAlias)
	}
}
