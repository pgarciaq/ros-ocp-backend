package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// #753: roles with no OpenShift grant (settings-only) must not read
// infrastructure telemetry or recommendations. Koku's permission lattice
// starts resources EMPTY — an absent grant is not unrestricted access. These
// regressions pin the primary repro (node hourly telemetry), the fleet
// surfaces, the compat list, and the settings-routes positive control.

const eligibilityClusterB = "99999999-9999-9999-9999-999999999999"

func eligibilitySettingsReadPerms() map[string][]string {
	return map[string][]string{"settings.read": {"*"}}
}

func eligibilitySettingsWritePerms() map[string][]string {
	return map[string][]string{"settings.write": {"*"}}
}

func eligibilityClusterAPerms() map[string][]string {
	return map[string][]string{"openshift.cluster": {testutil.TestClusterUUID}}
}

// permsApp builds an app whose requests carry fixed permissions (the #754
// heatmapRBACApp pattern, generalized to arbitrary route registration).
func permsApp(perms map[string][]string, register func(*echo.Group)) *echo.Echo {
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
	register(v1)
	return app
}

func setupEligibilityTest(t *testing.T) {
	t.Helper()
	t.Setenv("RBAC_ENABLE", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest) // rebuild config from restored env — no RBACEnabled leak into later tests
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

// seedEligibilityClusters seeds two intra-org clusters: A (the testutil
// cluster, node-a) and B (node-b with the issue's exact telemetry: CPU 617,
// memory 123456, max_pod_count 42).
func seedEligibilityClusters(t *testing.T, ctx context.Context) {
	t.Helper()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $2, $1, 'eligibility-cluster-a', 'src-el-a', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $2, $1, 'eligibility-cluster-b', 'src-el-b', now()) ON CONFLICT DO NOTHING`, eligibilityClusterB, testutil.TestOrgID)
	require.NoError(t, err)

	reportDate := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	testutil.EnsureMonthlyPartition(t, pool, "hourly_node_digests", reportDate)
	_, err = pool.Exec(ctx, `
		INSERT INTO hourly_node_digests (org_id, cluster_uuid, node_name, report_date, hour,
			cpu_usage_p95_mc, mem_usage_p95_kib, sample_count, max_pod_count)
		VALUES ($1, $2, 'node-a', $3, 0, 100, 2048, 4, 10),
		       ($1, $4, 'node-b', $3, 0, 617, 123456, 4, 42)`,
		testutil.TestOrgID, testutil.TestClusterUUID, reportDate, eligibilityClusterB)
	require.NoError(t, err)
}

func nodeHourlyAppPerms(perms map[string][]string) *echo.Echo {
	return permsApp(perms, func(v1 *echo.Group) {
		v1.GET("/recommendations/openshift/node/:id/hourly-utilization", api.GetNodeHourlyUtilization)
	})
}

func getNodeHourlyCount(t *testing.T, app *echo.Echo, node, clusterUUID string) int {
	t.Helper()
	url := fmt.Sprintf(
		"/api/cost-management/v1/recommendations/openshift/node/%s/hourly-utilization?cluster_uuid=%s",
		node, clusterUUID)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp.Meta.Count
}

// TestRBAC_Eligibility_NodeHourly is the #753 primary repro: a settings-only
// role must receive no node telemetry (pre-fix: cluster B's CPU 617, memory
// 123456 and pod count 42 were served). Positive controls pin that the fix
// does not overreach: cluster grants, inheritance and wildcards keep working.
func TestRBAC_Eligibility_NodeHourly(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedEligibilityClusters(t, ctx)

	cases := []struct {
		name    string
		perms   func() map[string][]string
		node    string
		cluster string
		want    int
		ruleOut string
	}{
		{
			name: "settings read only denied", perms: eligibilitySettingsReadPerms,
			node: "node-b", cluster: eligibilityClusterB, want: 0,
			ruleOut: "settings-read-only must not read node telemetry (pre-fix: count=1 with CPU 617/mem 123456/pods 42)",
		},
		{
			name: "settings write only denied", perms: eligibilitySettingsWritePerms,
			node: "node-b", cluster: eligibilityClusterB, want: 0,
			ruleOut: "settings-write-only must not read node telemetry",
		},
		{
			name: "settings both denied", perms: func() map[string][]string {
				return map[string][]string{"settings.read": {"*"}, "settings.write": {"*"}}
			},
			node: "node-b", cluster: eligibilityClusterB, want: 0,
			ruleOut: "combined settings roles hold no data-read grant",
		},
		{
			name: "no permissions denied", perms: func() map[string][]string { return map[string][]string{} },
			node: "node-b", cluster: eligibilityClusterB, want: 0,
			ruleOut: "empty permission set means no data access",
		},
		{
			name: "cluster A grant denies B", perms: eligibilityClusterAPerms,
			node: "node-b", cluster: eligibilityClusterB, want: 0,
			ruleOut: "positive control: an A-only grant already hid B pre-fix — must stay hidden",
		},
		{
			name: "cluster A grant sees own cluster (inheritance)", perms: eligibilityClusterAPerms,
			node: "node-a", cluster: testutil.TestClusterUUID, want: 1,
			ruleOut: "positive control: cluster grant with no node grant still sees its own cluster's nodes",
		},
		{
			name: "global wildcard sees B", perms: func() map[string][]string { return map[string][]string{"*": {}} },
			node: "node-b", cluster: eligibilityClusterB, want: 1,
			ruleOut: "positive control: the global wildcard stays unrestricted",
		},
		{
			name: "explicit empty cluster list denied", perms: func() map[string][]string {
				return map[string][]string{"openshift.cluster": {}}
			},
			node: "node-b", cluster: eligibilityClusterB, want: 0,
			ruleOut: "an explicitly EMPTY grant scopes to nothing (Koku: empty is no access, never unrestricted)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count := getNodeHourlyCount(t, nodeHourlyAppPerms(tc.perms()), tc.node, tc.cluster)
			assert.Equal(t, tc.want, count, tc.ruleOut)
		})
	}
}

// TestRBAC_Eligibility_FleetSummary: settings-only roles must get an empty
// fleet summary, not org-wide aggregates (pre-fix: unrestricted).
func TestRBAC_Eligibility_FleetSummary(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	pool := database.GetPool()
	seedEligibilityClusters(t, ctx)
	for _, cluster := range []string{testutil.TestClusterUUID, eligibilityClusterB} {
		_, err := pool.Exec(ctx, `INSERT INTO recommendation_sets (
				org_id, cluster_uuid, namespace, workload, workload_type, container_name,
				term, engine, stale, notification_codes, idle_state, estimated_savings_cents, updated_at)
			VALUES ($1, $2, 'ns-el', 'wl-el', 'Deployment', 'ctr-el', 'medium', 'cost', false, '{}', 'idle', 500, now())`,
			testutil.TestOrgID, cluster)
		require.NoError(t, err)
	}

	getSummary := func(perms map[string][]string) api.FleetSummaryResponse {
		app := permsApp(perms, func(v1 *echo.Group) {
			v1.GET("/recommendations/openshift/fleet-summary", api.GetFleetSummary)
		})
		req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/fleet-summary", nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var resp api.FleetSummaryResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp
	}

	settings := getSummary(eligibilitySettingsReadPerms())
	assert.Equal(t, 0, settings.ClusterCount, "settings-only must get an empty summary (pre-fix: org-wide aggregates)")
	assert.Equal(t, 0, settings.IdleContainers, "settings-only must get zero idle containers")

	clusterA := getSummary(eligibilityClusterAPerms())
	assert.Equal(t, 1, clusterA.ClusterCount, "positive control: A-only grant summarizes its own cluster")

	wildcard := getSummary(map[string][]string{"*": {}})
	assert.Equal(t, 2, wildcard.ClusterCount, "positive control: wildcard summarizes both clusters")
}

// TestRBAC_Eligibility_FleetHeatmap: settings-only roles must get an empty
// heatmap (pre-fix: unrestricted node list).
func TestRBAC_Eligibility_FleetHeatmap(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedHeatmapNodes(t, ctx, "visible-node", "secret-node")

	settings, _ := getHeatmapAs(t, heatmapRBACApp(eligibilitySettingsReadPerms()))
	assert.Equal(t, 0, settings.Meta.Count, "settings-only must get an empty heatmap (pre-fix: all nodes)")

	wildcard, _ := getHeatmapAs(t, heatmapRBACApp(map[string][]string{"*": {}}))
	assert.Equal(t, 2, wildcard.Meta.Count, "positive control: wildcard sees all nodes")
}

// TestRBAC_Eligibility_CompatContainerList: settings-only roles must get an
// empty compat list (the AddRBACFilter path — pre-fix: no filter applied).
func TestRBAC_Eligibility_CompatContainerList(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	pool := database.GetPool()
	seedEligibilityClusters(t, ctx)
	_, err := pool.Exec(ctx, `INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			term, engine, stale, notification_codes, updated_at, container_id,
			monitoring_start_time, monitoring_end_time)
		VALUES ($1, $2, 'ns-el', 'wl-el', 'Deployment', 'ctr-el',
			'short', 'cost', false, '{}', now(), $3,
			now() - interval '7 days', now() - interval '1 hour')`,
		testutil.TestOrgID, testutil.TestClusterUUID,
		pgrec.NativeContainerID(testutil.TestClusterUUID, "ns-el", "wl-el", "Deployment", "ctr-el"))
	require.NoError(t, err)

	getCount := func(perms map[string][]string) int {
		app := permsApp(perms, func(v1 *echo.Group) {
			v1.GET("/recommendations/openshift/container", api.GetRecommendationSetList)
		})
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/container?filter[limit]=10", nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var resp struct {
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp.Meta.Count
	}

	assert.Equal(t, 0, getCount(eligibilitySettingsReadPerms()),
		"settings-only must get an empty compat list (pre-fix: unfiltered rows)")
	assert.Equal(t, 1, getCount(map[string][]string{"*": {}}),
		"positive control: wildcard sees the seeded row")
}

// TestRBAC_Eligibility_GpuMigGroupedAndUngrouped (#753 review Critical 1):
// the GPU MIG handler's engine treats a zero-length cluster slice as NO
// filter — an empty allowed-cluster set must return empty for BOTH the
// ungrouped list and the grouped views (pre-fix: org-wide groups and totals).
func TestRBAC_Eligibility_GpuMigGroupedAndUngrouped(t *testing.T) {
	setupEligibilityTest(t)
	pool := database.GetPool()
	seedMIGRecommendationWorkloads(t, pool, testutil.TestClusterUUID, []struct {
		ns, wl, cn, node string
	}{{"ns-mig", "wl-mig", "cn-mig", "node-mig"}})

	// Ungrouped: settings-only must get an empty list (pre-fix: full org list).
	ungrouped := migListGET(t, setupGPUMIGEchoWithRBAC(t, eligibilitySettingsReadPerms()), "")
	assert.Equal(t, 0, ungrouped.Meta.Count,
		"settings-only must get an empty GPU MIG list (pre-fix: empty cluster slice meant no filter)")

	// Grouped by cluster: settings-only must get empty groups (pre-fix: all
	// cluster groups with org totals).
	app := setupGPUMIGEchoWithRBAC(t, eligibilitySettingsReadPerms())
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/gpu/mig?group_by[cluster]=*", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var grouped struct {
		Data []map[string]interface{} `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &grouped))
	assert.Equal(t, 0, grouped.Meta.Count,
		"settings-only must get empty GPU MIG groups (pre-fix: org-wide groups)")

	// Positive control: wildcard sees the seeded workload.
	wildcard := migListGET(t, setupGPUMIGEchoWithRBAC(t, map[string][]string{"*": {}}), "")
	assert.Greater(t, wildcard.Meta.Count, 0, "positive control: wildcard sees GPU MIG rows")
}

