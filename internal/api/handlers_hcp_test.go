package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/hcp"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

const (
	hcpTestMC1 = "22222222-2222-2222-2222-222222222222"
	hcpTestMC2 = "33333333-3333-3333-3333-333333333333"
	hcpTestNS1 = "clusters-hc1"
	hcpTestNS2 = "clusters-hc2"
	hcpTestHC1 = "aaa-11111"
	hcpTestHC2 = "bbb-22222"
)

// seedHCPServerFixtures creates two management clusters with HCP namespaces,
// rec rows (associated + incomplete + app rows), and backing snapshots.
// MC1 serves HC1 (associated etcd + incomplete kas); MC2 serves HC2.
func seedHCPServerFixtures(t *testing.T, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	seedCluster := func(uuid, alias string, namespaces []string) {
		require.NoError(t, pgrec.EnsureIngestClusterRow(ctx, pool, orgID, "src-"+alias, uuid, alias, now))
		require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, orgID, "src-"+alias, uuid, namespaces))
	}
	seedRec := func(clusterUUID, namespace, workload, hc, containerID string, savingsCents int64) {
		// Clock discipline: monitoring_end_time must strictly precede the
		// truncated `< now` upper bound the list handlers apply, so back off
		// an hour — seeding exactly now() filters the row back out.
		for _, te := range [][2]string{
			{"short", "cost"}, {"short", "performance"},
			{"medium", "cost"}, {"medium", "performance"},
			{"long", "cost"}, {"long", "performance"},
		} {
			_, err := pool.Exec(ctx, `
				INSERT INTO recommendation_sets (
					org_id, cluster_uuid, namespace, workload, workload_type,
					container_name, term, engine, stale, updated_at, hosted_cluster_id,
					container_id, monitoring_start_time, monitoring_end_time,
					rec_cpu_request_millicores, rec_memory_request_kib,
					estimated_savings_cents
				) VALUES ($1, $2, $3, $4, 'deployment', $4, $7, $8, false, now(), NULLIF($5, ''), $6,
					now() - interval '25 hours', now() - interval '1 hour', 100, 1024, $9)
				ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine)
				DO UPDATE SET hosted_cluster_id = EXCLUDED.hosted_cluster_id,
					monitoring_start_time = EXCLUDED.monitoring_start_time,
					monitoring_end_time = EXCLUDED.monitoring_end_time,
					estimated_savings_cents = EXCLUDED.estimated_savings_cents`,
				orgID, clusterUUID, namespace, workload, hc, containerID, te[0], te[1], savingsCents)
			require.NoError(t, err)
		}
	}

	seedCluster(hcpTestMC1, "mc1", []string{hcpTestNS1})
	seedCluster(hcpTestMC2, "mc2", []string{hcpTestNS2})
	seedRec(hcpTestMC1, hcpTestNS1, "etcd", hcpTestHC1, "44444444-4444-4444-4444-444444444444", 300000)
	seedRec(hcpTestMC1, hcpTestNS1, "kube-apiserver", "", "55555555-5555-5555-5555-555555555555", 0)
	seedRec(hcpTestMC1, "tenant-app", "web", "", "66666666-6666-6666-6666-666666666666", 0)
	seedRec(hcpTestMC2, hcpTestNS2, "etcd", hcpTestHC2, "77777777-7777-7777-7777-777777777777", 150000)

	snap := func(ns, hc string) topology.HCPSnapshotEntry {
		return topology.HCPSnapshotEntry{
			HCPNamespace: ns, HostedClusterID: hc, HcUID: "uid-" + hc,
			ObservedAt: now.Format(time.RFC3339), Complete: true,
		}
	}
	require.NoError(t, pgrec.UpsertHCPSnapshots(ctx, pool, orgID, hcpTestMC1, "m-hcp-1",
		[]topology.HCPSnapshotEntry{snap(hcpTestNS1, hcpTestHC1)}))
	require.NoError(t, pgrec.UpsertHCPSnapshots(ctx, pool, orgID, hcpTestMC2, "m-hcp-2",
		[]topology.HCPSnapshotEntry{snap(hcpTestNS2, hcpTestHC2)}))
}

// hcpTestEcho builds the contract-style app with optional fixed RBAC perms.
// Empty perms + disabled RBAC (test default) means full access.
func hcpTestEcho(perms map[string][]string) *echo.Echo {
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	if perms != nil {
		v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				c.Set("user.permissions", perms)
				return next(c)
			}
		})
	}
	api.RegisterV1RoutesForTest(v1, nil)
	return e
}

