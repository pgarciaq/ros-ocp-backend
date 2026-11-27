package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// Follow-up matrix (#963, #964, #967, #968): the surfaces the #950 inventory
// found still on the OLD cluster-filter rules, now on the same contract as
// everything else — a UUID binds exactly, any other value is an org-scoped
// case-insensitive alias substring, and the result is the union of matches
// intersected with the RBAC grant (docs-site/plugin-reference/query-parameters.md#cluster-filter).
//
// Where the response cannot carry the cluster (a single object, or a per-node
// series whose rows have no cluster column), an alias matching several readable
// clusters answers 400 and names the way out instead of picking one.
//
// Fixture: three clusters, two of whose aliases share the substring "prod".
// Every surface gets one row per cluster, so "prod" is observably a union of
// two and "staging" resolves to exactly one.

const clusterAliasBatch2Base = "/api/cost-management/v1/recommendations/openshift"

type clusterAliasBatch2Fixture struct {
	orgID string
	a     string
	b     string
	c     string
}

func seedClusterAliasBatch2Fixture(t *testing.T) clusterAliasBatch2Fixture {
	t.Helper()

	orgID := "org-cluster-alias2-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() {
		database.Pool = nil
		database.DB = nil
	})

	t.Setenv("RBAC_ENABLE", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	require.True(t, config.GetConfig().RBACEnabled, "this matrix needs RBAC_ENABLE=true for the grant leg")

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)

	fx := clusterAliasBatch2Fixture{
		orgID: orgID,
		a:     uuid.New().String(),
		b:     uuid.New().String(),
		c:     uuid.New().String(),
	}
	for _, row := range []struct{ alias, id string }{
		{"alias-prod-east", fx.a},
		{"alias-prod-west", fx.b},
		{"alias-staging", fx.c},
	} {
		_, err = pool.Exec(ctx, `
			INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
			VALUES (1, $1::uuid, $2, 'src-'||$2, now())`, row.id, row.alias)
		require.NoError(t, err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	// #963: one snapshot set per cluster.
	_, err = pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name, creation_timestamp,
			restore_size_bytes, age_days, recommendation_type, estimated_cost_cents
		) VALUES
			($1, $2::uuid, 'alias-ns', 'snap-a', $5, 1000, 10, 'orphaned', 100),
			($1, $3::uuid, 'alias-ns', 'snap-b', $5, 2000, 20, 'orphaned', 200),
			($1, $4::uuid, 'alias-ns', 'snap-c', $5, 3000, 30, 'orphaned', 300)`,
		fx.orgID, fx.a, fx.b, fx.c, now)
	require.NoError(t, err)

	// #964a: hourly digests share the node name across clusters — the reason
	// this route cannot take a union.
	reportDate := now.AddDate(0, 0, -1).Truncate(24 * time.Hour)
	testutil.EnsureMonthlyPartition(t, pool, "hourly_node_digests", reportDate)
	_, err = pool.Exec(ctx, `
		INSERT INTO hourly_node_digests (
			org_id, cluster_uuid, node_name, report_date, hour,
			cpu_usage_p95_mc, mem_usage_p95_kib, sample_count, max_pod_count
		) VALUES
			($1, $2::uuid, 'alias-hourly-node', $5, 0, 100, 2048, 4, 10),
			($1, $3::uuid, 'alias-hourly-node', $5, 0, 200, 4096, 4, 11),
			($1, $4::uuid, 'alias-hourly-node', $5, 0, 300, 8192, 4, 12)`,
		fx.orgID, fx.a, fx.b, fx.c, reportDate)
	require.NoError(t, err)

	// #964b: GPU time-slicing history rows carry cluster_uuid, so the route
	// returns the union.
	_, err = pool.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendation_history (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, candidate_count, impacted_count,
			estimated_savings_cents
		) VALUES
			($1, $2::uuid, 'alias-gpu-node', 'Tesla T4', 'short', 4, 0.75, 2, 1, 40000),
			($1, $3::uuid, 'alias-gpu-node', 'Tesla T4', 'short', 4, 0.75, 2, 1, 40000),
			($1, $4::uuid, 'alias-gpu-node', 'Tesla T4', 'short', 4, 0.75, 2, 1, 40000)`,
		fx.orgID, fx.a, fx.b, fx.c)
	require.NoError(t, err)

	// #967: the single-object detail routes. One row per cluster under the same
	// name, which is exactly the shape a union would silently merge.
	_, err = pool.Exec(ctx, `
		INSERT INTO vm_recommendations (
			org_id, cluster_uuid, vm_name, namespace, guest_os,
			current_vcpu, current_memory_gib, recommended_vcpu, recommended_memory_gib,
			guest_agent_detected, confidence, term, engine,
			category, estimated_savings_cents, savings_currency, last_recommended_at
		) VALUES
			($1, $2::uuid, 'alias-vm', 'alias-ns', 'RHEL 9.2', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'optimized', 1000, 'USD', now()),
			($1, $3::uuid, 'alias-vm', 'alias-ns', 'RHEL 9.2', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'optimized', 1000, 'USD', now()),
			($1, $4::uuid, 'alias-vm', 'alias-ns', 'RHEL 9.2', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'optimized', 1000, 'USD', now())`,
		fx.orgID, fx.a, fx.b, fx.c)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, recommendation_type
		) VALUES
			($1, $2::uuid, 'alias-ns', 'alias-pvc', 'resize'),
			($1, $3::uuid, 'alias-ns', 'alias-pvc', 'resize'),
			($1, $4::uuid, 'alias-ns', 'alias-pvc', 'resize')`,
		fx.orgID, fx.a, fx.b, fx.c)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace, quota_name, recommendation_type
		) VALUES
			($1, $2::uuid, 'alias-ns', 'alias-quota', 'resize'),
			($1, $3::uuid, 'alias-ns', 'alias-quota', 'resize'),
			($1, $4::uuid, 'alias-ns', 'alias-quota', 'resize')`,
		fx.orgID, fx.a, fx.b, fx.c)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO cluster_quota_recommendation_sets (
			org_id, cluster_uuid, cluster_quota_name, recommendation_type
		) VALUES
			($1, $2::uuid, 'alias-crq', 'resize'),
			($1, $3::uuid, 'alias-crq', 'resize'),
			($1, $4::uuid, 'alias-crq', 'resize')`,
		fx.orgID, fx.a, fx.b, fx.c)
	require.NoError(t, err)

	// The VM history route reads cluster_id (a TEXT uuid) — its rows carry it,
	// so it is a union surface.
	_, err = pool.Exec(ctx, `
		INSERT INTO vm_recommendation_history (
			org_id, cluster_id, vm_name, namespace, term, engine,
			recommended_vcpu, recommended_memory_gib, created_at
		) VALUES
			($1, $2, 'alias-vm', 'alias-ns', 'short_term', 'cost', 2, 8, $5),
			($1, $3, 'alias-vm', 'alias-ns', 'short_term', 'cost', 2, 8, $5),
			($1, $4, 'alias-vm', 'alias-ns', 'short_term', 'cost', 2, 8, $5)`,
		fx.orgID, fx.a, fx.b, fx.c, now)
	require.NoError(t, err)

	// #968: history and the quality families compared the client value to the
	// alias EXACTLY — a UUID matched nothing.
	testutil.EnsureMonthlyPartition(t, pool, "recommendation_history", now)
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_history (
			recorded_at, org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine
		) VALUES
			($5, $1, $2::uuid, 'alias-ns', 'alias-wl', 'Deployment', 'alias-ctr', 'medium', 'cost'),
			($5, $1, $3::uuid, 'alias-ns', 'alias-wl', 'Deployment', 'alias-ctr', 'medium', 'cost'),
			($5, $1, $4::uuid, 'alias-ns', 'alias-wl', 'Deployment', 'alias-ctr', 'medium', 'cost')`,
		fx.orgID, fx.a, fx.b, fx.c, now)
	require.NoError(t, err)

	testutil.EnsureMonthlyPartition(t, pool, "recommendation_quality", now)
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_quality (
			measured_at, org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, engine, stability_pct, adoption_detected, recommendation_age_hours
		) VALUES
			($5, $1, $2::uuid, 'alias-ns', 'alias-wl', 'Deployment', 'alias-ctr', 'cost', 0.9, true, 24),
			($5, $1, $3::uuid, 'alias-ns', 'alias-wl', 'Deployment', 'alias-ctr', 'cost', 0.9, true, 24),
			($5, $1, $4::uuid, 'alias-ns', 'alias-wl', 'Deployment', 'alias-ctr', 'cost', 0.9, true, 24)`,
		fx.orgID, fx.a, fx.b, fx.c, now)
	require.NoError(t, err)

	return fx
}

// clusterAliasBatch2App mounts every surface this batch touches, carrying fixed
// permissions so one fixture serves the unrestricted and the partial-grant leg.
func clusterAliasBatch2App(perms map[string][]string) *echo.Echo {
	return permsApp(perms, func(v1 *echo.Group) {
		v1.GET("/recommendations/openshift/node/:id/hourly-utilization", api.GetNodeHourlyUtilization)
		v1.GET("/recommendations/openshift/gpu/timeslicing/history", api.GetNodeGPUTimeslicingRecommendationHistory)
		v1.GET("/recommendations/openshift/snapshots/summary", api.GetSnapshotSummary)
		v1.GET("/recommendations/openshift/vm/detail", api.GetVMRecommendationDetail)
		v1.GET("/recommendations/openshift/vms/:vm_name/history", api.GetVMRecommendationHistory)
		v1.GET("/recommendations/openshift/pvcs/detail", api.GetPVCRecommendationDetail)
		v1.GET("/recommendations/openshift/quota/detail", api.GetQuotaRecommendationDetail)
		v1.GET("/recommendations/openshift/cluster-quota/detail", api.GetClusterQuotaRecommendationDetail)
		v1.GET("/recommendations/openshift/history", api.GetRecommendationHistory)
		v1.GET("/recommendations/openshift/quality", api.GetRecommendationQuality)
		v1.GET("/recommendations/openshift/quality/pvcs", api.GetPVCRecommendationQuality)
		v1.GET("/recommendations/openshift/quality/vms", api.GetVMRecommendationQuality)
		v1.GET("/recommendations/openshift/quality/gpu", api.GetGPUMIGRecommendationQuality)
		v1.GET("/recommendations/openshift/quality/snapshots", api.GetSnapshotRecommendationQuality)
	})
}

// TestClusterAliasBatch2_UnionLists pins the union contract on every list-shaped
// surface this batch moved: a UUID binds exactly, a substring returns every
// matching cluster, and a value matching nothing is an empty page.
func TestClusterAliasBatch2_UnionLists(t *testing.T) {
	fx := seedClusterAliasBatch2Fixture(t)
	app := clusterAliasBatch2App(unrestrictedHeatmapPerms())

	t.Run("snapshot summary, UUID", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/snapshots/summary", url.Values{"filter[cluster]": {fx.a}})
		require.Equal(t, http.StatusOK, code, "a UUID used to fail the uuid-column bind with a 503; body: %v", body)
		assert.Equal(t, 1, metaCount(body))
	})

	t.Run("snapshot summary, alias substring is the union", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/snapshots/summary", url.Values{"filter[cluster]": {"prod"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 2, metaCount(body))
	})

	t.Run("snapshot summary, alias matching nothing", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/snapshots/summary", url.Values{"filter[cluster]": {"nope"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 0, metaCount(body))
	})

	// The hourly series is per (cluster, node) and its rows carry no cluster
	// column: one cluster or a 400.
	t.Run("node hourly, UUID", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/node/alias-hourly-node/hourly-utilization", url.Values{"cluster_uuid": {fx.a}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 1, metaCount(body))
	})

	t.Run("node hourly, single-cluster alias", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/node/alias-hourly-node/hourly-utilization", url.Values{"cluster_uuid": {"staging"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 1, metaCount(body))
	})

	t.Run("node hourly, alias matching several clusters", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/node/alias-hourly-node/hourly-utilization", url.Values{"cluster_uuid": {"prod"}})
		require.Equal(t, http.StatusBadRequest, code, "one series cannot interleave two clusters; body: %v", body)
		assert.Contains(t, metaMessage(body), "resolves to 2 clusters")
	})

	t.Run("node hourly, alias matching nothing", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/node/alias-hourly-node/hourly-utilization", url.Values{"cluster_uuid": {"nope"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 0, metaCount(body))
	})

	// GPU time-slicing history rows carry cluster_uuid: union surface.
	t.Run("gpu history, UUID", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/gpu/timeslicing/history", url.Values{"cluster_uuid": {fx.a}, "node_name": {"alias-gpu-node"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 1, metaCount(body))
	})

	t.Run("gpu history, alias substring is the union", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/gpu/timeslicing/history", url.Values{"cluster_uuid": {"prod"}, "node_name": {"alias-gpu-node"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 2, metaCount(body))
	})

	t.Run("gpu history, alias matching nothing", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/gpu/timeslicing/history", url.Values{"cluster_uuid": {"nope"}, "node_name": {"alias-gpu-node"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 0, metaCount(body))
	})

	t.Run("vm history, UUID", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/vms/alias-vm/history", url.Values{
			"cluster_uuid": {fx.a}, "namespace": {"alias-ns"}, "term": {"short"}, "engine": {"cost"},
		})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 1, metaCount(body))
	})

	t.Run("vm history, alias substring is the union", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/vms/alias-vm/history", url.Values{
			"cluster_uuid": {"prod"}, "namespace": {"alias-ns"}, "term": {"short"}, "engine": {"cost"},
		})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 2, metaCount(body))
	})

	t.Run("vm history, alias matching nothing", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/vms/alias-vm/history", url.Values{
			"cluster_uuid": {"nope"}, "namespace": {"alias-ns"}, "term": {"short"}, "engine": {"cost"},
		})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 0, metaCount(body))
	})

	// #968: /history compared the value to the alias EXACTLY. A UUID — the
	// shape every other surface binds — returned nothing.
	historyParams := func(cluster string) url.Values {
		return url.Values{"filter[cluster]": {cluster}}
	}
	t.Run("history, UUID", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/history", historyParams(fx.a))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 1, metaCount(body), "a UUID used to match no alias and return an empty page")
	})

	t.Run("history, alias substring is the union", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/history", historyParams("prod"))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 2, metaCount(body), "a substring used to be compared to the full alias and return an empty page")
	})

	t.Run("history, alias matching nothing", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/history", historyParams("nope"))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 0, metaCount(body))
	})

	// The quality families run the same mapper → ApplyQueryParams path as
	// /history, but against a different table alias (`q`/`c` joins). One
	// end-to-end leg here proves the atoms land in a working query rather than
	// only in the parameter map.
	t.Run("quality, UUID", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/quality", historyParams(fx.a))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 1, metaCount(body), "a UUID used to match no alias and return an empty page")
	})

	t.Run("quality, alias substring is the union", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/quality", historyParams("prod"))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 2, metaCount(body))
	})

	t.Run("quality, alias matching nothing", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/quality", historyParams("nope"))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 0, metaCount(body))
	})

	// #968 acceptance: exclude negates the membership, by UUID and by alias.
	// These were dropped silently before — the mappers read include values only.
	t.Run("history, exclude by UUID", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/history", url.Values{"exclude[cluster]": {fx.a}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 2, metaCount(body), "excluding one of three clusters leaves the other two")
	})

	t.Run("history, exclude by alias", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/history", url.Values{"exclude[cluster]": {"alias-prod-east"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 2, metaCount(body))
	})

	t.Run("history, exclude by substring is not a substring match", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/history", url.Values{"exclude[cluster]": {"prod"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 3, metaCount(body), "exclude negates the exact alias; it is not an ILIKE")
	})

	// #973: exclude matches the alias case-insensitively. A copied alias in a
	// different case used to be a silent no-op — the page came back whole, which
	// reads as "the cluster has no rows".
	t.Run("history, exclude by alias in another case", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/history", url.Values{"exclude[cluster]": {"ALIAS-PROD-EAST"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 2, metaCount(body), "an exclude in a different case must remove the same cluster as the lowercase alias")
	})
}

// TestClusterAliasBatch2_QualityFamiliesNeedOnlyTheMapping pins the six mappers
// that emitted the exact-alias key: each now produces the same atoms the
// container lists use, so a UUID and a substring both bind. The end-to-end leg
// above proves the SQL path; this proves every call site was migrated (a missed
// mapper would keep the old key and silently return nothing).
func TestClusterAliasBatch2_QualityFamiliesNeedOnlyTheMapping(t *testing.T) {
	mappers := []struct {
		name string
		fn   func(echo.Context) (map[string]interface{}, error)
	}{
		{"history", api.MapHistoryQueryParameters},
		{"quality", api.MapQualityQueryParameters},
		{"quality pvcs", api.MapPVCQualityQueryParameters},
		{"quality vms", api.MapVMQualityQueryParameters},
		{"quality gpu", api.MapGPUMIGQualityQueryParameters},
		{"quality snapshots", api.MapSnapshotQualityQueryParameters},
	}

	mapped := func(t *testing.T, fn func(echo.Context) (map[string]interface{}, error), rawQuery string) map[string]interface{} {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)
		e := echo.New()
		params, err := fn(e.NewContext(req, httptest.NewRecorder()))
		require.NoError(t, err)
		return params
	}

	for _, m := range mappers {
		t.Run(m.name+", UUID binds the uuid column", func(t *testing.T) {
			clusterUUID := "11111111-2222-3333-4444-555555555555"
			params := mapped(t, m.fn, "filter[cluster]="+clusterUUID)
			require.Contains(t, params, "c.cluster_uuid = ?")
			assert.Equal(t, []string{clusterUUID}, params["c.cluster_uuid = ?"])
			for key := range params {
				assert.NotContains(t, key, "cluster_alias IN ?", "the exact-alias key must be gone")
			}
		})

		t.Run(m.name+", alias is a case-insensitive substring", func(t *testing.T) {
			params := mapped(t, m.fn, "filter[cluster]=Prod")
			require.Contains(t, params, `c.cluster_alias ILIKE ? ESCAPE '\'`)
			assert.Equal(t, []string{"%Prod%"}, params[`c.cluster_alias ILIKE ? ESCAPE '\'`])
		})

		t.Run(m.name+", several values are OR'd", func(t *testing.T) {
			params := mapped(t, m.fn, "filter[cluster]=prod&filter[cluster]=stage")
			require.Contains(t, params, `c.cluster_alias ILIKE ? ESCAPE '\' OR c.cluster_alias ILIKE ? ESCAPE '\'`)
			assert.Equal(t, []string{"%prod%", "%stage%"}, params[`c.cluster_alias ILIKE ? ESCAPE '\' OR c.cluster_alias ILIKE ? ESCAPE '\'`])
		})

		// #968 acceptance: exclude keeps negating the membership and exact
		// binds the alias literally. Both used to be dropped silently —
		// only filter[cluster] was read.
		t.Run(m.name+", exclude by UUID negates the membership", func(t *testing.T) {
			params := mapped(t, m.fn, "exclude[cluster]=11111111-2222-3333-4444-555555555555")
			require.Contains(t, params, "c.cluster_uuid != ?")
			assert.Equal(t, []string{"11111111-2222-3333-4444-555555555555"}, params["c.cluster_uuid != ?"])
		})

		t.Run(m.name+", exclude by alias negates the alias, case-insensitively", func(t *testing.T) {
			params := mapped(t, m.fn, "exclude[cluster]=prod-east")
			require.Contains(t, params, "LOWER(c.cluster_alias) != LOWER(?)")
			assert.Equal(t, []string{"prod-east"}, params["LOWER(c.cluster_alias) != LOWER(?)"])

			// #973: the comparison is case-insensitive, so an alias copied in a
			// different case excludes the same cluster instead of silently
			// excluding nothing.
			params = mapped(t, m.fn, "exclude[cluster]=PROD-EAST")
			require.Contains(t, params, "LOWER(c.cluster_alias) != LOWER(?)")
			assert.Equal(t, []string{"PROD-EAST"}, params["LOWER(c.cluster_alias) != LOWER(?)"])
		})

		t.Run(m.name+", exact binds the alias literally", func(t *testing.T) {
			params := mapped(t, m.fn, "filter[exact:cluster]=Prod-East")
			require.Contains(t, params, "c.cluster_alias = ?")
			assert.Equal(t, []string{"Prod-East"}, params["c.cluster_alias = ?"])
		})
	}
}

