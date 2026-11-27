package api_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/clustercache"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #950/#952 matrix: filter[cluster] must mean one thing on every cluster
// surface. A UUID binds exactly; any other value is an org-scoped,
// case-insensitive substring over cluster_alias and scopes the request to the
// UNION of every matching cluster, intersected with RBAC (#790/#947).
//
// These tests pin the five surfaces that still compared the value to a UUID
// literally — GPU time-slicing detail, fleet heatmap, /vm, /machinesets and the
// savings-summary grouped paths — and the RBAC intersection that keeps the
// union from widening a grant.

const clusterAliasBase = "/api/cost-management/v1/recommendations/openshift"

// clusterAliasFixture seeds three clusters whose aliases share a substring:
// "prod" matches two of them (the union case), "staging" matches exactly one,
// and "nope" matches none. Every surface gets rows on all three, so a partial
// grant is observable as a lower count rather than an unexplained empty page.
type clusterAliasFixture struct {
	orgID string
	a     string
	b     string
	c     string
}

func seedClusterAliasFixture(t *testing.T) clusterAliasFixture {
	t.Helper()

	orgID := "org-cluster-alias-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	database.DB = testutil.OpenTestGORM(pool)
	clustercache.ResetForTest()
	fleetheatmap.ResetForTest()
	fleetsummary.ResetForTest()
	t.Cleanup(func() {
		database.Pool = nil
		database.DB = nil
		clustercache.ResetForTest()
		fleetheatmap.ResetForTest()
		fleetsummary.ResetForTest()
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

	fx := clusterAliasFixture{
		orgID: orgID,
		a:     uuid.New().String(),
		b:     uuid.New().String(),
		c:     uuid.New().String(),
	}
	// clusters_fill_org_id fills org_id from rh_accounts via tenant_id, so the
	// plain insert form the other fixtures use is enough here too.
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

	// Node recommendations, term 'medium' — the default of both readers below
	// (heatmap and machine sets). Two nodes in A (one MachineSet), one each in
	// B and C.
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, category, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes,
			machineset_name, instance_type, node_count_reduction,
			estimated_savings_cents, updated_at
		) VALUES
			($1, $2::uuid, 'alias-node-a1', 'medium', 'cost', 0.5, 0.5, 0.5, 0.5, 1.0, 'optimized', 'active', NULL, 5, 0, '{}', 'ms-a', 'm5.large', 0, 1000, now()),
			($1, $2::uuid, 'alias-node-a2', 'medium', 'cost', 0.5, 0.5, 0.5, 0.5, 1.0, 'optimized', 'active', NULL, 5, 0, '{}', 'ms-a', 'm5.large', 0, 1000, now()),
			($1, $3::uuid, 'alias-node-b1', 'medium', 'cost', 0.5, 0.5, 0.5, 0.5, 1.0, 'optimized', 'active', NULL, 5, 0, '{}', 'ms-b', 'm5.large', 0, 1000, now()),
			($1, $4::uuid, 'alias-node-c1', 'medium', 'cost', 0.5, 0.5, 0.5, 0.5, 1.0, 'optimized', 'active', NULL, 5, 0, '{}', 'ms-c', 'm5.large', 0, 1000, now())`,
		fx.orgID, fx.a, fx.b, fx.c)
	require.NoError(t, err)

	// GPU time-slicing: one row per cluster, so the list count is the number
	// of clusters the filter resolves to.
	_, err = pool.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendations (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, confidence_level,
			candidate_count, impacted_count,
			candidate_containers, impacted_containers,
			notification_codes, estimated_savings_cents, savings_per_gpu_cents
		) VALUES
			($1, $2::uuid, 'gpu-node-a', 'Tesla T4', 'short', 4, 0.7, 0.7, 1, 0, '[]'::jsonb, '[]'::jsonb, '{36}', 40000, 10000),
			($1, $3::uuid, 'gpu-node-b', 'Tesla T4', 'short', 4, 0.7, 0.7, 1, 0, '[]'::jsonb, '[]'::jsonb, '{36}', 40000, 10000),
			($1, $4::uuid, 'gpu-node-c', 'Tesla T4', 'short', 4, 0.7, 0.7, 1, 0, '[]'::jsonb, '[]'::jsonb, '{36}', 40000, 10000)`,
		fx.orgID, fx.a, fx.b, fx.c)
	require.NoError(t, err)

	// VMs: one per cluster. /vm applies no term filter when none is asked for,
	// so the storage spelling only has to be a legal one.
	_, err = pool.Exec(ctx, `
		INSERT INTO vm_recommendations (
			org_id, cluster_uuid, vm_name, namespace, guest_os,
			current_vcpu, current_memory_gib, recommended_vcpu, recommended_memory_gib,
			guest_agent_detected, confidence, term, engine,
			category, estimated_savings_cents, savings_currency, last_recommended_at
		) VALUES
			($1, $2::uuid, 'vm-a', 'alias-ns', 'RHEL 9.2', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'optimized', 1000, 'USD', now()),
			($1, $3::uuid, 'vm-b', 'alias-ns', 'RHEL 9.2', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'optimized', 1000, 'USD', now()),
			($1, $4::uuid, 'vm-c', 'alias-ns', 'RHEL 9.2', 4, 16, 2, 8, true, 'high', 'medium_term', 'cost', 'optimized', 1000, 'USD', now())`,
		fx.orgID, fx.a, fx.b, fx.c)
	require.NoError(t, err)

	// Container recommendation sets for the savings-summary idle_state
	// grouping: A contributes idle + zombie, B and C one idle each.
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			term, engine, stale, idle_state, notification_codes,
			estimated_savings_cents, estimated_waste_cents, updated_at
		) VALUES
			($1, $2::uuid, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, 'idle',   '{}', 50000, 50000, now()),
			($1, $2::uuid, 'ns1', 'w2', 'Deployment', 'c2', 'medium', 'cost', false, 'zombie', '{}', 30000, 30000, now()),
			($1, $3::uuid, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, 'idle',   '{}', 50000, 50000, now()),
			($1, $4::uuid, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, 'idle',   '{}', 50000, 50000, now())`,
		fx.orgID, fx.a, fx.b, fx.c)
	require.NoError(t, err)

	return fx
}

// clusterAliasApp mounts every surface this matrix covers, carrying fixed
// permissions (the permsApp pattern) so one fixture serves the unrestricted
// leg and the partial-grant leg.
func clusterAliasApp(perms map[string][]string) *echo.Echo {
	return permsApp(perms, func(v1 *echo.Group) {
		v1.GET("/recommendations/openshift/gpu/timeslicing", api.GetNodeRecommendations)
		v1.GET("/recommendations/openshift/gpu/timeslicing/:node", api.GetNodeGPUTimeslicingDetail)
		v1.GET("/recommendations/openshift/nodes/:node", api.GetNodeUtilizationDetail)
		v1.GET("/recommendations/openshift/fleet-heatmap", api.GetFleetHeatmap)
		v1.GET("/recommendations/openshift/vm", api.GetVMRecommendations)
		v1.GET("/recommendations/openshift/machinesets", api.GetMachineSetRecommendations)
		v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)
	})
}

func clusterAliasParams(filter string) url.Values {
	return url.Values{"filter[cluster]": {filter}}
}

func savingsIdleParams(filter string) url.Values {
	return url.Values{"group_by[idle_state]": {"*"}, "filter[cluster]": {filter}}
}

// savingsIdleRows maps idle_state -> container_count, which is how the
// grouping shows cluster scope without depending on currency conversion.
func savingsIdleRows(t *testing.T, body map[string]any) map[string]int {
	t.Helper()
	out := map[string]int{}
	data, _ := body["data"].([]any)
	for _, raw := range data {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		state, _ := row["idle_state"].(string)
		if n, ok := row["container_count"].(float64); ok {
			out[state] = int(n)
		}
	}
	return out
}

// TestClusterAlias_UnionEverySurface pins the union contract: a substring alias
// returns every matching cluster's rows instead of an empty page (or, on the
// detail route, a 404).
func TestClusterAlias_UnionEverySurface(t *testing.T) {
	fx := seedClusterAliasFixture(t)
	app := clusterAliasApp(unrestrictedHeatmapPerms())

	cases := []struct {
		name   string
		path   string
		params url.Values
		want   int
	}{
		// GPU time-slicing list: one row per cluster.
		{"gpu list, alias matching two clusters", "/gpu/timeslicing", clusterAliasParams("prod"), 2},
		{"gpu list, alias matching one cluster", "/gpu/timeslicing", clusterAliasParams("staging"), 1},
		{"gpu list, alias matching nothing", "/gpu/timeslicing", clusterAliasParams("nope"), 0},
		{"gpu list, uuid filter stays exact", "/gpu/timeslicing", clusterAliasParams(fx.a), 1},

		// Fleet heatmap: one row per node; three of them sit under "prod".
		{"heatmap, alias matching two clusters", "/fleet-heatmap", clusterAliasParams("prod"), 3},
		{"heatmap, alias matching one cluster", "/fleet-heatmap", clusterAliasParams("staging"), 1},
		{"heatmap, alias matching nothing", "/fleet-heatmap", clusterAliasParams("nope"), 0},

		// VM list: one recommendation per cluster.
		{"vm, alias matching two clusters", "/vm", clusterAliasParams("prod"), 2},
		{"vm, alias matching one cluster", "/vm", clusterAliasParams("staging"), 1},
		{"vm, alias matching nothing", "/vm", clusterAliasParams("nope"), 0},

		// Machine sets: ms-a (A) and ms-b (B) under "prod".
		{"machinesets, alias matching two clusters", "/machinesets", clusterAliasParams("prod"), 2},
		{"machinesets, alias matching one cluster", "/machinesets", clusterAliasParams("staging"), 1},
		{"machinesets, alias matching nothing", "/machinesets", clusterAliasParams("nope"), 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := getJSON(t, app, fx.orgID, clusterAliasBase+tc.path, tc.params)
			require.Equal(t, http.StatusOK, code, metaMessage(body))
			assert.Equal(t, tc.want, metaCount(body), "rows: %v", body["data"])
		})
	}

	t.Run("savings grouped, alias matching two clusters", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/savings-summary", savingsIdleParams("prod"))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		// A contributes idle + zombie, B contributes idle: both states are
		// present and the idle bucket holds A's and B's containers.
		assert.Equal(t, map[string]int{"idle": 2, "zombie": 1}, savingsIdleRows(t, body), "rows: %v", body["data"])
	})

	t.Run("savings grouped, alias matching one cluster", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/savings-summary", savingsIdleParams("staging"))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, map[string]int{"idle": 1}, savingsIdleRows(t, body), "rows: %v", body["data"])
	})

	t.Run("savings grouped, alias matching nothing", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/savings-summary", savingsIdleParams("nope"))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Empty(t, savingsIdleRows(t, body))
		assert.Equal(t, 0, metaCount(body))
	})

	// The detail route keeps its 404: it is a node lookup, so a filter that
	// resolves to no readable cluster is an unknown node there.
	t.Run("gpu detail, alias matching the cluster that holds the node", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/gpu/timeslicing/gpu-node-a", clusterAliasParams("prod"))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
	})

	t.Run("gpu detail, alias matching a different cluster", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/gpu/timeslicing/gpu-node-a", clusterAliasParams("staging"))
		require.Equal(t, http.StatusNotFound, code, "a filter that resolves to no readable cluster must 404; body: %v", body)
	})

	t.Run("gpu detail, alias matching nothing", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/gpu/timeslicing/gpu-node-a", clusterAliasParams("nope"))
		require.Equal(t, http.StatusNotFound, code, "body: %v", body)
	})

	// /nodes/{node} is the same shape and had the same exact scan. It reads
	// the flat cluster/cluster_uuid form only (no bracket alias), so this leg
	// stays on the flat parameter.
	//
	// #964: this route renders ONE node object, so it needs one cluster. An
	// alias resolving to several readable clusters used to merge their
	// term/engine variants under a single cluster_uuid; it now answers the
	// shared 400 and names the way out.
	t.Run("node detail, single-cluster alias holding the node", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/nodes/alias-node-a1", url.Values{"cluster": {"prod-east"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
	})

	t.Run("node detail, alias matching several clusters", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/nodes/alias-node-a1", url.Values{"cluster": {"prod"}})
		require.Equal(t, http.StatusBadRequest, code, "one object cannot carry two clusters; body: %v", body)
		assert.Contains(t, metaMessage(body), "resolves to 2 clusters")
	})

	t.Run("node detail, alias matching a different cluster", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/nodes/alias-node-a1", url.Values{"cluster": {"staging"}})
		require.Equal(t, http.StatusNotFound, code, "a filter that resolves to no readable cluster must 404; body: %v", body)
	})

	t.Run("node detail, alias matching nothing", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/nodes/alias-node-a1", url.Values{"cluster": {"nope"}})
		require.Equal(t, http.StatusNotFound, code, "body: %v", body)
	})
}

// TestClusterAlias_PartialGrant_EverySurface pins the permission half: the
// union is always intersected with the caller's granted clusters, so an alias
// resolving to more clusters than the caller may read returns only theirs, and
// an alias resolving only to ungranted clusters returns empty — never a 200
// carrying another cluster's rows.
func TestClusterAlias_PartialGrant_EverySurface(t *testing.T) {
	fx := seedClusterAliasFixture(t)
	app := clusterAliasApp(map[string][]string{"openshift.cluster": {fx.a}})

	cases := []struct {
		name   string
		path   string
		params url.Values
		want   int
	}{
		{"gpu list, union narrowed to the grant", "/gpu/timeslicing", clusterAliasParams("prod"), 1},
		{"gpu list, alias resolving only to an ungranted cluster", "/gpu/timeslicing", clusterAliasParams("staging"), 0},
		{"heatmap, union narrowed to the grant", "/fleet-heatmap", clusterAliasParams("prod"), 2},
		{"heatmap, alias resolving only to an ungranted cluster", "/fleet-heatmap", clusterAliasParams("staging"), 0},
		{"vm, union narrowed to the grant", "/vm", clusterAliasParams("prod"), 1},
		{"vm, alias resolving only to an ungranted cluster", "/vm", clusterAliasParams("staging"), 0},
		{"machinesets, union narrowed to the grant", "/machinesets", clusterAliasParams("prod"), 1},
		{"machinesets, alias resolving only to an ungranted cluster", "/machinesets", clusterAliasParams("staging"), 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := getJSON(t, app, fx.orgID, clusterAliasBase+tc.path, tc.params)
			require.Equal(t, http.StatusOK, code, metaMessage(body))
			assert.Equal(t, tc.want, metaCount(body), "rows: %v", body["data"])
		})
	}

	t.Run("savings grouped, union narrowed to the grant", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/savings-summary", savingsIdleParams("prod"))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		// A only: idle (c1) and zombie (c2). B's idle container is outside the
		// grant and must not be counted.
		assert.Equal(t, map[string]int{"idle": 1, "zombie": 1}, savingsIdleRows(t, body), "rows: %v", body["data"])
	})

	t.Run("savings grouped, alias resolving only to an ungranted cluster", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/savings-summary", savingsIdleParams("staging"))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Empty(t, savingsIdleRows(t, body))
	})

	t.Run("gpu detail, node in the granted cluster", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/gpu/timeslicing/gpu-node-a", clusterAliasParams("prod"))
		require.Equal(t, http.StatusOK, code, metaMessage(body))
	})

	t.Run("gpu detail, node in an ungranted cluster of the same union", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/gpu/timeslicing/gpu-node-b", clusterAliasParams("prod"))
		require.Equal(t, http.StatusNotFound, code, "the union must not widen the grant; body: %v", body)
	})

	t.Run("node detail, node in the granted cluster", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/nodes/alias-node-a1", url.Values{"cluster": {"prod"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
	})

	t.Run("node detail, node in an ungranted cluster of the same union", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, clusterAliasBase+"/nodes/alias-node-b1", url.Values{"cluster": {"prod"}})
		require.Equal(t, http.StatusNotFound, code, "the union must not widen the grant; body: %v", body)
	})
}
