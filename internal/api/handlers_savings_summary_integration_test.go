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

	_, err = pool.Exec(ctx, `
		INSERT INTO vm_recommendations (
			org_id, cluster_uuid, vm_name, namespace, guest_os,
			current_vcpu, current_memory_gib, recommended_vcpu, recommended_memory_gib,
			guest_agent_detected, confidence, term, engine,
			is_idle, is_abandoned, is_oversized, savings_amount, savings_currency, last_recommended_at
		) VALUES (
			$1, $2, 'idle-vm', 'vm-ns', 'linux',
			4, 16, 2, 8,
			true, 'high', 'medium_term', 'cost',
			true, false, false, 250.50, 'USD', now())`,
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
	assert.Equal(t, "1285.060000", summary.EstimatedMonthlySavings.Value)
	assert.Equal(t, "USD", summary.EstimatedMonthlySavings.Units)
	assert.NotEmpty(t, summary.GPUSavingsNote)

	assert.InDelta(t, 800.00, summary.ByPlugin.Container, 0.01)
	assert.Equal(t, 0.00, summary.ByPlugin.GPU)
	assert.InDelta(t, 150.00, summary.ByPlugin.Node, 0.01)
	assert.InDelta(t, 84.56, summary.ByPlugin.PVC, 0.01)
	assert.Equal(t, 0.00, summary.ByPlugin.Snapshot)
	assert.InDelta(t, 250.50, summary.ByPlugin.VM, 0.01)

	require.Len(t, summary.ByCluster, 2)

	byUUID := map[string]api.FleetClusterSavings{}
	for _, row := range summary.ByCluster {
		byUUID[row.ClusterUUID] = row
	}

	prod, ok := byUUID[testutil.TestClusterUUID]
	require.True(t, ok)
	assert.Equal(t, "prod-1", prod.ClusterAlias)
	assert.Equal(t, "1285.060000", prod.EstimatedMonthlySavings.Value)
	assert.True(t, prod.HasCostData)

	dev, ok := byUUID[savingsSummaryCluster2]
	require.True(t, ok)
	assert.Equal(t, "dev-1", dev.ClusterAlias)
	assert.Equal(t, "0.000000", dev.EstimatedMonthlySavings.Value)
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
	assert.Equal(t, "200.000000", summary.EstimatedMonthlySavings.Value)
}

func TestGetFleetSavingsSummary_TermDifferentiation(t *testing.T) {
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
		VALUES (1, $1, 'term-diff-cluster', 'src-td', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES
			($1, $2, 'node-short', 'short', 'cost', '{}', 10000, now()),
			($1, $2, 'node-medium', 'medium', 'cost', '{}', 50000, now()),
			($1, $2, 'node-long', 'long', 'cost', '{}', 90000, now())`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)

	callSummary := func(term string) api.FleetSavingsSummaryResponse {
		url := "/api/cost-management/v1/recommendations/openshift/savings-summary"
		if term != "" {
			url += "?term=" + term
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "term=%q body: %s", term, rec.Body.String())
		var summary api.FleetSavingsSummaryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))
		return summary
	}

	shortSummary := callSummary("short")
	assert.Equal(t, "100.000000", shortSummary.EstimatedMonthlySavings.Value)
	assert.InDelta(t, 100.00, shortSummary.ByPlugin.Node, 0.01)

	mediumSummary := callSummary("medium")
	assert.Equal(t, "500.000000", mediumSummary.EstimatedMonthlySavings.Value)
	assert.InDelta(t, 500.00, mediumSummary.ByPlugin.Node, 0.01)

	defaultSummary := callSummary("")
	assert.Equal(t, mediumSummary.EstimatedMonthlySavings.Value, defaultSummary.EstimatedMonthlySavings.Value)
	assert.InDelta(t, mediumSummary.ByPlugin.Node, defaultSummary.ByPlugin.Node, 0.01)

	longSummary := callSummary("long")
	assert.Equal(t, "900.000000", longSummary.EstimatedMonthlySavings.Value)
	assert.NotEqual(t, shortSummary.EstimatedMonthlySavings.Value, longSummary.EstimatedMonthlySavings.Value)
}

func TestGetFleetSavingsSummary_EngineFilterCostVsPerformance(t *testing.T) {
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
		VALUES (1, $1, 'savings-engine-cluster', 'src-se', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES
			($1, $2, 'node-cost-only', 'medium', 'cost', '{}', 50000, now()),
			($1, $2, 'node-perf-only', 'medium', 'performance', '{}', 90000, now())`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)

	callSummary := func(engine string) api.FleetSavingsSummaryResponse {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/savings-summary?engine="+engine, nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "engine=%s body: %s", engine, rec.Body.String())
		var summary api.FleetSavingsSummaryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))
		return summary
	}

	costSummary := callSummary("cost")
	perfSummary := callSummary("performance")

	assert.Equal(t, "500.000000", costSummary.EstimatedMonthlySavings.Value)
	assert.InDelta(t, 500.00, costSummary.ByPlugin.Node, 0.01)
	assert.Equal(t, "900.000000", perfSummary.EstimatedMonthlySavings.Value)
	assert.InDelta(t, 900.00, perfSummary.ByPlugin.Node, 0.01)
	assert.NotEqual(t, costSummary.EstimatedMonthlySavings.Value, perfSummary.EstimatedMonthlySavings.Value)
}