// TestClusterAliasBatch2_DetailRoutesNeedOneCluster pins the single-object
// routes: a UUID and a single-cluster alias resolve, an alias matching several
// readable clusters is the shared 400 (never one cluster's object picked for
// the caller), and a value resolving to nothing keeps the route's 404.
func TestClusterAliasBatch2_DetailRoutesNeedOneCluster(t *testing.T) {
	fx := seedClusterAliasBatch2Fixture(t)
	app := clusterAliasBatch2App(unrestrictedHeatmapPerms())

	cases := []struct {
		name  string
		path  string
		rest  url.Values
		param string
	}{
		{"vm detail", "/vm/detail", url.Values{"vm_name": {"alias-vm"}, "namespace": {"alias-ns"}}, "cluster_uuid"},
		{"pvc detail", "/pvcs/detail", url.Values{"namespace": {"alias-ns"}, "persistentvolumeclaim": {"alias-pvc"}}, "cluster_uuid"},
		{"quota detail", "/quota/detail", url.Values{"namespace": {"alias-ns"}, "quota_name": {"alias-quota"}}, "cluster_uuid"},
		{"cluster-quota detail", "/cluster-quota/detail", url.Values{"cluster_quota_name": {"alias-crq"}}, "cluster_uuid"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+", UUID resolves", func(t *testing.T) {
			params := url.Values{}
			for k, v := range tc.rest {
				params[k] = v
			}
			params[tc.param] = []string{fx.a}
			code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+tc.path, params)
			require.Equal(t, http.StatusOK, code, metaMessage(body))
		})

		t.Run(tc.name+", single-cluster alias resolves", func(t *testing.T) {
			params := url.Values{}
			for k, v := range tc.rest {
				params[k] = v
			}
			params[tc.param] = []string{"prod-east"}
			code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+tc.path, params)
			require.Equal(t, http.StatusOK, code, "an alias that resolves to one cluster must not 404; body: %v", body)
		})

		t.Run(tc.name+", alias matching several clusters", func(t *testing.T) {
			params := url.Values{}
			for k, v := range tc.rest {
				params[k] = v
			}
			params[tc.param] = []string{"prod"}
			code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+tc.path, params)
			require.Equal(t, http.StatusBadRequest, code, "one object cannot carry two clusters; body: %v", body)
			assert.Contains(t, metaMessage(body), "resolves to 2 clusters")
		})

		t.Run(tc.name+", alias matching nothing keeps the 404", func(t *testing.T) {
			params := url.Values{}
			for k, v := range tc.rest {
				params[k] = v
			}
			params[tc.param] = []string{"nope"}
			code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+tc.path, params)
			require.Equal(t, http.StatusNotFound, code, "body: %v", body)
		})
	}
}

