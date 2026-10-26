package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugins/hcp"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// W4 fleet-capacity API tests (#392). Fixtures seed hcp_capacity_history
// directly (the ingestion contract is pinned in internal/ingestion); these
// tests pin the serving contract: authorization, isolation, serve-time
// headroom/status/staleness derivation, and route precedence.

const (
	hcpCapClusterB = "22222222-2222-2222-2222-222222222222"
	hcpCapClusterC = "33333333-3333-3333-3333-333333333333"
	hcpCapClusterD = "44444444-4444-4444-4444-444444444444"
	hcpCapClusterE = "55555555-5555-5555-5555-555555555555"
	hcpCapClusterF = "66666666-6666-6666-6666-666666666666"
	hcpCapClusterG = "77777777-7777-7777-7777-777777777777"
)

func seedHCPCapCluster(t *testing.T, orgID, clusterUUID string) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		hcpCapAccountID(orgID), orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2, 'hcp-cap-test', 'hcp-cap', NOW()) ON CONFLICT DO NOTHING`,
		hcpCapAccountID(orgID), clusterUUID,
	)
	require.NoError(t, err)
}

func hcpCapAccountID(orgID string) int {
	// Deterministic distinct account id per org (cluster_uuid is not
	// globally unique; tenant_id distinguishes the same UUID across orgs).
	n := 0
	for _, r := range orgID {
		n = (n*31 + int(r)) % 100000
	}
	return 90000 + n
}

// seedHCPCapRow inserts the healthy Task 2 fixture (64 vCPU / 128 GiB /
// 1500 pods, 4 HCs, 2 ready, 300 QPS): stored scenarios request/low/average
// 7 memory_request, medium 5 cpu_usage, high 3 cpu_usage; MCE refs 8/NULL/
// 8/4/2. Tests override specific columns with UPDATEs.
func seedHCPCapRow(t *testing.T, orgID, clusterUUID, observedAt string) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `
		INSERT INTO hcp_capacity_history (
			observed_at, org_id, cluster_uuid, mce_sampled_at,
			operator_version, ocp_version, mce_version, native_model_version,
			collection_interval_minutes, upload_cycle_minutes,
			inventory_complete, current_hc_count, ready_hcp_count,
			average_ready_hcp_qps, ready_hcp_qps_known, hosted_availability_basis,
			eligible_worker_cpu_allocatable_millicores, eligible_worker_memory_allocatable_bytes,
			eligible_worker_max_pods, worker_placement_known, pressure_state, shared_infrastructure,
			mce_request_max_hcs, mce_low_qps_max_hcs, mce_average_qps_max_hcs,
			mce_medium_qps_max_hcs, mce_high_qps_max_hcs,
			request_max_hcs, request_limiting_resource,
			low_qps_max_hcs, low_qps_limiting_resource,
			average_qps_max_hcs, average_qps_limiting_resource,
			medium_qps_max_hcs, medium_qps_limiting_resource,
			high_qps_max_hcs, high_qps_limiting_resource
		) VALUES ($1, $2, $3, NULL,
			'deadbeef', '4.22.17', '', 'hcp-published-sizing-v1',
			60, 360,
			true, 4, 2,
			300, true, 'highly_available',
			64000, 137438953472, 1500, true, 'unknown', false,
			8, NULL, 8, 4, 2,
			7, 'memory_request', 7, 'memory_request', 7, 'memory_request',
			5, 'cpu_usage', 3, 'cpu_usage')`,
		observedAt, orgID, clusterUUID)
	require.NoError(t, err)
}

func hcpCapUpdate(t *testing.T, orgID, clusterUUID, observedAt, setSQL string, args ...interface{}) {
	t.Helper()
	_, err := database.GetPool().Exec(context.Background(),
		`UPDATE hcp_capacity_history SET `+setSQL+` WHERE org_id = $1 AND cluster_uuid = $2 AND observed_at = $3`,
		append([]interface{}{orgID, clusterUUID, observedAt}, args...)...)
	require.NoError(t, err)
}

func setupHCPCapEcho(t *testing.T, perms map[string][]string) *echo.Echo {
	t.Helper()
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	if perms != nil {
		cfg := config.GetConfig()
		orig := cfg.RBACEnabled
		cfg.RBACEnabled = true
		t.Cleanup(func() { cfg.RBACEnabled = orig })
		v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				c.Set("user.permissions", perms)
				return next(c)
			}
		})
	}
	// The real plugin registration: fleet-capacity routes must win over
	// :recommendation-id exactly as production serves them.
	p := &hcp.HCPPlugin{}
	p.RegisterRoutes(v1)
	return e
}

func hcpCapGET(t *testing.T, e *echo.Echo, orgID, path string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var body map[string]interface{}
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body: %s", rec.Body.String())
	}
	return rec.Code, body
}

func hcpCapData(t *testing.T, body map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, ok := body["data"].([]interface{})
	require.True(t, ok, "data must be a list: %v", body)
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.(map[string]interface{}))
	}
	return out
}

func hcpCapScenarios(t *testing.T, row map[string]interface{}) map[string]map[string]interface{} {
	t.Helper()
	raw, ok := row["scenarios"].([]interface{})
	require.True(t, ok, "scenarios must be a list: %v", row)
	out := make(map[string]map[string]interface{}, len(raw))
	for _, item := range raw {
		s := item.(map[string]interface{})
		out[s["name"].(string)] = s
	}
	return out
}

func hcpCapCleanup(t *testing.T, orgIDs ...string) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	for _, orgID := range orgIDs {
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_capacity_history WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM clusters WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM rh_accounts WHERE org_id = $1`, orgID)
	}
}

