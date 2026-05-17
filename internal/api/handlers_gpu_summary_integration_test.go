package api_test

import (
	"context"
	"encoding/json"
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
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupGPUSummaryEcho(pool *pgxpool.Pool) *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/gpu", api.GetGPUSummary)
	return app
}

func TestGetGPUSummary_UnauthorizedWithoutIdentity(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	defer func() { database.Pool = nil }()

	app := setupGPUSummaryEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetGPUSummary_OK_JSONStructureAndNonNegativeCounts(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.Pool = pool
	defer func() { database.Pool = nil }()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'gpu-summary-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	seedGPUNodesForTimeslicing(t, pool, start, 7, "gpu-t4-worker-summary")

	app := setupGPUSummaryEcho(pool)
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp model.GPUSummaryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, "/api/cost-management/v1/recommendations/openshift/gpu/mig", resp.MIG.Link)
	assert.Equal(t, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", resp.Timeslicing.Link)

	assert.GreaterOrEqual(t, resp.MIG.Count, 0)
	assert.GreaterOrEqual(t, resp.Timeslicing.Count, 0)
	assert.GreaterOrEqual(t, resp.TotalGPUsAnalyzed, 0)
	assert.GreaterOrEqual(t, resp.ClustersWithGPUData, 0)

	assert.Greater(t, resp.ClustersWithGPUData, 0, "seeded cluster should have GPU rows")
	assert.Greater(t, resp.TotalGPUsAnalyzed, 0, "seeded triples should be counted")
}
