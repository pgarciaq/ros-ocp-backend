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
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestGetFleetSummary_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	// Seed rh_accounts and clusters
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'fleet-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	// Insert recommendation_sets rows:
	// 1 active, 1 idle (notification 5), 1 zombie (idle_state='zombie'), 1 stale
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, idle_state, estimated_savings_cents, updated_at)
		VALUES
			($1, $2, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, '{}', 'active', 1050, now()),
			($1, $2, 'ns1', 'w2', 'Deployment', 'c2', 'medium', 'cost', false, '{5}', 'idle', 2000, now()),
			($1, $2, 'ns2', 'w3', 'Deployment', 'c3', 'medium', 'cost', false, '{5}', 'zombie', 5000, now()),
			($1, $2, 'ns2', 'w4', 'Deployment', 'c4', 'medium', 'cost', true, '{}', 'active', 500, now())
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

	// #874: the card counts the live fleet only — the fourth row is stale, so it
	// leaves total_containers while still being the population behind
	// active_containers, idle_containers and abandoned_containers.
	assert.Equal(t, 3, summary.TotalContainers)
	assert.Equal(t, 1, summary.ActiveContainers)
	assert.Equal(t, 2, summary.IdleContainers)
	assert.Equal(t, 1, summary.AbandonedContainers)
	assert.Equal(t, 1, summary.ClusterCount)
	assert.Equal(t, "80.50", summary.TotalMonthlySavings.Value)
	assert.Equal(t, "USD", summary.TotalMonthlySavings.Units)

	// The invariant the card's percentages depend on: idle everywhere is a
	// subset of the same population as the denominator.
	assert.Equal(t, summary.TotalContainers, summary.ActiveContainers+summary.IdleContainers,
		"active + idle must account for every counted row, or idle% is diluted")
}

// #874: a cluster that stopped reporting must stop being counted. The sweep is
// the only actor that can notice, because every other staleness writer runs
// inside the dead cluster's own processing cycle.
func TestRunRetentionSweep_MarksAndHidesAbandonedCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	const (
		abandonedCluster = "8f0f6f5e-0000-4000-8000-000000008741"
		liveCluster      = "8f0f6f5e-0000-4000-8000-000000008742"
		orgID            = testutil.TestOrgID
	)

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'abandoned-cluster', 'src-874', now() - interval '30 days'),
		       (1, $2, 'live-cluster', 'src-874', now())`,
		abandonedCluster, liveCluster)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets
			(org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine,
			 stale, notification_codes, estimated_savings_cents, updated_at)
		VALUES
			($1, $2, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, '{}', 1050, now() - interval '30 days'),
			($1, $3, 'ns1', 'w2', 'Deployment', 'c2', 'medium', 'cost', false, '{}', 1050, now())`,
		orgID, abandonedCluster, liveCluster)
	require.NoError(t, err)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/fleet-summary", api.GetFleetSummary)

	getSummary := func() api.FleetSummaryResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-summary", nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var summary api.FleetSummaryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &summary))
		return summary
	}

	// The fleet summary is cached per org and the shared test DB reuses TestOrgID,
	// so drop any entry a previous test left before the precondition GET.
	fleetsummary.InvalidateOrg(orgID)

	// The summary is served (and cached) while both clusters still look alive.
	before := getSummary()
	require.Equal(t, 2, before.TotalContainers, "fixture precondition: both clusters counted")
	require.Equal(t, 2, before.ActiveContainers, "fixture precondition: both rows active")

	require.NoError(t, engine.RunRetentionSweep(ctx, pool, 6))

	// #874 decision (2026-11-18, revised): the whole card describes the live
	// fleet. total_containers is the denominator the UI divides idle and
	// abandoned by, so it must exclude stale rows with them; cluster_count
	// follows the same population so the card cannot say "0 containers, 1
	// cluster".
	after := getSummary()
	assert.Equalf(t, 1, after.TotalContainers,
		"cluster %s stopped reporting 30 days ago and must not be counted", abandonedCluster)
	assert.Equalf(t, 1, after.ClusterCount,
		"cluster %s must not contribute to the cluster count", abandonedCluster)
	assert.Equalf(t, 1, after.ActiveContainers,
		"cluster %s stopped reporting 30 days ago and must not be active (stale rows excluded)", abandonedCluster)
	assert.Equalf(t, "10.50", after.TotalMonthlySavings.Value,
		"savings from cluster %s must not be counted", abandonedCluster)
	assert.Equal(t, after.TotalContainers, after.ActiveContainers+after.IdleContainers,
		"the card's percentages divide by total_containers; the parts must sum to it")

	// Whatever the retention path does with the row afterwards (Task 5 pins the
	// visible stale window), no LIVE row may remain for the abandoned cluster.
	var liveRows int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND stale = false`,
		orgID, abandonedCluster).Scan(&liveRows))
	assert.Zerof(t, liveRows,
		"cluster %s stopped reporting 30 days ago; no live row may remain", abandonedCluster)
}