const hcpCapBase = "/api/cost-management/v1/recommendations/openshift/hcp/fleet-capacity"

func TestHCPCapacityLatest_FilterSpellingsAndLatest(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	orgID := "org-hcp-cap-latest"
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	hcpCapCleanup(t, orgID)
	t.Cleanup(func() { hcpCapCleanup(t, orgID) })

	seedHCPCapCluster(t, orgID, testutil.TestClusterUUID)
	seedHCPCapRow(t, orgID, testutil.TestClusterUUID, "2026-10-25T13:59:59Z")
	seedHCPCapRow(t, orgID, testutil.TestClusterUUID, "2026-10-25T14:59:59Z")

	e := setupHCPCapEcho(t, nil)
	for _, q := range []string{
		"?filter[cluster_uuid]=" + testutil.TestClusterUUID,
		"?filter[cluster]=" + testutil.TestClusterUUID,
		"?cluster_uuid=" + testutil.TestClusterUUID,
	} {
		code, body := hcpCapGET(t, e, orgID, hcpCapBase+q)
		require.Equal(t, http.StatusOK, code, "query %s", q)
		rows := hcpCapData(t, body)
		require.Len(t, rows, 1, "query %s: %v", q, body)
		assert.Equal(t, "2026-10-25T14:59:59Z", rows[0]["observed_at"], "latest by observed_at")
		assert.Equal(t, "hcp-published-sizing-v1", rows[0]["native_model_version"])
	}
}

func TestHCPCapacityLatest_UnfilteredOneRowPerCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	orgID := "org-hcp-cap-fanout"
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	hcpCapCleanup(t, orgID)
	t.Cleanup(func() { hcpCapCleanup(t, orgID) })

	// Same observed_at on both clusters: the collision must not fan out or
	// collapse — exactly one row per authorized cluster.
	seedHCPCapCluster(t, orgID, testutil.TestClusterUUID)
	seedHCPCapCluster(t, orgID, hcpCapClusterB)
	seedHCPCapRow(t, orgID, testutil.TestClusterUUID, "2026-10-25T14:59:59Z")
	seedHCPCapRow(t, orgID, hcpCapClusterB, "2026-10-25T14:59:59Z")

	e := setupHCPCapEcho(t, nil)
	code, body := hcpCapGET(t, e, orgID, hcpCapBase)
	require.Equal(t, http.StatusOK, code)
	rows := hcpCapData(t, body)
	require.Len(t, rows, 2, "one row per cluster: %v", body)
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row["cluster_uuid"].(string)] = true
	}
	assert.True(t, seen[testutil.TestClusterUUID] && seen[hcpCapClusterB])
}