func hcpGET(t *testing.T, e *echo.Echo, identityHeader, path string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var body map[string]interface{}
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body: %s", rec.Body.String())
	}
	return rec.Code, body
}

func hcpDataItems(t *testing.T, body map[string]interface{}) []interface{} {
	t.Helper()
	raw, ok := body["data"]
	require.True(t, ok, "response must have data key: %v", body)
	if raw == nil {
		return []interface{}{}
	}
	data, ok := raw.([]interface{})
	require.True(t, ok, "data must be an array: %v", body)
	return data
}

// TestHCPList_ServesOnlyHCPNamespaces proves row-scope: associated rows carry
// the frozen ID, unassociated HCP rows show incomplete:true, app-namespace
// rows never appear — with adversarial same workload names across scopes.
func TestHCPList_ServesOnlyHCPNamespaces(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID + "-hcp-list"
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })
	seedHCPServerFixtures(t, pool, orgID)

	e := hcpTestEcho(nil)
	code, body := hcpGET(t, e, makeIdentityHeader(orgID),
		"/api/cost-management/v1/recommendations/openshift/hcp?limit=50")
	require.Equal(t, http.StatusOK, code, "body: %v", body)

	byWorkload := map[string]map[string]interface{}{}
	for _, item := range hcpDataItems(t, body) {
		m := item.(map[string]interface{})
		byWorkload[m["workload"].(string)] = m
	}
	require.Contains(t, byWorkload, "etcd", "associated HCP row must appear")
	assert.Equal(t, hcpTestHC1, byWorkload["etcd"]["hosted_cluster_id"])
	require.Contains(t, byWorkload, "kube-apiserver", "unassociated HCP row must appear")
	assert.Equal(t, true, byWorkload["kube-apiserver"]["incomplete"], "unassociated HCP rows show incomplete:true")
	assert.NotContains(t, byWorkload["kube-apiserver"], "hosted_cluster_id", "empty IDs must omit the field")
	assert.NotContains(t, byWorkload, "web", "app-namespace rows must never appear on the HCP surface")
}

// TestHCPList_FilterNarrows proves filter[hosted_cluster_id] narrows to one
// HC and unknown IDs return empty (200, not 404).
func TestHCPList_FilterNarrows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID + "-hcp-filter"
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })
	seedHCPServerFixtures(t, pool, orgID)

	e := hcpTestEcho(nil)
	code, body := hcpGET(t, e, makeIdentityHeader(orgID),
		"/api/cost-management/v1/recommendations/openshift/hcp?limit=50&filter[hosted_cluster_id]="+hcpTestHC2)
	require.Equal(t, http.StatusOK, code)
	items := hcpDataItems(t, body)
	require.Len(t, items, 1)
	assert.Equal(t, hcpTestHC2, items[0].(map[string]interface{})["hosted_cluster_id"])

	code, body = hcpGET(t, e, makeIdentityHeader(orgID),
		"/api/cost-management/v1/recommendations/openshift/hcp?limit=50&filter[hosted_cluster_id]=no-such-hc")
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, hcpDataItems(t, body), "unknown HC IDs return empty, never an error")
}

// TestHCPList_TermProjectionSynthesizesVariants proves #651: list rows carry
// all six term/engine variants (classic sibling fan-out), so term/engine
// projection is display-side. Medium-term data must be present even though
// keys collapse on short/cost — pre-fan-out rows carried short/cost only.
func TestHCPList_TermProjectionSynthesizesVariants(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID + "-hcp-projection"
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })
	seedHCPServerFixtures(t, pool, orgID)

	e := hcpTestEcho(nil)
	// Filters tolerated (no 400) and ignored at DB level: same rows back.
	for _, query := range []string{
		"/api/cost-management/v1/recommendations/openshift/hcp?limit=50",
		"/api/cost-management/v1/recommendations/openshift/hcp?limit=50&filter[term]=medium_term&filter[engine]=cost",
	} {
		code, body := hcpGET(t, e, makeIdentityHeader(orgID), query)
		require.Equal(t, http.StatusOK, code, "body: %v", body)
		items := hcpDataItems(t, body)
		require.Len(t, items, 3, "associated + incomplete rows regardless of projection")
		for _, item := range items {
			m := item.(map[string]interface{})
			recs, ok := m["recommendations"].(map[string]interface{})
			require.True(t, ok, "row must carry recommendations: %v", m)
			terms, ok := recs["recommendation_terms"].(map[string]interface{})
			require.True(t, ok, "row must carry recommendation_terms: %v", recs)
			medium, ok := terms["medium_term"].(map[string]interface{})
			require.True(t, ok, "sibling fan-out must synthesize medium_term: %v", terms)
			engines, ok := medium["recommendation_engines"].(map[string]interface{})
			require.True(t, ok)
			cost, ok := engines["cost"].(map[string]interface{})
			require.True(t, ok, "medium cost engine must be present: %v", engines)
			_, hasConfig := cost["config"]
			assert.True(t, hasConfig, "medium cost engine must have config: %v", cost)
		}
	}
}

