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
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #754: the fleet heatmap cache key carries the cluster dimension only, so a
// wildcard-cluster role with a narrow openshift.node grant shares the org:all
// entry with an unrestricted role, and a cache hit returns before the node
// filter. These tests pin node scope on every path — cold, warm, both warm
// orders — against the package-global fleetheatmap cache.

func unrestrictedHeatmapPerms() map[string][]string {
	return map[string][]string{"*": {"*"}}
}

func nodeScopedHeatmapPerms(nodes ...string) map[string][]string {
	return map[string][]string{
		"openshift.cluster": {"*"},
		"openshift.node":    nodes,
	}
}

// heatmapRBACApp builds an app whose requests carry fixed permissions. The
// fleetheatmap cache is package-global: separate apps share it, which is
// exactly the surface under test.
func heatmapRBACApp(perms map[string][]string) *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	if perms != nil {
		v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				c.Set("user.permissions", perms)
				return next(c)
			}
		})
	}
	v1.GET("/recommendations/openshift/fleet-heatmap", api.GetFleetHeatmap)
	return app
}

// seedHeatmapNodes clears the org's node_recommendations and seeds one row
// per name (term medium, engine cost — the defaults the existing integration
// tests use).
func seedHeatmapNodes(t *testing.T, ctx context.Context, nodeNames ...string) {
	t.Helper()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'heatmap-rbac-cluster', 'src-hm-rbac', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM node_recommendations WHERE org_id = $1`, testutil.TestOrgID)
	require.NoError(t, err)
	for _, name := range nodeNames {
		_, err = pool.Exec(ctx, `
			INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine,
				cpu_util_p95, mem_util_p95, idle_state, machineset_name, instance_type,
				node_count_reduction, estimated_savings_cents, updated_at)
			VALUES ($1, $2, $3, 'medium', 'cost', 0.50, 0.55, 'active', 'ms-rbac', 'm5.large', 0, 1000, now())`,
			testutil.TestOrgID, testutil.TestClusterUUID, name)
		require.NoError(t, err)
	}
}

func setupHeatmapRBAC(t *testing.T) {
	t.Helper()
	t.Setenv("RBAC_ENABLE", "true")
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	fleetheatmap.ResetForTest()
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
		fleetheatmap.ResetForTest()
	})
}

func getHeatmapAsFor(t *testing.T, app *echo.Echo, orgID string) (api.FleetHeatmapResponse, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-heatmap", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp api.FleetHeatmapResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp, rec.Body.String()
}

func getHeatmapAs(t *testing.T, app *echo.Echo) (api.FleetHeatmapResponse, string) {
	return getHeatmapAsFor(t, app, testutil.TestOrgID)
}

func heatmapNodeNames(resp api.FleetHeatmapResponse) []string {
	names := make([]string, 0, len(resp.Data))
	for _, n := range resp.Data {
		names = append(names, n.Node)
	}
	return names
}

// TestFleetHeatmap_RBAC_NodeScopeOnCacheHit (#754 criterion 1, the
// demonstrated leak): an unrestricted user warms the org:all entry, then a
// wildcard-cluster/narrow-node user requests the same dimensions and hits
// that entry. Rules out: a cache hit returning before the node filter.
func TestFleetHeatmap_RBAC_NodeScopeOnCacheHit(t *testing.T) {
	setupHeatmapRBAC(t)
	ctx := context.Background()
	seedHeatmapNodes(t, ctx, "visible-node", "secret-node")

	warm, _ := getHeatmapAs(t, heatmapRBACApp(unrestrictedHeatmapPerms()))
	require.Equal(t, 2, warm.Meta.Count, "unrestricted warm request sees both nodes")

	// Prove the next request is a cache HIT, not a recompute (#754 review):
	// delete the granted node from the DB — a hit still serves the warmed
	// org-level pool (filtered to the grant); a recompute would see nothing.
	_, err := database.GetPool().Exec(context.Background(),
		`DELETE FROM node_recommendations WHERE org_id = $1 AND node = 'visible-node'`, testutil.TestOrgID)
	require.NoError(t, err)

	hit, _ := getHeatmapAs(t, heatmapRBACApp(nodeScopedHeatmapPerms("visible-node")))
	assert.Equal(t, 1, hit.Meta.Count,
		"node-scoped user must see only their granted node on a cache hit (pre-fix: count=2 with secret-node)")
	assert.ElementsMatch(t, []string{"visible-node"}, heatmapNodeNames(hit),
		"secret-node must never be served to a node-scoped user; the granted node must come from the warmed pool")
}

// TestFleetHeatmap_RBAC_NoPoisoningFromRestrictedWarm (#754 criterion 2): a
// restricted user warming first must not shrink the unrestricted user's view
// through a partial shared entry.
func TestFleetHeatmap_RBAC_NoPoisoningFromRestrictedWarm(t *testing.T) {
	setupHeatmapRBAC(t)
	ctx := context.Background()
	seedHeatmapNodes(t, ctx, "visible-node", "secret-node")

	restricted, _ := getHeatmapAs(t, heatmapRBACApp(nodeScopedHeatmapPerms("visible-node")))
	require.Equal(t, 1, restricted.Meta.Count, "restricted cold request sees only the granted node")

	// Prove the next request is a cache HIT (#754 review): delete the
	// secret node — an unrestricted HIT still serves the warmed org-level
	// pool; a recompute would see only the granted node.
	_, err := database.GetPool().Exec(context.Background(),
		`DELETE FROM node_recommendations WHERE org_id = $1 AND node = 'secret-node'`, testutil.TestOrgID)
	require.NoError(t, err)

	unrestricted, _ := getHeatmapAs(t, heatmapRBACApp(unrestrictedHeatmapPerms()))
	assert.Equal(t, 2, unrestricted.Meta.Count,
		"a restricted first caller must not poison the shared entry with a partial view (pre-fix: count=1)")
	assert.ElementsMatch(t, []string{"visible-node", "secret-node"}, heatmapNodeNames(unrestricted),
		"the warmed org-level pool must survive a restricted warm read unchanged")
}

// TestFleetHeatmap_RBAC_ColdWarmParityAndImmutability (#754 criterion 4):
// the restricted user's cold and warm responses are identical, and a
// restricted warm read must not mutate the shared cached data — the
// unrestricted user still sees both nodes afterwards.
func TestFleetHeatmap_RBAC_ColdWarmParityAndImmutability(t *testing.T) {
	setupHeatmapRBAC(t)
	ctx := context.Background()
	seedHeatmapNodes(t, ctx, "visible-node", "secret-node")

	_, coldBody := getHeatmapAs(t, heatmapRBACApp(nodeScopedHeatmapPerms("visible-node")))
	warm, warmBody := getHeatmapAs(t, heatmapRBACApp(nodeScopedHeatmapPerms("visible-node")))
	assert.Equal(t, coldBody, warmBody, "cold and warm responses for the same scoped user must be identical")
	assert.Equal(t, 1, warm.Meta.Count)

	after, _ := getHeatmapAs(t, heatmapRBACApp(unrestrictedHeatmapPerms()))
	assert.Equal(t, 2, after.Meta.Count,
		"a scoped warm read must not mutate the shared cached pool (rules out in-place filtering)")
	assert.ElementsMatch(t, []string{"visible-node", "secret-node"}, heatmapNodeNames(after))
}

// TestFleetHeatmap_RBAC_MaxNodesPerUser: maxNodes truncation and the cap
// warning are per-user properties computed after node filtering — count must
// always match the served data.
func TestFleetHeatmap_RBAC_MaxNodesPerUser(t *testing.T) {
	t.Setenv("RBAC_ENABLE", "true")
	t.Setenv("ROS_FLEET_HEATMAP_MAX_NODES", "2")
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	fleetheatmap.ResetForTest()
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
		fleetheatmap.ResetForTest()
	})
	ctx := context.Background()
	seedHeatmapNodes(t, ctx, "node-a", "node-b", "node-c")

	// Grant node-a and node-c only: filtering must happen BEFORE truncation —
	// both granted nodes are served with NO cap warning (truncate-then-filter
	// would lose node-c to the org-level top-2 and serve only node-a).
	scoped, _ := getHeatmapAs(t, heatmapRBACApp(nodeScopedHeatmapPerms("node-a", "node-c")))
	assert.Equal(t, 2, scoped.Meta.Count, "granted nodes are both served — filter runs before truncation")
	assert.ElementsMatch(t, []string{"node-a", "node-c"}, heatmapNodeNames(scoped),
		"node-c must survive: it is granted even though it falls outside the org-level top-2")
	assert.Empty(t, scoped.Meta.Warnings, "no cap warning when the granted set fits within maxNodes")

	full, _ := getHeatmapAs(t, heatmapRBACApp(unrestrictedHeatmapPerms()))
	assert.Equal(t, 2, full.Meta.Count, "unrestricted view truncated to maxNodes")
	assert.Len(t, full.Data, 2)
	if assert.NotEmpty(t, full.Meta.Warnings, "unrestricted truncation must produce the cap warning") {
		assert.Contains(t, full.Meta.Warnings[0], "capped")
	}
}

// TestFleetHeatmap_RBAC_ClusterScopedKeySeparation (#754 review
// strengthening): TWO clusters with the same node names and machinesets —
// rows are distinguishable only by cluster_uuid. A cluster-scoped user must
// receive only their cluster's rows (same-name substitution caught by UUID),
// their key must be distinct from org:all (both warm orders), and the
// org:all entry must survive their cold compute.
func TestFleetHeatmap_RBAC_ClusterScopedKeySeparation(t *testing.T) {
	setupHeatmapRBAC(t)
	ctx := context.Background()
	pool := database.GetPool()
	const clusterB = "99999999-9999-9999-9999-999999999999"
	seedHeatmapNodes(t, ctx, "shared-node", "a-only-node")
	_, err := pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $2, $1, 'heatmap-cluster-b', 'src-hm-b', now()) ON CONFLICT DO NOTHING`, clusterB, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine,
			cpu_util_p95, mem_util_p95, idle_state, machineset_name, instance_type,
			node_count_reduction, estimated_savings_cents, updated_at)
		VALUES ($1, $2, 'shared-node', 'medium', 'cost', 0.50, 0.55, 'active', 'ms-rbac', 'm5.large', 0, 1000, now()),
		       ($1, $2, 'b-only-node', 'medium', 'cost', 0.50, 0.55, 'active', 'ms-rbac', 'm5.large', 0, 1000, now())`,
		testutil.TestOrgID, clusterB)
	require.NoError(t, err)

	// Warm org:all through the unrestricted user.
	full, _ := getHeatmapAs(t, heatmapRBACApp(unrestrictedHeatmapPerms()))
	require.Equal(t, 4, full.Meta.Count, "unrestricted sees both clusters' rows")

	// Cluster-scoped to cluster A with a same-name node grant: the hit set
	// is the org:all entry, but the response must contain only cluster A's
	// shared-node — not cluster B's row of the same name.
	clusterNodeScoped, _ := getHeatmapAs(t, heatmapRBACApp(map[string][]string{
		"openshift.cluster": {testutil.TestClusterUUID},
		"openshift.node":    {"shared-node"},
	}))
	assert.Equal(t, 1, clusterNodeScoped.Meta.Count, "cluster+node scoped user sees exactly their row")
	for _, n := range clusterNodeScoped.Data {
		assert.Equal(t, testutil.TestClusterUUID, n.ClusterUUID,
			"same-name node from another cluster must not substitute (cluster_uuid distinguishes)")
		assert.Equal(t, "shared-node", n.Node)
	}

	// Cluster-scoped without node grants (all nodes in cluster A).
	clusterScoped, _ := getHeatmapAs(t, heatmapRBACApp(map[string][]string{
		"openshift.cluster": {testutil.TestClusterUUID},
	}))
	assert.Equal(t, 2, clusterScoped.Meta.Count, "cluster-scoped user sees only their cluster's nodes")
	for _, n := range clusterScoped.Data {
		assert.Equal(t, testutil.TestClusterUUID, n.ClusterUUID, "no cross-cluster rows")
	}

	// Reverse warm order: the cluster-scoped entry must not poison org:all.
	fleetheatmap.ResetForTest()
	getHeatmapAs(t, heatmapRBACApp(map[string][]string{
		"openshift.cluster": {testutil.TestClusterUUID},
	}))
	orgAll, _ := getHeatmapAs(t, heatmapRBACApp(unrestrictedHeatmapPerms()))
	assert.Equal(t, 4, orgAll.Meta.Count, "org:all recomputes independently of the cluster-scoped entry")
}

// TestFleetHeatmap_RBAC_DisabledServesAll: with RBAC disabled, node grants
// do not filter — cold and warm.
func TestFleetHeatmap_RBAC_DisabledServesAll(t *testing.T) {
	t.Setenv("RBAC_ENABLE", "false") // explicit: ambient env must not flip this test
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	fleetheatmap.ResetForTest()
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
		fleetheatmap.ResetForTest()
	})
	ctx := context.Background()
	seedHeatmapNodes(t, ctx, "visible-node", "secret-node")

	app := heatmapRBACApp(nodeScopedHeatmapPerms("visible-node"))
	cold, _ := getHeatmapAs(t, app)
	assert.Equal(t, 2, cold.Meta.Count, "RBAC disabled: no node filtering on cold path")
	warm, _ := getHeatmapAs(t, app)
	assert.Equal(t, 2, warm.Meta.Count, "RBAC disabled: no node filtering on warm path")
}

// TestFleetHeatmap_RBAC_CrossOrgSameNodeNames (adversarial weakened fixture,
// #754): identical node names in a second org must not widen or mask the
// first org's node-scoped view — org boundary and node grant must compose.
// Seed order is reversed (secret-node first) to rule out insertion-order
// dependence; the pool SQL orders deterministically, so which nodes survive
// any truncation cannot depend on seed order.
func TestFleetHeatmap_RBAC_CrossOrgSameNodeNames(t *testing.T) {
	setupHeatmapRBAC(t)
	ctx := context.Background()
	pool := database.GetPool()
	const orgB = "org-heatmap-collision"
	const clusterB = "99999999-9999-9999-9999-999999999999"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (77, $1) ON CONFLICT DO NOTHING`, orgB)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (77, $2, $1, 'heatmap-org-b', 'src-hm-b', now()) ON CONFLICT DO NOTHING`, clusterB, orgB)
	require.NoError(t, err)
	// Reversed order (secret first) in org A; same names in org B.
	seedHeatmapNodes(t, ctx, "secret-node", "visible-node")
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine,
			cpu_util_p95, mem_util_p95, idle_state, machineset_name, instance_type,
			node_count_reduction, estimated_savings_cents, updated_at)
		VALUES ($1, $2, 'secret-node', 'medium', 'cost', 0.50, 0.55, 'active', 'ms-b', 'm5.large', 0, 1000, now()),
		       ($1, $2, 'visible-node', 'medium', 'cost', 0.50, 0.55, 'active', 'ms-b', 'm5.large', 0, 1000, now())`,
		orgB, clusterB)
	require.NoError(t, err)

	// Warm org A through the unrestricted user, then read as org A's
	// node-scoped user: only org A's visible-node may appear.
	getHeatmapAs(t, heatmapRBACApp(unrestrictedHeatmapPerms()))
	hit, _ := getHeatmapAs(t, heatmapRBACApp(nodeScopedHeatmapPerms("visible-node")))
	assert.Equal(t, 1, hit.Meta.Count,
		"same-name nodes in another org must not widen or mask the scoped view")
	assert.ElementsMatch(t, []string{"visible-node"}, heatmapNodeNames(hit))

	// Org B's own view: same node names, but only org B's rows (distinguished
	// by cluster_uuid) — both orgs see their own data under their own keys.
	orgBView, _ := getHeatmapAsFor(t, heatmapRBACApp(unrestrictedHeatmapPerms()), orgB)
	assert.Equal(t, 2, orgBView.Meta.Count, "org B sees its own rows")
	for _, n := range orgBView.Data {
		assert.Equal(t, clusterB, n.ClusterUUID, "org B must not receive org A's rows")
	}
}
