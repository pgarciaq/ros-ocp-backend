package api_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func seedSnapshotRecCluster(t *testing.T, orgID string) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'snap-test', 1, NOW()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID,
	)
	require.NoError(t, err)
}

func insertSnapshotRecommendation(t *testing.T, orgID, clusterUUID, namespace, snapshotName, recType string, ageDays int) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name,
			recommendation_type, age_days, creation_timestamp, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW() - ($6::int * INTERVAL '1 day'), NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, snapshot_name)
		DO UPDATE SET recommendation_type = EXCLUDED.recommendation_type,
			age_days = EXCLUDED.age_days,
			updated_at = NOW()`,
		orgID, clusterUUID, namespace, snapshotName, recType, ageDays,
	)
	require.NoError(t, err)
}

func insertSnapshotRecommendationWithCodes(
	t *testing.T,
	orgID, clusterUUID, namespace, snapshotName, recType string,
	ageDays int,
	codes []int16,
) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name,
			recommendation_type, age_days, notification_codes, creation_timestamp, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW() - ($6::int * INTERVAL '1 day'), NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, snapshot_name)
		DO UPDATE SET recommendation_type = EXCLUDED.recommendation_type,
			age_days = EXCLUDED.age_days,
			notification_codes = EXCLUDED.notification_codes,
			updated_at = NOW()`,
		orgID, clusterUUID, namespace, snapshotName, recType, ageDays, codes,
	)
	require.NoError(t, err)
}

func setupSnapshotRecsEcho(pool *pgxpool.Pool) *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/snapshots", api.GetSnapshotRecommendations)
	return app
}

func setupSnapshotRecsEchoWithRBAC(t *testing.T, perms map[string][]string) *echo.Echo {
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
	v1.GET("/recommendations/openshift/snapshots", api.GetSnapshotRecommendations)
	return app
}

func TestGetSnapshotRecommendations_Empty(t *testing.T) {
	orgID := "org-snap-empty-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/snapshots", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
}

func TestGetSnapshotRecommendations_WithData(t *testing.T) {
	orgID := "org-snap-data-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "apps", "snap-orphan-1", "orphaned", 30)

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/snapshots?limit=20", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "snap-orphan-1", resp.Data[0].SnapshotName)
	assert.Equal(t, "orphaned", resp.Data[0].RecommendationType)
	assert.Equal(t, 30, resp.Data[0].AgeDays)
}

func TestSnapshotRecommendations_FilterByCluster(t *testing.T) {
	orgID := "org-snap-cluster-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	otherCluster := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'snap-cluster-a', 1, NOW()),
		       (1, $2, 'snap-cluster-b', 2, NOW())
		ON CONFLICT DO NOTHING`, testutil.TestClusterUUID, otherCluster)
	require.NoError(t, err)

	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "apps", "snap-cluster-filter-a", "orphaned", 14)
	insertSnapshotRecommendation(t, orgID, otherCluster, "apps", "snap-cluster-filter-b", "orphaned", 14)

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?filter[cluster]="+testutil.TestClusterUUID+"&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, testutil.TestClusterUUID, resp.Data[0].ClusterUUID)
	assert.Equal(t, "snap-cluster-filter-a", resp.Data[0].SnapshotName)
}

func TestSnapshotRecommendations_FilterByProject(t *testing.T) {
	orgID := "org-snap-project-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "target-ns", "snap-project-target", "orphaned", 14)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "other-ns", "snap-project-other", "orphaned", 14)

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?filter[project]=target-ns&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "target-ns", resp.Data[0].Namespace)
	assert.Equal(t, "snap-project-target", resp.Data[0].SnapshotName)
}

func insertSnapshotRecommendationWithSource(
	t *testing.T,
	orgID, clusterUUID, namespace, snapshotName, sourcePVC, recType string,
	ageDays int,
) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name, source_pvc_name,
			recommendation_type, age_days, creation_timestamp, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW() - ($7::int * INTERVAL '1 day'), NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, snapshot_name)
		DO UPDATE SET source_pvc_name = EXCLUDED.source_pvc_name,
			recommendation_type = EXCLUDED.recommendation_type,
			age_days = EXCLUDED.age_days,
			updated_at = NOW()`,
		orgID, clusterUUID, namespace, snapshotName, sourcePVC, recType, ageDays,
	)
	require.NoError(t, err)
}