// TestHCPList_CSVExpandsVariants proves the fan-out reaches CSV: one data row
// per term/engine variant (3 containers x 6 variants), mirroring classic.
func TestHCPList_CSVExpandsVariants(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID + "-hcp-projection-csv"
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })
	seedHCPServerFixtures(t, pool, orgID)

	e := hcpTestEcho(nil)
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/hcp?limit=50&format=csv", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	req.Header.Set("Accept", "text/csv")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	require.Greater(t, len(lines), 4, "CSV must expand variants, not stream pinned rows only")
	assert.Equal(t, 19, len(lines), "header + 3 containers x 6 variants")
}

// TestHCPList_GroupByCounts proves the grouped rollup: per-HC counts over
// associated rows only (unassociated + app rows excluded), plus summed
// savings rendered as MoneyAmount in display currency.
func TestHCPList_GroupByCounts(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID + "-hcp-group"
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })
	seedHCPServerFixtures(t, pool, orgID)

	e := hcpTestEcho(nil)
	code, body := hcpGET(t, e, makeIdentityHeader(orgID),
		"/api/cost-management/v1/recommendations/openshift/hcp?group_by[hosted_cluster_id]=*")
	require.Equal(t, http.StatusOK, code, "body: %v", body)
	items := hcpDataItems(t, body)
	require.Len(t, items, 2, "exactly the two associated HCs; nothing unassociated")
	byHC := map[string]map[string]interface{}{}
	for _, item := range items {
		m := item.(map[string]interface{})
		byHC[m["hosted_cluster_id"].(string)] = m
	}
	require.Contains(t, byHC, hcpTestHC1)
	require.Contains(t, byHC, hcpTestHC2)
	assert.Equal(t, float64(1), byHC[hcpTestHC1]["count"])
	assert.Equal(t, float64(1), byHC[hcpTestHC2]["count"])
	// Seeded pinned cents: HC1 etcd 300000 ($3000.00), HC2 etcd 150000 ($1500.00).
	sav1 := byHC[hcpTestHC1]["estimated_savings"].(map[string]interface{})
	assert.Equal(t, "3000.00", sav1["value"])
	assert.Equal(t, "USD", sav1["units"])
	sav2 := byHC[hcpTestHC2]["estimated_savings"].(map[string]interface{})
	assert.Equal(t, "1500.00", sav2["value"])
	assert.Equal(t, "USD", sav2["units"])
	meta := body["meta"].(map[string]interface{})
	assert.Equal(t, "USD", meta["currency"])
}

// TestHCPList_GroupBySavingsSumsPinnedRows proves the rollup is a SUM over
// pinned (short/cost) rows: a second HC1 container at 100000 cents joins the
// first at 300000 for $4000.00 — not MAX ($3000.00), not all-variant rows
// ($24000.00 had the query forgotten the pinned filter).
func TestHCPList_GroupBySavingsSumsPinnedRows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID + "-hcp-group-sum"
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })
	seedHCPServerFixtures(t, pool, orgID)

	ctx := context.Background()
	for _, te := range [][2]string{
		{"short", "cost"}, {"short", "performance"},
		{"medium", "cost"}, {"medium", "performance"},
		{"long", "cost"}, {"long", "performance"},
	} {
		_, err := pool.Exec(ctx, `
			INSERT INTO recommendation_sets (
				org_id, cluster_uuid, namespace, workload, workload_type,
				container_name, term, engine, stale, updated_at, hosted_cluster_id,
				container_id, monitoring_start_time, monitoring_end_time,
				rec_cpu_request_millicores, rec_memory_request_kib,
				estimated_savings_cents
			) VALUES ($1, $2, $3, $4, 'deployment', $4, $7, $8, false, now(), $5,
				$6,
				now() - interval '25 hours', now() - interval '1 hour', 100, 1024, $9)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine)
			DO UPDATE SET estimated_savings_cents = EXCLUDED.estimated_savings_cents`,
			orgID, hcpTestMC1, hcpTestNS1, "coredns", hcpTestHC1, "88888888-8888-8888-8888-888888888888", te[0], te[1], 100000)
		require.NoError(t, err)
	}

	e := hcpTestEcho(nil)
	code, body := hcpGET(t, e, makeIdentityHeader(orgID),
		"/api/cost-management/v1/recommendations/openshift/hcp?group_by[hosted_cluster_id]=*")
	require.Equal(t, http.StatusOK, code, "body: %v", body)
	byHC := map[string]map[string]interface{}{}
	for _, item := range hcpDataItems(t, body) {
		m := item.(map[string]interface{})
		byHC[m["hosted_cluster_id"].(string)] = m
	}
	require.Contains(t, byHC, hcpTestHC1)
	assert.Equal(t, float64(2), byHC[hcpTestHC1]["count"])
	sav := byHC[hcpTestHC1]["estimated_savings"].(map[string]interface{})
	assert.Equal(t, "4000.00", sav["value"], "SUM of pinned rows (300000+100000), not MAX, not all variants")
	assert.Equal(t, "USD", sav["units"])
}

