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
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func seedManyNodeRecommendations(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string, count int) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1::uuid, 'scale-cluster', 'scale-src', NOW())
		ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	for i := 0; i < count; i++ {
		node := fmt.Sprintf("worker-%03d", i)
		_, err = pool.Exec(ctx, `
			INSERT INTO node_recommendations (
				org_id, cluster_uuid, node, term, engine,
				category, estimated_savings_cents, notification_codes, updated_at
			) VALUES ($1, $2::uuid, $3, 'medium', 'cost', 'underutilized', $4, '{}', NOW())
			ON CONFLICT DO NOTHING`,
			orgID, clusterUUID, node, int64((i+1)*100))
		require.NoError(t, err)
	}
}

func setupNodeUtilEcho(pool *pgxpool.Pool) *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/nodes", api.GetNodeUtilizationRecs)
	return app
}

func TestGetNodeUtilizationRecs_PaginationAtScale(t *testing.T) {
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	database.Pool = pool

	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID
	seedManyNodeRecommendations(t, pool, orgID, clusterUUID, 55)

	app := setupNodeUtilEcho(pool)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/nodes?limit=20&offset=0", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var page1 model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page1))
	assert.Equal(t, 55, page1.Meta.Count)
	assert.Len(t, page1.Data, 20)

	req2 := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/nodes?limit=20&offset=20", nil)
	req2.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var page2 model.NodeUtilizationListResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &page2))
	assert.Equal(t, 55, page2.Meta.Count)
	assert.Len(t, page2.Data, 20)

	nodesPage1 := map[string]bool{}
	for _, r := range page1.Data {
		nodesPage1[r.Node] = true
	}
	for _, r := range page2.Data {
		assert.False(t, nodesPage1[r.Node], "page 2 should not repeat nodes from page 1")
	}
}
