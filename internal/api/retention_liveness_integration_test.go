package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #874 blocker: the sweep marks recommendation rows stale, but the list
// endpoints page and count through the org key catalogs (org_container_keys,
// org_namespace_keys), whose is_stale is copied only by RefreshOrgMetadata /
// RefreshOrgNamespaceKeys. The in-cycle writer always follows
// MarkUnreportedContainersStale with both refreshes; the sweep originally did
// not, so after it hid a dead cluster:
//
//   - meta.count counted the dead cluster while the row leg excluded it
//     (count 2, rows 1, then an empty page past the end), and
//   - filter[stale]=only, the view that is supposed to expose the hidden rows,
//     returned nothing for them.
//
// Both are asserted here on the container and namespace lists.
const (
	livenessDarkCluster = "8f0f6f5e-0000-4000-8000-000000008711"
	livenessLiveCluster = "8f0f6f5e-0000-4000-8000-000000008712"
)

func setupRetentionLivenessListTest(t *testing.T) *echo.Echo {
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
	// Production wiring (server.go, plugins/namespace/plugin.go): native engine
	// with the legacy Kruize fallback when the native list returns nothing.
	v1.GET("/recommendations/openshift", api.GetRecommendationSetListWithFallback)
	v1.GET("/recommendations/openshift/namespace", api.GetNamespaceRecommendationSetListWithFallback)
	return app
}