// TestRBAC_Eligibility_SnapshotAndVmRoutes (#753 review Critical 3): the
// snapshot cost-by-type, age-distribution and cluster instance-type routes
// previously bypassed every RBAC data helper — org-scoping only.
func TestRBAC_Eligibility_SnapshotAndVmRoutes(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	pool := database.GetPool()
	seedEligibilityClusters(t, ctx)
	for _, cluster := range []string{testutil.TestClusterUUID, eligibilityClusterB} {
		_, err := pool.Exec(ctx, `INSERT INTO snapshot_recommendation_sets
			(org_id, cluster_uuid, namespace, snapshot_name, recommendation_type, estimated_cost_cents, age_days, creation_timestamp)
			VALUES ($1, $2, 'ns-el', 'snap-el', 'delete', 5000, 10, now())`,
			testutil.TestOrgID, cluster)
		require.NoError(t, err)
	}

	getBody := func(t *testing.T, perms map[string][]string, path string) ([]byte, int) {
		t.Helper()
		app := permsApp(perms, func(v1 *echo.Group) {
			v1.GET("/recommendations/openshift/snapshots/cost-by-type", api.GetSnapshotCostByType)
			v1.GET("/recommendations/openshift/snapshots/age-distribution", api.GetSnapshotAgeDistribution)
			v1.GET("/recommendations/openshift/instance-types", api.GetClusterInstanceTypes)
		})
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec.Body.Bytes(), rec.Code
	}

	// Cost-by-type: settings-only → empty; A-only → A's rows only; wildcard → both clusters'.
	body, code := getBody(t, eligibilitySettingsReadPerms(), "/api/cost-management/v1/recommendations/openshift/snapshots/cost-by-type")
	require.Equal(t, http.StatusOK, code, "body: %s", body)
	var costResp struct {
		Data []struct {
			Count int `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &costResp))
	assert.Empty(t, costResp.Data, "settings-only must get no snapshot cost data (pre-fix: org-wide costs)")

	body, _ = getBody(t, eligibilityClusterAPerms(), "/api/cost-management/v1/recommendations/openshift/snapshots/cost-by-type")
	require.NoError(t, json.Unmarshal(body, &costResp))
	require.Len(t, costResp.Data, 1, "A-only sees one recommendation type from cluster A only")
	assert.Equal(t, 1, costResp.Data[0].Count, "A-only must not aggregate cluster B's rows (pre-fix: both)")

	body, _ = getBody(t, map[string][]string{"*": {}}, "/api/cost-management/v1/recommendations/openshift/snapshots/cost-by-type")
	require.NoError(t, json.Unmarshal(body, &costResp))
	require.Len(t, costResp.Data, 1)
	assert.Equal(t, 2, costResp.Data[0].Count, "wildcard aggregates both clusters")

	// Age-distribution: settings-only → total 0; A-only → only A's snapshot.
	body, code = getBody(t, eligibilitySettingsReadPerms(), "/api/cost-management/v1/recommendations/openshift/snapshots/age-distribution")
	require.Equal(t, http.StatusOK, code, "body: %s", body)
	var ageResp struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal(body, &ageResp))
	assert.Equal(t, 0, ageResp.Total, "settings-only must get a zero total (pre-fix: org-wide histogram)")

	body, _ = getBody(t, eligibilityClusterAPerms(), "/api/cost-management/v1/recommendations/openshift/snapshots/age-distribution")
	require.NoError(t, json.Unmarshal(body, &ageResp))
	assert.Equal(t, 1, ageResp.Total, "A-only counts only cluster A's snapshot")

	// Instance types: settings-only → empty items for any requested cluster.
	body, code = getBody(t, eligibilitySettingsReadPerms(),
		"/api/cost-management/v1/recommendations/openshift/instance-types?cluster_uuid="+eligibilityClusterB)
	require.Equal(t, http.StatusOK, code, "body: %s", body)
	var vmResp struct {
		InstanceTypes []map[string]interface{} `json:"instance_types"`
	}
	require.NoError(t, json.Unmarshal(body, &vmResp))
	assert.Empty(t, vmResp.InstanceTypes, "settings-only must get no instance types (pre-fix: unscoped)")
}

// TestRBAC_Eligibility_SavingsSummaryShapes (#753 review Important 4): the
// settings-only empty path must preserve the requested response shape —
// grouped requests get their grouped shape, CSV gets CSV.
func TestRBAC_Eligibility_SavingsSummaryShapes(t *testing.T) {
	setupEligibilityTest(t)

	app := permsApp(eligibilitySettingsReadPerms(), func(v1 *echo.Group) {
		v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)
	})

	// Grouped by idle_state: must return the idle-state shape, not the default.
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/savings-summary?group_by[idle_state]=*", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var grouped struct {
		Data []map[string]interface{} `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &grouped))
	assert.Empty(t, grouped.Data, "grouped savings request must get the grouped empty shape")
	assert.Equal(t, 0, grouped.Meta.Count)

	// CSV: must stream CSV, not JSON.
	req = httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/savings-summary?format=csv", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv",
		"CSV savings request must get a CSV response on the empty path")
}