func TestHCPCapacityLatest_RBACNonDisclosing(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	orgID := "org-hcp-cap-rbac"
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	hcpCapCleanup(t, orgID)
	t.Cleanup(func() { hcpCapCleanup(t, orgID) })

	seedHCPCapCluster(t, orgID, testutil.TestClusterUUID)
	seedHCPCapCluster(t, orgID, hcpCapClusterB)
	seedHCPCapRow(t, orgID, testutil.TestClusterUUID, "2026-10-25T14:59:59Z")
	seedHCPCapRow(t, orgID, hcpCapClusterB, "2026-10-25T14:59:59Z")

	// Permission for B only. The unauthorized cluster must not disclose
	// existence: 200-empty, not 403/404.
	e := setupHCPCapEcho(t, map[string][]string{"openshift.cluster": {hcpCapClusterB}})
	code, body := hcpCapGET(t, e, orgID, hcpCapBase+"?filter[cluster_uuid]="+testutil.TestClusterUUID)
	require.Equal(t, http.StatusOK, code, "unauthorized must be non-disclosing: %s", body)
	assert.Empty(t, hcpCapData(t, body))

	code, body = hcpCapGET(t, e, orgID, hcpCapBase)
	require.Equal(t, http.StatusOK, code)
	rows := hcpCapData(t, body)
	require.Len(t, rows, 1)
	assert.Equal(t, hcpCapClusterB, rows[0]["cluster_uuid"], "unfiltered list shows only authorized clusters")

	code, body = hcpCapGET(t, e, orgID, hcpCapBase+"?filter[cluster_uuid]="+hcpCapClusterB)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, hcpCapData(t, body), 1)
}

func TestHCPCapacityLatest_CrossOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	orgA := "org-hcp-cap-iso-a"
	orgB := "org-hcp-cap-iso-b"
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	hcpCapCleanup(t, orgA, orgB)
	t.Cleanup(func() { hcpCapCleanup(t, orgA, orgB) })

	// The same cluster UUID registered under both orgs (cluster_uuid is not
	// globally unique) with distinct capacity rows: each org must read only
	// its own row — org_id is part of every capacity query.
	seedHCPCapCluster(t, orgA, testutil.TestClusterUUID)
	seedHCPCapCluster(t, orgB, testutil.TestClusterUUID)
	seedHCPCapRow(t, orgA, testutil.TestClusterUUID, "2026-10-25T10:59:59Z")
	seedHCPCapRow(t, orgB, testutil.TestClusterUUID, "2026-10-25T16:59:59Z")

	e := setupHCPCapEcho(t, nil)
	code, body := hcpCapGET(t, e, orgA, hcpCapBase+"?filter[cluster_uuid]="+testutil.TestClusterUUID)
	require.Equal(t, http.StatusOK, code)
	rows := hcpCapData(t, body)
	require.Len(t, rows, 1)
	assert.Equal(t, "2026-10-25T10:59:59Z", rows[0]["observed_at"], "org A must not read org B's row")

	code, body = hcpCapGET(t, e, orgB, hcpCapBase+"?filter[cluster_uuid]="+testutil.TestClusterUUID)
	require.Equal(t, http.StatusOK, code)
	rows = hcpCapData(t, body)
	require.Len(t, rows, 1)
	assert.Equal(t, "2026-10-25T16:59:59Z", rows[0]["observed_at"], "org B must not read org A's row")
}

func TestHCPCapacityHistory_RequiresClusterAndOrders(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	orgID := "org-hcp-cap-hist"
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	hcpCapCleanup(t, orgID)
	t.Cleanup(func() { hcpCapCleanup(t, orgID) })

	seedHCPCapCluster(t, orgID, testutil.TestClusterUUID)
	seedHCPCapRow(t, orgID, testutil.TestClusterUUID, "2026-10-25T12:59:59Z")
	seedHCPCapRow(t, orgID, testutil.TestClusterUUID, "2026-10-25T13:59:59Z")
	seedHCPCapRow(t, orgID, testutil.TestClusterUUID, "2026-10-25T14:59:59Z")

	e := setupHCPCapEcho(t, nil)

	// History is a per-cluster trend: the single-cluster filter is required.
	code, _ := hcpCapGET(t, e, orgID, hcpCapBase+"/history")
	assert.Equal(t, http.StatusBadRequest, code, "history without cluster filter must 400")
	code, _ = hcpCapGET(t, e, orgID, hcpCapBase+"/history?filter[cluster_uuid]=not-a-uuid")
	assert.Equal(t, http.StatusBadRequest, code, "invalid UUID must 400")

	code, body := hcpCapGET(t, e, orgID,
		hcpCapBase+"/history?filter[cluster_uuid]="+testutil.TestClusterUUID+"&limit=2&offset=0")
	require.Equal(t, http.StatusOK, code)
	rows := hcpCapData(t, body)
	require.Len(t, rows, 2, "limit respected")
	meta := body["meta"].(map[string]interface{})
	assert.Equal(t, float64(3), meta["count"], "meta.count is the total, not the page size")
	assert.Equal(t, "2026-10-25T14:59:59Z", rows[0]["observed_at"], "history is newest-first")
	assert.Equal(t, "2026-10-25T13:59:59Z", rows[1]["observed_at"])

	// filter[cluster] spelling works on history too.
	code, body = hcpCapGET(t, e, orgID,
		hcpCapBase+"/history?filter[cluster]="+testutil.TestClusterUUID+"&limit=10&offset=0")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, hcpCapData(t, body), 3)
}