// TestHCPDetail_ScopeGate proves detail serves HCP rows with fields and 404s
// app-namespace IDs (scope enforcement, not just filtering).
func TestHCPDetail_ScopeGate(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID + "-hcp-detail"
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })
	seedHCPServerFixtures(t, pool, orgID)

	var etcdID, webID string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT container_id FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2 AND workload = 'etcd'`,
		orgID, hcpTestMC1).Scan(&etcdID))
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT container_id FROM recommendation_sets WHERE org_id = $1 AND workload = 'web'`,
		orgID).Scan(&webID))

	e := hcpTestEcho(nil)
	code, body := hcpGET(t, e, makeIdentityHeader(orgID),
		"/api/cost-management/v1/recommendations/openshift/hcp/"+etcdID)
	require.Equal(t, http.StatusOK, code, "body: %v", body)
	assert.Equal(t, hcpTestHC1, body["hosted_cluster_id"])

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/hcp/"+webID, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code, "app-namespace IDs must 404 on the HCP surface")
}

// TestHCPList_RBACAdversarial proves the hosted filter never grants access:
// a caller scoped to MC1 cannot reach MC2's HC through the HC filter, and a
// project-scoped caller outside HCP namespaces sees nothing.
func TestHCPList_RBACAdversarial(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID + "-hcp-rbac"
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })
	seedHCPServerFixtures(t, pool, orgID)

	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = true
	t.Cleanup(func() { cfg.RBACEnabled = orig })

	clusterScoped := hcpTestEcho(map[string][]string{"openshift.cluster": {hcpTestMC1}})
	code, body := hcpGET(t, clusterScoped, makeIdentityHeader(orgID),
		"/api/cost-management/v1/recommendations/openshift/hcp?limit=50&filter[hosted_cluster_id]="+hcpTestHC2)
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, hcpDataItems(t, body), "MC1-scoped caller must not reach MC2's HC via the filter")

	projectScoped := hcpTestEcho(map[string][]string{
		"openshift.cluster": {"*"},
		"openshift.project": {"tenant-app"},
	})
	code, body = hcpGET(t, projectScoped, makeIdentityHeader(orgID),
		"/api/cost-management/v1/recommendations/openshift/hcp?limit=50")
	require.Equal(t, http.StatusOK, code)
	assert.Empty(t, hcpDataItems(t, body), "callers outside HCP namespaces see nothing")
}

// TestHCPDisabledPlugin_Routes404 proves the disabled-plugin guards shadow
// the surface instead of falling through to :recommendation-id (ADR-0168).
func TestHCPDisabledPlugin_Routes404(t *testing.T) {
	t.Setenv("ROS_ENABLED_PLUGINS", "container")
	config.ResetForTest()
	_ = config.GetConfig()
	t.Cleanup(func() { config.ResetForTest(); _ = config.GetConfig() })

	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	api.RegisterV1RoutesForTest(v1, nil)

	for _, path := range []string{
		"/api/cost-management/v1/recommendations/openshift/hcp",
		"/api/cost-management/v1/recommendations/openshift/hcp/some-id",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
		assert.True(t, strings.Contains(rec.Body.String(), "hcp"), "guard names the plugin: %s", path)
	}
}