// TestClusterAliasBatch2_PartialGrant pins the permission half of the batch:
// the union is always intersected with the caller's grant, and an alias
// resolving to several clusters does not become "ambiguous" just because the
// caller may read only one of them.
func TestClusterAliasBatch2_PartialGrant(t *testing.T) {
	fx := seedClusterAliasBatch2Fixture(t)
	// A only (alias-prod-east); B belongs to the same "prod" union.
	app := clusterAliasBatch2App(map[string][]string{
		"openshift.cluster": {fx.a},
		"openshift.node":    {"*"},
	})

	t.Run("snapshot summary, union narrowed to the grant", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/snapshots/summary", url.Values{"filter[cluster]": {"prod"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 1, metaCount(body))
	})

	t.Run("gpu history, union narrowed to the grant", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/gpu/timeslicing/history", url.Values{"cluster_uuid": {"prod"}, "node_name": {"alias-gpu-node"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 1, metaCount(body), "the union must not widen the grant")
	})

	t.Run("history, union narrowed to the grant", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/history", url.Values{"filter[cluster]": {"prod"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 1, metaCount(body))
	})

	t.Run("pvc detail, alias resolved inside the grant", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/pvcs/detail", url.Values{
			"cluster_uuid": {"prod"}, "namespace": {"alias-ns"}, "persistentvolumeclaim": {"alias-pvc"},
		})
		require.Equal(t, http.StatusOK, code, "prod narrows to the granted cluster, so this is one cluster, not an ambiguous alias; body: %v", body)
	})

	t.Run("node hourly, grant narrows the alias to one cluster", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/node/alias-hourly-node/hourly-utilization", url.Values{"cluster_uuid": {"prod"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 1, metaCount(body))
	})

	t.Run("vm history, alias outside the grant is empty", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBatch2Base+"/vms/alias-vm/history", url.Values{
			"cluster_uuid": {"staging"}, "namespace": {"alias-ns"}, "term": {"short"}, "engine": {"cost"},
		})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 0, metaCount(body))
	})
}

