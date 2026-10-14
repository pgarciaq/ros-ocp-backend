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
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestSavingsSummary_PluginBreakdownSplitsSign seeds a mix of positive,
// negative, and zero savings rows with distinct values per row (so a
// single-row or same-value fixture cannot green it) and asserts the
// additive by_plugin_breakdown separates savings from additional cost.
// Existing by_plugin signed totals must stay byte-identical.
func TestSavingsSummary_PluginBreakdownSplitsSign(t *testing.T) {
	resetSavingsSummaryCacheForTest(t)
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})

	orgID := "org-breakdown-split"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'split-cluster', 'src-split', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
		VALUES
			($1, $2, 'ns-save', 'w-save', 'Deployment', 'c-save', 'medium', 'cost', false, '{}', 80000, now()),
			($1, $2, 'ns-cost', 'w-cost', 'Deployment', 'c-cost', 'medium', 'cost', false, '{}', -18229, now()),
			($1, $2, 'ns-zero', 'w-zero', 'Deployment', 'c-zero', 'medium', 'cost', false, '{}', 0, now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, notification_codes, estimated_savings_cents, updated_at)
		VALUES
			($1, $2, 'node-save', 'medium', 'cost', '{}', 15000, now()),
			($1, $2, 'node-cost', 'medium', 'cost', '{}', -12345, now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (org_id, cluster_uuid, namespace, persistentvolumeclaim, term, notification_codes, estimated_savings_cents, updated_at)
		VALUES
			($1, $2, 'ns-save', 'vol-save', 'medium', '{}', 8456, now()),
			($1, $2, 'ns-cost', 'vol-cost', 'medium', '{}', -100, now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO vm_recommendations (
			org_id, cluster_uuid, vm_name, namespace, guest_os,
			current_vcpu, current_memory_gib, recommended_vcpu, recommended_memory_gib,
			guest_agent_detected, confidence, term, engine,
			category, estimated_savings_cents, savings_currency, last_recommended_at
		) VALUES
			($1, $2, 'vm-save', 'vm-ns', 'linux', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'idle', 25050, 'USD', now()),
			($1, $2, 'vm-cost', 'vm-ns', 'linux', 2, 8, 4, 16, true, 'high', 'medium_term', 'cost', 'undersized', -5050, 'USD', now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (org_id, cluster_uuid, namespace, snapshot_name, creation_timestamp, estimated_cost_cents, notification_codes, updated_at)
		VALUES ($1, $2, 'ns1', 'snap-stale', now(), 4250, '{}', now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var summary api.FleetSavingsSummaryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))

	// Existing signed totals must not move (additive change only).
	assert.Equal(t, "617.71", summary.ByPlugin.Container.Value)
	assert.Equal(t, "26.55", summary.ByPlugin.Node.Value)
	assert.Equal(t, "83.56", summary.ByPlugin.PVC.Value)
	assert.Equal(t, "200.00", summary.ByPlugin.VM.Value)
	assert.Equal(t, "42.50", summary.ByPlugin.Snapshot.Value)

	split := summary.ByPluginBreakdown
	assert.Equal(t, "800.00", split.Container.Savings.Value)
	assert.Equal(t, "182.29", split.Container.AdditionalCost.Value)
	assert.Equal(t, 1, split.Container.SavingsCount)
	assert.Equal(t, 1, split.Container.AdditionalCostCount)

	assert.Equal(t, "150.00", split.Node.Savings.Value)
	assert.Equal(t, "123.45", split.Node.AdditionalCost.Value)
	assert.Equal(t, 1, split.Node.SavingsCount)
	assert.Equal(t, 1, split.Node.AdditionalCostCount)

	assert.Equal(t, "84.56", split.PVC.Savings.Value)
	assert.Equal(t, "1.00", split.PVC.AdditionalCost.Value)
	assert.Equal(t, 1, split.PVC.SavingsCount)
	assert.Equal(t, 1, split.PVC.AdditionalCostCount)

	assert.Equal(t, "250.50", split.VM.Savings.Value)
	assert.Equal(t, "50.50", split.VM.AdditionalCost.Value)
	assert.Equal(t, 1, split.VM.SavingsCount)
	assert.Equal(t, 1, split.VM.AdditionalCostCount)

	assert.Equal(t, "42.50", split.Snapshot.Savings.Value)
	assert.Equal(t, "0.00", split.Snapshot.AdditionalCost.Value)
	assert.Equal(t, 1, split.Snapshot.SavingsCount)
	assert.Equal(t, 0, split.Snapshot.AdditionalCostCount)

	assert.Equal(t, "USD", split.Container.Savings.Units)
	assert.Equal(t, "USD", split.Container.AdditionalCost.Units)

	// Second identical request must serve the breakdown from cache too.
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req)
	require.Equal(t, http.StatusOK, rec2.Code, "body: %s", rec2.Body.String())

	var cached api.FleetSavingsSummaryResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &cached))
	assert.Equal(t, "800.00", cached.ByPluginBreakdown.Container.Savings.Value)
	assert.Equal(t, "182.29", cached.ByPluginBreakdown.Container.AdditionalCost.Value)
	assert.Equal(t, 1, cached.ByPluginBreakdown.Container.SavingsCount)
	assert.Equal(t, 1, cached.ByPluginBreakdown.Container.AdditionalCostCount)
	assert.Equal(t, "50.50", cached.ByPluginBreakdown.VM.AdditionalCost.Value)
}