// TestRBAC_Eligibility_EmptyGrantAtCompatList (#753 review Critical 2): an
// explicitly EMPTY node grant must not authorize the compat list — the
// container/namespace builders do not filter on node grants, so key presence
// alone was unrestricted (pre-fix).
func TestRBAC_Eligibility_EmptyGrantAtCompatList(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	pool := database.GetPool()
	seedEligibilityClusters(t, ctx)
	_, err := pool.Exec(ctx, `INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			term, engine, stale, notification_codes, updated_at, container_id,
			monitoring_start_time, monitoring_end_time)
		VALUES ($1, $2, 'ns-el', 'wl-el', 'Deployment', 'ctr-el',
			'short', 'cost', false, '{}', now(), $3,
			now() - interval '7 days', now() - interval '1 hour')`,
		testutil.TestOrgID, testutil.TestClusterUUID,
		pgrec.NativeContainerID(testutil.TestClusterUUID, "ns-el", "wl-el", "Deployment", "ctr-el"))
	require.NoError(t, err)

	app := permsApp(map[string][]string{"openshift.node": {}}, func(v1 *echo.Group) {
		v1.GET("/recommendations/openshift/container", api.GetRecommendationSetList)
	})
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/container?filter[limit]=10", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count,
		"an explicitly empty node grant must not authorize container reads (pre-fix: key presence granted eligibility)")
}

