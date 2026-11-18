package api_test

import (
	"context"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #874 follow-up: the native lists apply filter[stale], but the legacy query
// layer behind them builds its WHERE map in utils.go without a stale
// predicate. Four list surfaces reach that layer:
//
//   - the namespace list's Kruize fallback (GetNamespaceRecommendationSetList,
//     taken when the native leg returns nothing),
//   - the Kruize-mode container list (GetRecommendationSetList),
//   - the deprecated /container compatibility list, and
//   - the HCP list (flat and grouped, both through MapHCPQueryParameters).
//
// Their shared mappers are MapNamespaceQueryParameters and
// mapContainerQueryParameters. Without the predicate an org whose rows are all
// stale — the state right after the sweep marks an abandoned cluster and
// prunes its catalog entries — gets them back from the fallback: the default
// list served a dead cluster while filter[stale]=only returned the same rows,
// so the two views were indistinguishable and the escape hatch proved nothing.
//
// The fixture is a stale row on a cluster that stopped reporting 40 days ago,
// with no catalog rows built (the sweep pruned them). The row is stale and the
// default must not see it; filter[stale]=only and filter[stale]=true must.
const (
	legacyStaleCluster = "8f0f6f5e-0000-4000-8000-000000008721"
	legacyStaleNS      = "ns-874-legacy-stale"
	legacyStaleCtr     = "8f0f6f5e-0000-4000-8000-00000000872c"
)

func setupLegacyStaleFilterApp(t *testing.T) *echo.Echo {
	t.Helper()
	t.Setenv("RBAC_ENABLE", "false")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)

	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	// Production native container list: the list leg never falls back, the
	// detail leg does.
	v1.GET("/recommendations/openshift", api.GetRecommendationSetListWithFallback)
	// The Kruize-mode registration of that same path (server.go:240). Mounted
	// under its own prefix because registration is engine-gated at boot; the
	// handler never reads the request path.
	v1.GET("/kruize/recommendations/openshift", api.GetRecommendationSetList)
	v1.GET("/recommendations/openshift/container", api.GetNativeCompatRecommendationSetList)
	v1.GET("/recommendations/openshift/namespace", api.GetNamespaceRecommendationSetListWithFallback)
	v1.GET("/recommendations/openshift/hcp", api.GetHCPRecommendationSetList)
	return app
}

func seedLegacyStaleFilterFixture(t *testing.T, ctx context.Context) {
	t.Helper()
	pool := database.GetPool()
	org := testutil.TestOrgID

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, org)
	require.NoError(t, err)
	// Abandoned cluster, still carrying the HCP namespace so the HCP surface's
	// scope predicate admits its row (that surface is namespace-membership
	// based, not freshness based).
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at, hcp_namespaces)
		VALUES (1, $1, $2, 'legacy-stale', 'src-874-legacy', now() - interval '40 days', ARRAY[$3]::text[])`,
		org, legacyStaleCluster, legacyStaleNS)
	require.NoError(t, err)

	// Fixture invariants the legacy container queries depend on (see
	// seedRetentionLivenessLists): term='short' + engine='cost', a non-NULL
	// container_id, and monitoring_end_time/updated_at inside the default
	// request window.
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets
			(org_id, cluster_uuid, container_id, namespace, workload, workload_type,
			 container_name, term, engine, stale, notification_codes,
			 estimated_savings_cents, monitoring_end_time, updated_at)
		VALUES ($1, $2, $3, $4, 'legacy-stale-workload', 'Deployment', 'legacy-stale-ctr',
		 'short', 'cost', true, '{}', 100, now() - interval '1 minute', now())`,
		org, legacyStaleCluster, legacyStaleCtr, legacyStaleNS)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO namespace_recommendation_sets
			(org_id, cluster_uuid, namespace_name, term, engine, stale,
			 rec_cpu_request_millicores, rec_cpu_limit_millicores, rec_memory_request_kib, rec_memory_limit_kib,
			 current_cpu_request_millicores, current_cpu_limit_millicores, current_memory_request_kib, current_memory_limit_kib,
			 confidence_level, notification_codes, monitoring_start_time, monitoring_end_time, updated_at)
		VALUES ($1, $2, $3, 'short', 'cost', true,
		 4000, 8000, 8388608, 16777216, 5000, 10000, 10485760, 20971520,
		 0.9, '{}', now() - 7 * interval '1 day', now() - interval '1 minute', now())`,
		org, legacyStaleCluster, legacyStaleNS)
	require.NoError(t, err)

	// Deliberately no RefreshOrgMetadata / RefreshOrgNamespaceKeys: the sweep
	// pruned the catalogs after marking, which is what sends the default
	// namespace request into the fallback.
}

func TestStaleFilter_HidesRowsThroughEveryLegacyList(t *testing.T) {
	ctx := context.Background()
	app := setupLegacyStaleFilterApp(t)
	seedLegacyStaleFilterFixture(t, ctx)

	surfaces := []struct{ name, path string }{
		{"namespace fallback", "/api/cost-management/v1/recommendations/openshift/namespace"},
		{"kruize container list", "/api/cost-management/v1/kruize/recommendations/openshift"},
		{"compat container list", "/api/cost-management/v1/recommendations/openshift/container"},
		{"hcp list", "/api/cost-management/v1/recommendations/openshift/hcp"},
	}

	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			page, body := getLivenessList(t, app, s.path)
			assert.Zerof(t, page.Meta.Count,
				"%s: the default list must not count a stale row (body: %s)", s.name, body)
			assert.Emptyf(t, page.Data,
				"%s: the default list must not return a stale row", s.name)

			// The escape hatch has to keep working, and it can only be proven
			// against the same row the default view just hid — a blanket
			// exclusion in the legacy layer would pass the assertion above and
			// fail here.
			only, onlyBody := getLivenessList(t, app, s.path+"?filter%5Bstale%5D=only")
			assert.Equalf(t, 1, only.Meta.Count,
				"%s: filter[stale]=only must still count the hidden row (body: %s)", s.name, onlyBody)
			assert.Lenf(t, only.Data, 1,
				"%s: filter[stale]=only must still return the hidden row", s.name)

			// filter[stale]=true keeps its documented meaning: no filter.
			both, bothBody := getLivenessList(t, app, s.path+"?filter%5Bstale%5D=true")
			assert.Equalf(t, 1, both.Meta.Count,
				"%s: filter[stale]=true must keep meaning no filter (body: %s)", s.name, bothBody)
		})
	}
}
