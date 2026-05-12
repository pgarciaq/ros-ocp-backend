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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestGetFleetSummary_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	database.Pool = pool

	// Seed rh_accounts and clusters
	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'fleet-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	// Insert recommendation_sets rows:
	// 1 active, 1 idle (notification 5), 1 abandoned (notification 8), 1 stale
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES
			($1, $2, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, '{}', 10.5, now()),
			($1, $2, 'ns1', 'w2', 'Deployment', 'c2', 'medium', 'cost', false, '{5}', 20.0, now()),
			($1, $2, 'ns2', 'w3', 'Deployment', 'c3', 'medium', 'cost', false, '{8}', 50.0, now()),
			($1, $2, 'ns2', 'w4', 'Deployment', 'c4', 'medium', 'cost', true, '{}', 5.0, now())
	`, testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	// Set up Echo with identity middleware + fleet handler
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/fleet-summary", api.GetFleetSummary)

	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-summary", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var summary api.FleetSummaryResponse
	err = json.Unmarshal(rec.Body.Bytes(), &summary)
	require.NoError(t, err)

	assert.Equal(t, 4, summary.TotalContainers)
	assert.Equal(t, 3, summary.ActiveContainers)
	assert.Equal(t, 1, summary.IdleContainers)
	assert.Equal(t, 1, summary.AbandonedContainers)
	assert.Equal(t, 1, summary.ClusterCount)
	assert.InDelta(t, 80.5, summary.TotalMonthlySavingsUSD, 0.01)
}