func TestSnapshotRecommendations_FilterBySourcePVCName(t *testing.T) {
	orgID := "org-snap-pvc-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendationWithSource(
		t, orgID, testutil.TestClusterUUID, "apps", "snap-pvc-target", "data-pvc", "stale", 30,
	)
	insertSnapshotRecommendationWithSource(
		t, orgID, testutil.TestClusterUUID, "apps", "snap-pvc-other", "other-pvc", "stale", 30,
	)

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?filter[pvc_name]=data-pvc&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "snap-pvc-target", resp.Data[0].SnapshotName)
	assert.Equal(t, "data-pvc", resp.Data[0].SourcePVCName)
}

func TestGetSnapshotRecommendations_FilterByRecommendationType(t *testing.T) {
	orgID := "org-snap-type-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "apps", "snap-stale-1", "stale", 120)
	insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "apps", "snap-active-1", "active", 1)

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?filter[recommendation_type]=stale&limit=50",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "stale", resp.Data[0].RecommendationType)
}

func TestGetSnapshotRecommendations_Pagination(t *testing.T) {
	orgID := "org-snap-page-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	for i := 0; i < 3; i++ {
		insertSnapshotRecommendation(t, orgID, testutil.TestClusterUUID, "apps",
			"snap-page-"+uuid.New().String()[:6], "orphaned", 10+i)
	}

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?limit=2&offset=0",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var page1 api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page1))
	assert.Equal(t, 3, page1.Meta.Count)
	assert.Equal(t, 2, page1.Meta.Limit)
	assert.Len(t, page1.Data, 2)

	req2 := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?limit=2&offset=2",
		nil,
	)
	req2.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var page2 api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &page2))
	assert.Equal(t, 2, page2.Meta.Offset)
	assert.Len(t, page2.Data, 1)
}

func TestGetSnapshotRecommendations_RBAC_FiltersByCluster(t *testing.T) {
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

	insertSnapshotRecommendation(t, orgID, clusterAllowed, "apps", "snap-rbac-ok", "orphaned", 14)
	insertSnapshotRecommendation(t, orgID, clusterDenied, "apps", "snap-rbac-deny", "orphaned", 14)

	app := setupSnapshotRecsEchoWithRBAC(t, map[string][]string{
		"openshift.cluster": {clusterAllowed},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/snapshots", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	for _, row := range resp.Data {
		assert.Equal(t, clusterAllowed, row.ClusterUUID)
	}
}

func TestSnapshotRecommendations_NotificationCodes(t *testing.T) {
	orgID := "org-snap-notif-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	cases := []struct {
		snapshotName string
		recType      string
		ageDays      int
		wantCode     int16
	}{
		{"snap-orphan-nc", "orphaned", 30, engine.NotifSnapshotOrphaned},
		{"snap-never-nc", "never_restored", 45, engine.NotifSnapshotNeverUsed},
		{"snap-redundant-nc", "redundant", 100, engine.NotifSnapshotRedundant},
		{"snap-stale-nc", "stale", 120, engine.NotifSnapshotStale},
		{"snap-managed-nc", "managed", 25, engine.NotifSnapshotManaged},
	}
	for _, tc := range cases {
		insertSnapshotRecommendationWithCodes(
			t, orgID, testutil.TestClusterUUID, "apps", tc.snapshotName, tc.recType, tc.ageDays,
			[]int16{tc.wantCode},
		)
	}
	insertSnapshotRecommendationWithCodes(
		t, orgID, testutil.TestClusterUUID, "apps", "snap-active-nc", "active", 2, []int16{},
	)

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?limit=50",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.GreaterOrEqual(t, resp.Meta.Count, len(cases))

	byName := make(map[string]api.SnapshotRecommendationResponse, len(resp.Data))
	for _, row := range resp.Data {
		byName[row.SnapshotName] = row
	}
	for _, tc := range cases {
		row, ok := byName[tc.snapshotName]
		require.True(t, ok, "missing row %s", tc.snapshotName)
		codeKey := strconv.Itoa(int(tc.wantCode))
		entry, ok := row.Notifications[codeKey]
		require.True(t, ok, "%s: notifications missing code %s: %v", tc.snapshotName, codeKey, row.Notifications)
		assert.Equal(t, tc.wantCode, int16(entry.Code))
		assert.NotEmpty(t, entry.Message)
	}
	activeRow := byName["snap-active-nc"]
	assert.Empty(t, activeRow.Notifications)
}