// TestClusterAliasBatch2_ExactAliasBindsLiterally is the end-to-end leg #968
// was missing: the mapper-level assertion proved the mapping emitted
// `c.cluster_alias = ?`, but nothing proved the query still carried it.
// `ApplyQueryParams` drops keys absent from the native allowlist with a
// warning, so a missing atom rendered `filter[exact:cluster]=<alias>` as a
// silent no-op — 200 with every cluster's rows (found by the 2026-11-27 live
// replay, not by the unit suite).
//
// The assertions are on ROWS RETURNED, not on the mapper's map: the partial
// alias leg is the discriminator, because pre-fix it returns the unfiltered
// three-cluster page while post-fix it returns nothing.
func TestClusterAliasBatch2_ExactAliasBindsLiterally(t *testing.T) {
	fx := seedClusterAliasBatch2Fixture(t)
	app := clusterAliasBatch2App(unrestrictedHeatmapPerms())

	surfaces := []struct{ name, path string }{
		{"history", clusterAliasBatch2Base + "/history"},
		{"quality", clusterAliasBatch2Base + "/quality"},
	}
	legs := []struct {
		name        string
		value       string
		wantRows    int
		wantCluster string
	}{
		// The full alias is also the substring shape's answer, so on its own
		// it cannot tell a bound clause from a dropped one; the row identity
		// is what makes it meaningful.
		{"full alias of the first cluster", "alias-prod-east", 1, fx.a},
		{"full alias of the third cluster", "alias-staging", 1, fx.c},
		// The discriminator: not a full alias, so a literal bind matches
		// nothing. Pre-fix the clause is dropped and all three rows return.
		{"partial alias is not a literal match", "alias-prod", 0, ""},
	}
	for _, s := range surfaces {
		for _, l := range legs {
			code, body := getJSON(t, app, fx.orgID, s.path, url.Values{"filter[exact:cluster]": {l.value}})
			require.Equal(t, http.StatusOK, code, metaMessage(body))
			require.Equal(t, l.wantRows, metaCount(body),
				"%s: filter[exact:cluster]=%q must bind the alias literally; a count above the expectation means the applied query dropped `c.cluster_alias = ?` (the native allowlist atom) and returned every cluster's rows",
				s.name, l.value)
			if l.wantCluster != "" {
				data, _ := body["data"].([]any)
				require.Len(t, data, 1, "%s: one row for %q", s.name, l.value)
				row, _ := data[0].(map[string]any)
				assert.Equal(t, l.wantCluster, row["cluster_uuid"],
					"%s: filter[exact:cluster]=%q must return that cluster's row (row identity, not just count)",
					s.name, l.value)
			}
		}
	}
}