// TestRBAC_Eligibility_RealMiddlewareChain (#753 review Important 5): the
// deny and the settings positive controls through the REAL Rbac middleware
// with a mocked RBAC service — exercising get_user_permissions_from_rbac and
// aggregate_permissions (the settings-ACL parsing), not injected maps.
func TestRBAC_Eligibility_RealMiddlewareChain(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedEligibilityClusters(t, ctx)

	newRealChainApp := func(aclPermission string) *echo.Echo {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data": [{"permission": "` + aclPermission + `", "resourceDefinitions": []}]}`))
		}))
		t.Cleanup(srv.Close)
		u, err := url.Parse(srv.URL)
		require.NoError(t, err)
		t.Setenv("RBACPROTOCOL", "http")
		t.Setenv("RBACHOST", u.Hostname())
		t.Setenv("RBACPORT", u.Port())
		t.Setenv("ROS_RBAC_CACHE_TTL", "0")
		config.ResetForTest()
		t.Cleanup(config.ResetForTest) // rebuild config from restored env — no RBAC/host leak into later tests

		app := echo.New()
		v1 := app.Group("/api/cost-management/v1")
		v1.Use(ros_middleware.Identity)
		v1.Use(ros_middleware.Rbac)
		v1.GET("/recommendations/openshift/node/:id/hourly-utilization", api.GetNodeHourlyUtilization)
		v1.GET("/recommendations/openshift/settings/terms", api.GetTermSettings)
		v1.PUT("/recommendations/openshift/settings/thresholds", api.PutThresholdSettings)
		return app
	}
	identity := makeIdentityHeader(testutil.TestOrgID)

	// Settings-read ACL: data denied through the full real chain.
	app := newRealChainApp("cost-management:settings:read")
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/cost-management/v1/recommendations/openshift/node/node-b/hourly-utilization?cluster_uuid=%s", eligibilityClusterB), nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count,
		"a settings-read ACL through the real middleware must yield no telemetry (aggregate_permissions → eligibility)")

	// Same ACL: settings route still usable through the same real chain.
	req = httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code,
		"settings-read ACL must keep settings-route access through the real chain")

	// Settings-write ACL: a legitimate settings WRITE succeeds (positive
	// control against over-denial) — and would still be denied on data.
	engine.SetThresholdRecalcHookForTest(func(string, string) {})
	defer engine.ClearThresholdRecalcHookForTest()
	writeApp := newRealChainApp("cost-management:settings:write")
	req = httptest.NewRequest(http.MethodPut,
		"/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=container",
		bytes.NewReader([]byte(`{"cpu_cost_percentile": 0.72}`)))
	req.Header.Set("X-Rh-Identity", identity)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	writeApp.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code,
		"settings-write ACL must allow settings writes through the real chain (body: %s)", rec.Body.String())

	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/cost-management/v1/recommendations/openshift/node/node-b/hourly-utilization?cluster_uuid=%s", eligibilityClusterB), nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec = httptest.NewRecorder()
	writeApp.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count,
		"settings-write ACL through the real middleware must equally yield no telemetry")
}

// TestRBAC_Eligibility_SettingsRoutePositive: settings-only identities must
// still be able to use settings routes (guard against over-denial).
func TestRBAC_Eligibility_SettingsRoutePositive(t *testing.T) {
	setupEligibilityTest(t)
	app := permsApp(eligibilitySettingsReadPerms(), func(v1 *echo.Group) {
		v1.GET("/recommendations/openshift/settings/terms", api.GetTermSettings)
	})
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=container", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code,
		"settings-only identities must keep settings-route access (body: %s)", rec.Body.String())
}