func insertSnapshotRecommendationWithCost(
	t *testing.T,
	orgID, clusterUUID, namespace, snapshotName, recType string,
	ageDays int,
	costCents int64,
) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name,
			recommendation_type, age_days, estimated_cost_cents, creation_timestamp, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW() - ($6::int * INTERVAL '1 day'), NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, snapshot_name)
		DO UPDATE SET recommendation_type = EXCLUDED.recommendation_type,
			age_days = EXCLUDED.age_days,
			estimated_cost_cents = EXCLUDED.estimated_cost_cents,
			updated_at = NOW()`,
		orgID, clusterUUID, namespace, snapshotName, recType, ageDays, costCents,
	)
	require.NoError(t, err)
}

func TestGetSnapshotRecommendations_CursorPagination(t *testing.T) {
	orgID := "org-snap-cursor-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendationWithCost(t, orgID, testutil.TestClusterUUID, "apps", "snap-age-120", "stale", 120, 500)
	insertSnapshotRecommendationWithCost(t, orgID, testutil.TestClusterUUID, "apps", "snap-age-60", "orphaned", 60, 250)
	insertSnapshotRecommendationWithCost(t, orgID, testutil.TestClusterUUID, "apps", "snap-age-30", "active", 30, 100)

	app := setupSnapshotRecsEcho(pool)
	identityHeader := makeIdentityHeader(orgID)

	firstReq := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?limit=1&order_by=age_days&order_how=desc",
		nil,
	)
	firstReq.Header.Set("X-Rh-Identity", identityHeader)
	firstRec := httptest.NewRecorder()
	app.ServeHTTP(firstRec, firstReq)
	require.Equal(t, http.StatusOK, firstRec.Code, firstRec.Body.String())

	var firstPage api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(firstRec.Body.Bytes(), &firstPage))
	require.Len(t, firstPage.Data, 1)
	assert.Equal(t, "snap-age-120", firstPage.Data[0].SnapshotName)
	assert.True(t, firstPage.Meta.HasNext)
	require.NotEmpty(t, firstPage.Meta.NextCursor)

	secondReq := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/cost-management/v1/recommendations/openshift/snapshots?limit=1&after=%s", firstPage.Meta.NextCursor),
		nil)
	secondReq.Header.Set("X-Rh-Identity", identityHeader)
	secondRec := httptest.NewRecorder()
	app.ServeHTTP(secondRec, secondReq)
	require.Equal(t, http.StatusOK, secondRec.Code, secondRec.Body.String())

	var secondPage api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(secondRec.Body.Bytes(), &secondPage))
	require.Len(t, secondPage.Data, 1)
	assert.NotEqual(t, firstPage.Data[0].SnapshotName, secondPage.Data[0].SnapshotName)
}

func TestGetSnapshotRecommendations_MoneyAmountCost(t *testing.T) {
	orgID := "org-snap-cost-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendationWithCost(t, orgID, testutil.TestClusterUUID, "apps", "snap-cost", "stale", 45, 1250)

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?filter[project]=apps",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.NotNil(t, resp.Data[0].EstimatedMonthlyCost)
	assert.Equal(t, "12.50", resp.Data[0].EstimatedMonthlyCost.Value)
	assert.Equal(t, "USD", resp.Data[0].EstimatedMonthlyCost.Units)
}

func TestGetSnapshotRecommendations_CSVExport(t *testing.T) {
	orgID := "org-snap-csv-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendationWithCost(t, orgID, testutil.TestClusterUUID, "apps", "snap-csv", "orphaned", 14, 250)

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?format=csv&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv")

	reader := csv.NewReader(strings.NewReader(rec.Body.String()))
	rows, err := reader.ReadAll()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 2)
	assert.Equal(t, snapshotRecCSVHeaderForTest(), rows[0])

	headerIndex := make(map[string]int, len(rows[0]))
	for i, col := range rows[0] {
		headerIndex[col] = i
	}
	dataRow := rows[1]
	assert.Equal(t, testutil.TestClusterUUID, dataRow[headerIndex["cluster_uuid"]])
	assert.Equal(t, "apps", dataRow[headerIndex["namespace"]])
	assert.Equal(t, "snap-csv", dataRow[headerIndex["snapshot_name"]])
	assert.Equal(t, "orphaned", dataRow[headerIndex["classification"]])
	assert.Equal(t, "2.50", dataRow[headerIndex["estimated_monthly_cost_value"]])
	assert.Equal(t, "USD", dataRow[headerIndex["estimated_monthly_cost_units"]])
}

func snapshotRecCSVHeaderForTest() []string {
	return []string{
		"cluster_uuid", "namespace", "snapshot_name", "source_pvc_name",
		"classification", "age_days", "restore_size_bytes",
		"estimated_monthly_cost_value", "estimated_monthly_cost_units",
		"source_pvc_exists", "last_restored_at",
		"notification_codes", "created_at", "last_reported",
	}
}

func TestGetSnapshotRecommendations_Unauthorized(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/snapshots", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetSnapshotRecommendations_GroupByCluster(t *testing.T) {
	orgID := "org-snap-group-cluster-" + uuid.New().String()[:8]
	clusterA := "550e8400-e29b-41d4-a716-446655440090"
	clusterB := "550e8400-e29b-41d4-a716-446655440091"
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendationWithCost(t, orgID, clusterA, "apps", "snap-a1", "stale", 30, 100)
	insertSnapshotRecommendationWithCost(t, orgID, clusterA, "data", "snap-a2", "orphaned", 60, 200)
	insertSnapshotRecommendationWithCost(t, orgID, clusterB, "apps", "snap-b1", "active", 10, 300)

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?group_by[cluster]=*",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Meta.Count)
	require.Len(t, resp.Data, 2)

	byCluster := map[string]api.SnapshotRecommendationResponse{}
	for _, item := range resp.Data {
		byCluster[item.ClusterUUID] = item
	}
	require.Contains(t, byCluster, clusterA)
	assert.Equal(t, 2, byCluster[clusterA].Count)
	require.Contains(t, byCluster, clusterB)
	assert.Equal(t, 1, byCluster[clusterB].Count)
}

func TestGetSnapshotRecommendations_GroupByProject(t *testing.T) {
	orgID := "org-snap-group-project-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedSnapshotRecCluster(t, orgID)
	insertSnapshotRecommendationWithCost(t, orgID, clusterUUID, "project-a", "snap-a", "stale", 30, 100)
	insertSnapshotRecommendationWithCost(t, orgID, clusterUUID, "project-b", "snap-b", "orphaned", 60, 200)

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?group_by[project]=*",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Meta.Count)
	require.Len(t, resp.Data, 2)

	byNamespace := map[string]api.SnapshotRecommendationResponse{}
	for _, item := range resp.Data {
		byNamespace[item.Namespace] = item
	}
	require.Contains(t, byNamespace, "project-a")
	assert.Equal(t, 1, byNamespace["project-a"].Count)
	require.Contains(t, byNamespace, "project-b")
	assert.Equal(t, 1, byNamespace["project-b"].Count)
}

func TestGetSnapshotRecommendations_GroupByMutuallyExclusive(t *testing.T) {
	orgID := "org-snap-group-bad-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupSnapshotRecsEcho(pool)
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots?group_by[cluster]=*&group_by[project]=*",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