func TestHCPCapacityHistory_RBACAndCrossOrgNonDisclosing(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	orgID := "org-hcp-cap-hist-rbac"
	orgOther := "org-hcp-cap-hist-other"
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	hcpCapCleanup(t, orgID, orgOther)
	t.Cleanup(func() { hcpCapCleanup(t, orgID, orgOther) })

	seedHCPCapCluster(t, orgID, testutil.TestClusterUUID)
	seedHCPCapRow(t, orgID, testutil.TestClusterUUID, "2026-10-25T14:59:59Z")
	// Same UUID in another org with its own row.
	seedHCPCapCluster(t, orgOther, testutil.TestClusterUUID)
	seedHCPCapRow(t, orgOther, testutil.TestClusterUUID, "2026-10-25T15:59:59Z")

	// RBAC denies the only cluster: non-disclosing 200-empty.
	e := setupHCPCapEcho(t, map[string][]string{"openshift.cluster": {hcpCapClusterB}})
	code, body := hcpCapGET(t, e, orgID,
		hcpCapBase+"/history?filter[cluster_uuid]="+testutil.TestClusterUUID)
	require.Equal(t, http.StatusOK, code, "unauthorized history must be non-disclosing: %s", body)
	assert.Empty(t, hcpCapData(t, body))
	assert.Equal(t, float64(0), body["meta"].(map[string]interface{})["count"])

	// Authorized org reads only its own rows.
	e2 := setupHCPCapEcho(t, nil)
	code, body = hcpCapGET(t, e2, orgID,
		hcpCapBase+"/history?filter[cluster_uuid]="+testutil.TestClusterUUID)
	require.Equal(t, http.StatusOK, code)
	rows := hcpCapData(t, body)
	require.Len(t, rows, 1)
	assert.Equal(t, "2026-10-25T14:59:59Z", rows[0]["observed_at"], "org must not read another org's history")
}

