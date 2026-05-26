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
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const savingsSummaryCluster2 = "22222222-2222-2222-2222-222222222222"

func TestGetFleetSavingsSummary_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES
			(1, $1, 'prod-1', 'src-1', now()),
			(1, $2, 'dev-1', 'src-2', now())
		ON CONFLICT DO NOTHING`, testutil.TestClusterUUID, savingsSummaryCluster2)
	require.NoError(t, err)

	// prod-1: container + node + pvc + snapshot savings (stored as integer cents)
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES ($1, $2, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, '{}', 80000, now())`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES ($1, $2, 'node-a', 'medium', 'cost', '{}', 15000, now())`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (org_id, cluster_uuid, namespace, persistentvolumeclaim, term, notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES ($1, $2, 'ns1', 'data-vol', 'medium', '{}', 8456, now())`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (org_id, cluster_uuid, namespace, snapshot_name, creation_timestamp, estimated_monthly_cost_usd, notification_codes, updated_at)
		VALUES ($1, $2, 'ns1', 'snap-old', now(), 0.00, '{}', now())`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	// dev-1: recommendations exist but all lack cost data (NotifNoCostData)
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES ($1, $2, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, $3, 0.00, now())`,
		testutil.TestOrgID, savingsSummaryCluster2, []int16{engine.NotifNoCostData})
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var summary api.FleetSavingsSummaryResponse
	err = json.Unmarshal(rec.Body.Bytes(), &summary)
	require.NoError(t, err)

	assert.Equal(t, "USD", summary.Currency)
	assert.InDelta(t, 1034.56, summary.TotalEstimatedMonthlySavings, 0.01)
	assert.NotEmpty(t, summary.GPUSavingsNote)

	assert.InDelta(t, 800.00, summary.ByPlugin.Container, 0.01)
	assert.Equal(t, 0.00, summary.ByPlugin.GPU)
	assert.InDelta(t, 150.00, summary.ByPlugin.Node, 0.01)
	assert.InDelta(t, 84.56, summary.ByPlugin.PVC, 0.01)
	assert.Equal(t, 0.00, summary.ByPlugin.Snapshot)

	require.Len(t, summary.ByCluster, 2)

	byUUID := map[string]api.FleetClusterSavings{}
	for _, row := range summary.ByCluster {
		byUUID[row.ClusterUUID] = row
	}

	prod, ok := byUUID[testutil.TestClusterUUID]
	require.True(t, ok)
	assert.Equal(t, "prod-1", prod.ClusterAlias)
	assert.InDelta(t, 1034.56, prod.Savings, 0.01)
	assert.True(t, prod.HasCostData)

	dev, ok := byUUID[savingsSummaryCluster2]
	require.True(t, ok)
	assert.Equal(t, "dev-1", dev.ClusterAlias)
	assert.Equal(t, 0.00, dev.Savings)
	assert.False(t, dev.HasCostData)
}

func TestGetFleetSavingsSummary_PerformanceEngine(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'perf-cluster', 'src-perf', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES ($1, $2, 'node-perf', 'medium', 'performance', '{}', 20000, now())`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary?engine=performance", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var summary api.FleetSavingsSummaryResponse
	err = json.Unmarshal(rec.Body.Bytes(), &summary)
	require.NoError(t, err)
	assert.InDelta(t, 200.00, summary.ByPlugin.Node, 0.01)
	assert.InDelta(t, 200.00, summary.TotalEstimatedMonthlySavings, 0.01)
}