func seedRetentionLivenessLists(t *testing.T, ctx context.Context) {
	t.Helper()
	pool := database.GetPool()
	org := testutil.TestOrgID

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, org)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, $2, 'dark-874', 'src-874', now() - interval '30 days'),
		       (1, $1, $3, 'live-874', 'src-874', now())`,
		org, livenessDarkCluster, livenessLiveCluster)
	require.NoError(t, err)

	// Fixture invariants the compat lists depend on (see
	// seedAliasFilterCompatRows): term='short' + engine='cost', a non-NULL
	// container_id, and monitoring_end_time strictly before the request's
	// default end bound.
	for i, row := range []struct {
		cluster string
		name    string
	}{
		{livenessDarkCluster, "dark-a"},
		{livenessDarkCluster, "dark-b"},
		{livenessLiveCluster, "live-a"},
	} {
		_, err = pool.Exec(ctx, `
			INSERT INTO recommendation_sets
				(org_id, cluster_uuid, container_id, namespace, workload, workload_type,
				 container_name, term, engine, stale, notification_codes,
				 estimated_savings_cents, monitoring_end_time, updated_at)
			VALUES ($1, $2, $3, 'ns-874', $4, 'Deployment', $4, 'short', 'cost', false, '{}',
			 100, now() - interval '1 minute', now())`,
			org, row.cluster, row.cluster+"-ctr-"+string(rune('a'+i)), row.name)
		require.NoError(t, err)
	}

	for _, row := range []struct{ cluster, namespace string }{
		{livenessDarkCluster, "ns-874-dark-a"},
		{livenessDarkCluster, "ns-874-dark-b"},
		{livenessLiveCluster, "ns-874-live-a"},
	} {
		_, err = pool.Exec(ctx, `
			INSERT INTO namespace_recommendation_sets
				(org_id, cluster_uuid, namespace_name, term, engine, stale,
				 rec_cpu_request_millicores, rec_cpu_limit_millicores, rec_memory_request_kib, rec_memory_limit_kib,
				 current_cpu_request_millicores, current_cpu_limit_millicores, current_memory_request_kib, current_memory_limit_kib,
				 confidence_level, notification_codes, monitoring_start_time, monitoring_end_time, updated_at)
			VALUES ($1, $2, $3, 'short', 'cost', false,
			 4000, 8000, 8388608, 16777216, 5000, 10000, 10485760, 20971520,
			 0.9, '{}', now() - 7 * interval '1 day', now() - interval '1 minute', now())`,
			org, row.cluster, row.namespace)
		require.NoError(t, err)
	}

	// The native lists page and count through the org key catalogs, which
	// ingestion builds after writing recommendations. Build them here so the
	// precondition numbers describe a real ingest, not a half-populated org.
	require.NoError(t, engine.RefreshOrgMetadata(ctx, pool, org))
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, org))
}

// livenessListPage is the response shape both lists return: rows plus the
// catalog-derived count they are paged against.
type livenessListPage struct {
	Data []map[string]interface{} `json:"data"`
	Meta struct {
		Count   int  `json:"count"`
		HasNext bool `json:"has_next"`
	} `json:"meta"`
}

func getLivenessList(t *testing.T, app *echo.Echo, path string) (livenessListPage, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equalf(t, http.StatusOK, rec.Code, "%s body: %s", path, rec.Body.String())

	var page livenessListPage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	return page, rec.Body.String()
}

func TestRunRetentionSweep_KeepsListCountAndRowsAgreeing(t *testing.T) {
	ctx := context.Background()
	app := setupRetentionLivenessListTest(t)
	seedRetentionLivenessLists(t, ctx)

	paths := map[string]string{
		"containers": "/api/cost-management/v1/recommendations/openshift",
		"namespaces": "/api/cost-management/v1/recommendations/openshift/namespace",
	}

	for name, path := range paths {
		page, _ := getLivenessList(t, app, path)
		require.Equalf(t, 3, page.Meta.Count, "%s: fixture precondition", name)
		require.Equalf(t, 3, len(page.Data), "%s: fixture precondition", name)
	}

	require.NoError(t, engine.RunRetentionSweep(ctx, database.GetPool(), 6))

	for name, path := range paths {
		page, _ := getLivenessList(t, app, path)
		assert.Equalf(t, 1, page.Meta.Count,
			"%s: the dead cluster must leave the count, not just the rows", name)
		assert.Equalf(t, len(page.Data), page.Meta.Count,
			"%s: a count larger than the returned rows sends the UI paging past an invisible tail", name)

		// The promised escape hatch: the hidden rows stay listable.
		only, _ := getLivenessList(t, app, path+"?filter%5Bstale%5D=only")
		assert.Equalf(t, 2, only.Meta.Count,
			"%s: filter[stale]=only must see the marked cluster", name)
		assert.Equalf(t, 2, len(only.Data),
			"%s: filter[stale]=only must return the marked rows", name)
	}

	// Second half of the same invariant: the purge deletes rows, so it must also
	// prune the catalog entries that point at them. Otherwise filter[stale]=only
	// keeps counting a cluster whose rows are gone (count 2, rows 0) forever —
	// an org whose only cluster died never runs another ingest to repair it.
	_, err := database.GetPool().Exec(ctx,
		`UPDATE recommendation_sets SET staled_at = now() - interval '31 days' WHERE cluster_uuid = $1`,
		livenessDarkCluster)
	require.NoError(t, err)
	_, err = database.GetPool().Exec(ctx,
		`UPDATE namespace_recommendation_sets SET staled_at = now() - interval '31 days' WHERE cluster_uuid = $1`,
		livenessDarkCluster)
	require.NoError(t, err)
	require.NoError(t, engine.RunRetentionSweep(ctx, database.GetPool(), 6))

	for name, path := range paths {
		page, _ := getLivenessList(t, app, path)
		assert.Equalf(t, 1, page.Meta.Count, "%s: purge must not disturb the live cluster", name)
		assert.Equalf(t, len(page.Data), page.Meta.Count, "%s: count and rows must agree after the purge", name)

		only, onlyBody := getLivenessList(t, app, path+"?filter%5Bstale%5D=only")
		assert.Equalf(t, len(only.Data), only.Meta.Count,
			"%s: the stale view must not report a count its rows contradict", name)
		assert.NotContainsf(t, onlyBody, livenessDarkCluster,
			"%s: a purged cluster's rows must not be listable", name)

		if name == "containers" {
			// Nothing to fall back to here, so the stale view must be empty. The
			// catalog-prune mechanism itself is pinned by
			// TestPurge_PrunesOrgListCatalogs in internal/engine: the keys leg
			// never counts an orphan key, which is why this assertion alone could
			// not catch a missing refresh.
			assert.Zero(t, only.Meta.Count,
				"%s: the purged cluster must leave the stale view too", name)
		}
	}
}

// TestRunRetentionSweep_PagedListsMatchCount walks every page a UI could ask
// for with limit=1, which no earlier test does: the count is catalog-derived
// while rows come from the detail leg, so a sweep that moves one and not the
// other is invisible to a single limited request and only shows up as a page
// the reader reaches and cannot fill — or as a row served twice.
//
// It asserts per surface: meta.count is constant on every page, walking pages
// until has_next is false yields exactly meta.count distinct rows, and the page
// one past the end is empty rather than a duplicate or a phantom.
func TestRunRetentionSweep_PagedListsMatchCount(t *testing.T) {
	ctx := context.Background()
	app := setupRetentionLivenessListTest(t)
	seedRetentionLivenessLists(t, ctx)
	require.NoError(t, engine.RunRetentionSweep(ctx, database.GetPool(), 6))

	cases := []struct {
		name  string
		path  string
		query string
		want  int
	}{
		{"containers default", "/api/cost-management/v1/recommendations/openshift", "", 1},
		{"containers only", "/api/cost-management/v1/recommendations/openshift", "filter%5Bstale%5D=only", 2},
		{"namespaces default", "/api/cost-management/v1/recommendations/openshift/namespace", "", 1},
		{"namespaces only", "/api/cost-management/v1/recommendations/openshift/namespace", "filter%5Bstale%5D=only", 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			urlAt := func(offset int) string {
				u := fmt.Sprintf("%s?limit=1&offset=%d", tc.path, offset)
				if tc.query != "" {
					u += "&" + tc.query
				}
				return u
			}

			seen := make(map[string]int, tc.want)
			for offset := 0; offset < tc.want; offset++ {
				page, body := getLivenessList(t, app, urlAt(offset))
				assert.Equalf(t, tc.want, page.Meta.Count,
					"offset %d: meta.count must not move between pages; body %s", offset, body)
				require.Lenf(t, page.Data, 1, "offset %d: a page before the end must be filled; body %s", offset, body)

				key := rowIdentity(t, page.Data[0])
				prior, dup := seen[key]
				require.Falsef(t, dup, "offset %d: row already served by offset %d; body %s", offset, prior, body)
				seen[key] = offset

				assert.Equalf(t, offset < tc.want-1, page.Meta.HasNext,
					"offset %d: has_next must describe whether a further page exists", offset)
			}

			// The page past the end: reachable by a UI that trusts a stale count.
			over, overBody := getLivenessList(t, app, urlAt(tc.want))
			assert.Emptyf(t, over.Data, "page past the end must be empty; body %s", overBody)
			assert.Falsef(t, over.Meta.HasNext, "the last page must not advertise a next page")
			assert.Equalf(t, tc.want, over.Meta.Count, "the count must survive paging past the end; body %s", overBody)
		})
	}
}

// rowIdentity keys a list row by its full content. Two pages serving the same
// row produce equal identities; distinct rows cannot collide the way a
// hand-picked subset of columns could.
func rowIdentity(t *testing.T, row map[string]interface{}) string {
	t.Helper()
	blob, err := json.Marshal(row)
	require.NoError(t, err, "marshal row for identity")
	return string(blob)
}