func TestHCPCapacityRow_ServeTimeSemantics(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	orgID := "org-hcp-cap-semantics"
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	hcpCapCleanup(t, orgID)
	t.Cleanup(func() { hcpCapCleanup(t, orgID) })

	now := time.Now().UTC()
	freshAt := now.Add(-30 * time.Minute).Format(time.RFC3339)
	staleAt := now.Add(-13 * time.Hour).Format(time.RFC3339) // 2×max(60,360)=12h window
	pressuredAt := now.Add(-31 * time.Minute).Format(time.RFC3339)
	unknownQPSAt := now.Add(-32 * time.Minute).Format(time.RFC3339)
	belowFloorAt := now.Add(-33 * time.Minute).Format(time.RFC3339)
	illustrativeAt := now.Add(-34 * time.Minute).Format(time.RFC3339)
	compactAt := now.Add(-35 * time.Minute).Format(time.RFC3339)

	// One cluster per variant: the latest endpoint always serves the
	// newest row of the (filtered) cluster, so variants must not share one.
	for _, clusterUUID := range []string{testutil.TestClusterUUID, hcpCapClusterB, hcpCapClusterC, hcpCapClusterD, hcpCapClusterE, hcpCapClusterF, hcpCapClusterG} {
		seedHCPCapCluster(t, orgID, clusterUUID)
	}
	seedHCPCapRow(t, orgID, testutil.TestClusterUUID, freshAt)
	seedHCPCapRow(t, orgID, hcpCapClusterB, staleAt)
	seedHCPCapRow(t, orgID, hcpCapClusterC, pressuredAt)
	hcpCapUpdate(t, orgID, hcpCapClusterC, pressuredAt, "pressure_state = 'pressured'")
	seedHCPCapRow(t, orgID, hcpCapClusterD, unknownQPSAt)
	hcpCapUpdate(t, orgID, hcpCapClusterD, unknownQPSAt,
		"ready_hcp_qps_known = false, average_ready_hcp_qps = NULL, average_qps_max_hcs = 0, average_qps_limiting_resource = ''")
	seedHCPCapRow(t, orgID, hcpCapClusterE, belowFloorAt)
	hcpCapUpdate(t, orgID, hcpCapClusterE, belowFloorAt, "ocp_version = '4.16.9'")
	seedHCPCapRow(t, orgID, hcpCapClusterF, illustrativeAt)
	hcpCapUpdate(t, orgID, hcpCapClusterF, illustrativeAt, "hosted_availability_basis = 'mixed'")
	seedHCPCapRow(t, orgID, hcpCapClusterG, compactAt)
	hcpCapUpdate(t, orgID, hcpCapClusterG, compactAt, "shared_infrastructure = true")

	e := setupHCPCapEcho(t, nil)
	get := func(clusterUUID string) map[string]interface{} {
		code, body := hcpCapGET(t, e, orgID, hcpCapBase+"?filter[cluster_uuid]="+clusterUUID)
		require.Equal(t, http.StatusOK, code)
		rows := hcpCapData(t, body)
		require.Len(t, rows, 1, "latest for cluster %s: %v", clusterUUID, rows)
		return rows[0]
	}
	warnings := func(row map[string]interface{}) map[string]interface{} {
		w, ok := row["warnings"].(map[string]interface{})
		require.True(t, ok, "warnings object: %v", row)
		return w
	}

	// Fresh hourly row under a 6-hour upload cycle must NOT be stale (2×
	// max(cadence) clock from the row's own fields).
	fresh := get(testutil.TestClusterUUID)
	assert.Equal(t, freshAt, fresh["observed_at"])
	assert.False(t, warnings(fresh)["stale"].(bool), "30min-old row with 360min upload cycle is fresh")
	assert.Greater(t, fresh["sample_age_seconds"].(float64), float64(0))
	assert.False(t, warnings(fresh)["below_version_floor"].(bool))
	assert.False(t, warnings(fresh)["pressured"].(bool))
	assert.True(t, warnings(fresh)["pressure_unknown"].(bool), "unknown pressure is explicit, never silently clear")

	// Scenario math: request 7/memory_request headroom 3; high 3/cpu_usage
	// headroom -1 (signed over-capacity).
	scenarios := hcpCapScenarios(t, fresh)
	req := scenarios["request"]
	assert.Equal(t, float64(7), req["max_hcs"])
	assert.Equal(t, "memory_request", req["limiting_resource"])
	assert.Equal(t, float64(3), req["headroom"])
	assert.Equal(t, "ok", req["status"])
	high := scenarios["high_qps"]
	assert.Equal(t, float64(3), high["max_hcs"])
	assert.Equal(t, "cpu_usage", high["limiting_resource"])
	assert.Equal(t, float64(-1), high["headroom"], "over-capacity headroom stays signed")

	// Nullable MCE references stay explicit: absent is null, never zero.
	mceRefs := fresh["mce_references"].(map[string]interface{})
	assert.Equal(t, nil, mceRefs["low_qps"])
	assert.Equal(t, float64(8), mceRefs["request"])

	// Staleness: age 13h > 2×max(60,360)=12h → stale, data still served.
	stale := get(hcpCapClusterB)
	assert.True(t, warnings(stale)["stale"].(bool))

	// Affirmative pressure withholds exact headroom on every scenario.
	pressured := get(hcpCapClusterC)
	assert.True(t, warnings(pressured)["pressured"].(bool))
	for _, s := range hcpCapScenarios(t, pressured) {
		assert.Equal(t, nil, s["headroom"], "pressured rows withhold headroom")
	}

	// Unknown QPS degrades only the average scenario: no estimate (empty
	// limiting resource) and no headroom; others keep theirs.
	unknownQPS := get(hcpCapClusterD)
	avg := hcpCapScenarios(t, unknownQPS)["average_qps"]
	assert.Equal(t, "", avg["limiting_resource"], "no estimate signals unavailability")
	assert.Equal(t, nil, avg["headroom"])
	assert.Equal(t, float64(7), hcpCapScenarios(t, unknownQPS)["request"]["max_hcs"], "other scenarios unaffected")

	// Known below-floor OCP version warns without suppressing data.
	belowFloor := get(hcpCapClusterE)
	assert.True(t, warnings(belowFloor)["below_version_floor"].(bool))
	assert.Equal(t, float64(7), hcpCapScenarios(t, belowFloor)["request"]["max_hcs"], "warning never suppresses scenarios")

	// mixed availability basis → illustrative_only status, math unchanged.
	illustrative := get(hcpCapClusterF)
	illScenarios := hcpCapScenarios(t, illustrative)
	for _, s := range illScenarios {
		assert.Equal(t, "illustrative_only", s["status"])
	}
	assert.Equal(t, float64(7), illScenarios["request"]["max_hcs"], "warning never changes the math")
	assert.Equal(t, float64(3), illScenarios["high_qps"]["max_hcs"], "warning never changes the math")

	// Compact/shared-infrastructure topology → the §3.2.5 caveat warns
	// (ADR-0334); the scenarios still serve — math unchanged, never suppressed.
	compact := get(hcpCapClusterG)
	assert.True(t, warnings(compact)["shared_infrastructure"].(bool), "compact topology carries the shared-infrastructure caveat")
	assert.Equal(t, float64(7), hcpCapScenarios(t, compact)["request"]["max_hcs"], "caveat never changes the math")
	assert.False(t, warnings(fresh)["shared_infrastructure"].(bool), "dedicated-worker clusters do not warn")
}

