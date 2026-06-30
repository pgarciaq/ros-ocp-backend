package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupSnapshotCostByTypeEcho() *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/snapshots/cost-by-type", api.GetSnapshotCostByType)
	return app
}

func seedSnapshotForCostByType(t *testing.T, orgID, clusterUUID, recType string, costCents int64) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	snapName := "snap-cost-" + uuid.New().String()[:8]
	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name,
			recommendation_type, age_days, restore_size_bytes,
			estimated_cost_cents, creation_timestamp, updated_at
		) VALUES ($1, $2, 'cost-test-ns', $3, $4, 10, 1024, $5, NOW(), NOW())`,
		orgID, clusterUUID, snapName, recType, costCents,
	)
	require.NoError(t, err)
}

func TestGetSnapshotCostByType_MultipleTypes(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-cost-type-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'cost-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	seedSnapshotForCostByType(t, orgID, clusterUUID, "orphaned", 500)
	seedSnapshotForCostByType(t, orgID, clusterUUID, "orphaned", 300)
	seedSnapshotForCostByType(t, orgID, clusterUUID, "stale", 200)
	seedSnapshotForCostByType(t, orgID, clusterUUID, "active", 0)

	app := setupSnapshotCostByTypeEcho()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/cost-by-type", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotCostByTypeResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	require.Len(t, resp.Data, 3)

	assert.Equal(t, "orphaned", resp.Data[0].RecommendationType)
	assert.Equal(t, int64(800), resp.Data[0].TotalCostCents)
	assert.Equal(t, 2, resp.Data[0].Count)

	assert.Equal(t, "stale", resp.Data[1].RecommendationType)
	assert.Equal(t, int64(200), resp.Data[1].TotalCostCents)
	assert.Equal(t, 1, resp.Data[1].Count)

	assert.Equal(t, "active", resp.Data[2].RecommendationType)
	assert.Equal(t, int64(0), resp.Data[2].TotalCostCents)
	assert.Equal(t, 1, resp.Data[2].Count)
}

func TestGetSnapshotCostByType_Empty(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-cost-empty-" + uuid.New().String()[:8]
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)

	app := setupSnapshotCostByTypeEcho()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/cost-by-type", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotCostByTypeResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Empty(t, resp.Data)
	assert.NotNil(t, resp.Data)
}

func TestGetSnapshotCostByType_NoIdentity(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupSnapshotCostByTypeEcho()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/cost-by-type", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