func TestHCPCapacityLatest_MCEMayBeStale(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	orgID := "org-hcp-cap-mce-stale"
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	hcpCapCleanup(t, orgID)
	t.Cleanup(func() { hcpCapCleanup(t, orgID) })

	now := time.Now().UTC()
	seedHCPCapCluster(t, orgID, testutil.TestClusterUUID)
	seedHCPCapCluster(t, orgID, hcpCapClusterB)

	// Cluster A: MCE references frozen while native inputs move across the
	// freshness window (12h) → may-be-stale caution.
	for i, age := range []time.Duration{5 * time.Hour, 2 * time.Hour, 30 * time.Minute} {
		at := now.Add(-age).Format(time.RFC3339)
		seedHCPCapRow(t, orgID, testutil.TestClusterUUID, at)
		hcpCapUpdate(t, orgID, testutil.TestClusterUUID, at, fmt.Sprintf("eligible_worker_cpu_allocatable_millicores = %d", 64000+i*1000))
	}
	// Cluster B: MCE references move too → no caution.
	for i, age := range []time.Duration{2 * time.Hour, 30 * time.Minute} {
		at := now.Add(-age).Format(time.RFC3339)
		seedHCPCapRow(t, orgID, hcpCapClusterB, at)
		hcpCapUpdate(t, orgID, hcpCapClusterB, at, fmt.Sprintf("mce_request_max_hcs = %d", 8+i))
	}

	e := setupHCPCapEcho(t, nil)
	code, body := hcpCapGET(t, e, orgID, hcpCapBase)
	require.Equal(t, http.StatusOK, code)
	byCluster := map[string]map[string]interface{}{}
	for _, row := range hcpCapData(t, body) {
		byCluster[row["cluster_uuid"].(string)] = row
	}
	require.Len(t, byCluster, 2)
	warnA := byCluster[testutil.TestClusterUUID]["warnings"].(map[string]interface{})
	assert.Equal(t, true, warnA["mce_may_be_stale"], "frozen MCE refs + moving native inputs → caution")
	warnB := byCluster[hcpCapClusterB]["warnings"].(map[string]interface{})
	assert.Equal(t, false, warnB["mce_may_be_stale"], "moving MCE refs → no caution")
}

func TestHCPCapacityRoutes_StaticPathNotRecommendationID(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	orgID := "org-hcp-cap-static"
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	hcpCapCleanup(t, orgID)
	t.Cleanup(func() { hcpCapCleanup(t, orgID) })

	seedHCPCapCluster(t, orgID, testutil.TestClusterUUID)
	seedHCPCapRow(t, orgID, testutil.TestClusterUUID, "2026-10-25T14:59:59Z")

	e := setupHCPCapEcho(t, nil)
	// The static fleet-capacity segment must not fall through to
	// :recommendation-id ("fleet-capacity" is not a UUID).
	code, body := hcpCapGET(t, e, orgID, hcpCapBase)
	require.Equal(t, http.StatusOK, code, "static route wins over :recommendation-id")
	meta := body["meta"].(map[string]interface{})
	assert.Equal(t, float64(1), meta["count"])
}
