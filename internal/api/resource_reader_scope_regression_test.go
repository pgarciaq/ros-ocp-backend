package api_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// #752: resource readers used org scoping without consuming explicit cluster
// RBAC restrictions — a cluster-A-only role could enumerate cluster B's
// storage/quota groups and advisories, and read a known B container's OOM
// timeline. These regressions pin server-owned cluster scope on every
// list/group/CSV reader and 404-for-out-of-scope on resolved detail/trend
// paths (Koku convention: RBAC-filtered queryset + get_object → Http404,
// indistinguishable from unknown IDs).

const readerClusterB = "99999999-9999-9999-9999-999999999999"

func readerClusterAPerms() map[string][]string {
	return map[string][]string{"openshift.cluster": {testutil.TestClusterUUID}}
}

// seedReaderScopeOrg seeds clusters A/B in one org with the issue's repro
// data: B's PVC (capacity 987654321, savings 123.45 USD), quota and CRQ rows
// on both clusters, an unexpired B management advisory, and a B container
// with 7 OOM-event days.
func seedReaderScopeOrg(t *testing.T, ctx context.Context) (bContainerID string) {
	t.Helper()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	for _, cu := range []string{testutil.TestClusterUUID, readerClusterB} {
		alias := "reader-scope-a"
		if cu == readerClusterB {
			alias = "reader-scope-b"
		}
		_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
			VALUES (1, $2, $1, $3, 'src-rs', now()) ON CONFLICT DO NOTHING`, cu, testutil.TestOrgID, alias)
		require.NoError(t, err)
	}

	// PVC recommendation sets: A (modest) and B (the repro values).
	_, err = pool.Exec(ctx, `INSERT INTO pvc_recommendation_sets
		(org_id, cluster_uuid, namespace, persistentvolumeclaim, capacity_bytes, estimated_savings_cents)
		VALUES ($1, $2, 'ns-rs', 'pvc-a', 1000, 100), ($1, $3, 'ns-rs', 'pvc-b', 987654321, 12345)`,
		testutil.TestOrgID, testutil.TestClusterUUID, readerClusterB)
	require.NoError(t, err)

	// Quota and cluster-quota rows on both clusters.
	// Quota and cluster-quota rows on both clusters (B gets a quota_id for
	// the resolved-ID trend path).
	bQuotaUUID := "3aaa39c0-0000-4000-8000-00000000000b"
	aQuotaUUID := "3aaa39c0-0000-4000-8000-00000000000a"
	for _, q := range []struct {
		cluster, quotaID string
	}{{testutil.TestClusterUUID, aQuotaUUID}, {readerClusterB, bQuotaUUID}} {
		_, err = pool.Exec(ctx, `INSERT INTO quota_recommendation_sets
			(org_id, cluster_uuid, namespace, quota_name, recommendation_type, risk_level, quota_id)
			VALUES ($1, $2, 'ns-rs', 'quota-rs', 'decrease', 'low', $3)`,
			testutil.TestOrgID, q.cluster, q.quotaID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO cluster_quota_recommendation_sets
			(org_id, cluster_uuid, cluster_quota_name, recommendation_type, risk_level)
			VALUES ($1, $2, 'crq-rs', 'decrease', 'low')`,
			testutil.TestOrgID, q.cluster)
		require.NoError(t, err)
	}

	// Unexpired B management advisory.
	_, err = pool.Exec(ctx, `INSERT INTO hcp_correlation_advisories
		(org_id, hc_cluster_id, management_cluster_uuid, window_start, window_end, verdict, confidence,
		 h_p99_s, h_threshold_s, c_ratio, n_signals, expires_at)
		VALUES ($1, 'hc-rs-b', $2, now() - interval '2 hours', now() - interval '1 hour',
			'add_workers', 'high', 12.5, 10.0, 0.8, '{"webhook_a"}', now() + interval '1 day')`,
		testutil.TestOrgID, readerClusterB)
	require.NoError(t, err)

	// B container with a resolvable recommendation_sets id and 7 OOM days.
	bContainerID = pgrec.NativeContainerID(readerClusterB, "ns-rs", "wl-rs-b", "Deployment", "ctr-rs-b")
	_, err = pool.Exec(ctx, `INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			term, engine, stale, notification_codes, updated_at, container_id,
			monitoring_start_time, monitoring_end_time)
		VALUES ($1, $2, 'ns-rs', 'wl-rs-b', 'Deployment', 'ctr-rs-b',
			'short', 'cost', false, '{}', now(), $3,
			now() - interval '7 days', now() - interval '1 hour')`,
		testutil.TestOrgID, readerClusterB, bContainerID)
	require.NoError(t, err)
	base := time.Now().UTC().AddDate(0, 0, -8)
	for day := 1; day <= 7; day++ {
		d := base.AddDate(0, 0, day).Truncate(24 * time.Hour)
		testutil.EnsureMonthlyPartition(t, pool, "daily_container_digests", d)
		_, err = pool.Exec(ctx, `INSERT INTO daily_container_digests
			(org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			 bucket_date, oom_count_sum, sample_count, cpu_usage_p95_mc, memory_usage_p95_kib)
			VALUES ($1, $2, 'ns-rs', 'wl-rs-b', 'Deployment', 'ctr-rs-b', $3, $4, 4, 100, 2048)`,
			testutil.TestOrgID, readerClusterB, d, day)
		require.NoError(t, err)
	}
	return bContainerID
}

func readerScopeApp(perms map[string][]string) *echo.Echo {
	return permsApp(perms, func(v1 *echo.Group) {
		v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)
		v1.GET("/recommendations/openshift/pvcs/detail", api.GetPVCRecommendationDetail)
		v1.GET("/recommendations/openshift/quota", api.GetQuotaRecommendations)
		v1.GET("/recommendations/openshift/quota/detail", api.GetQuotaRecommendationDetail)
		v1.GET("/recommendations/openshift/quota/:quota-id/trend", api.GetQuotaTrend)
		v1.GET("/recommendations/openshift/cluster-quota", api.GetClusterQuotaRecommendations)
		v1.GET("/recommendations/openshift/cluster-quota/detail", api.GetClusterQuotaRecommendationDetail)
		v1.GET("/recommendations/openshift/hcp/advisories", api.GetHCPAdvisories)
		v1.GET("/recommendations/openshift/containers/:recommendation-id/oom-timeline", api.GetOOMTimeline)
		v1.GET("/recommendations/openshift/workload-types", api.GetWorkloadTypes)
	})
}

func readerScopeGET(t *testing.T, perms map[string][]string, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	readerScopeApp(perms).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func readerScopeJSON(t *testing.T, perms map[string][]string, path string) map[string]interface{} {
	t.Helper()
	_, body := readerScopeGET(t, perms, path)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	return resp
}

func dataClusterUUIDs(t *testing.T, resp map[string]interface{}) []string {
	t.Helper()
	data, ok := resp["data"].([]interface{})
	require.True(t, ok, "data must be an array")
	uuids := make([]string, 0, len(data))
	for _, row := range data {
		m, ok := row.(map[string]interface{})
		require.True(t, ok)
		uuids = append(uuids, fmt.Sprint(m["cluster_uuid"]))
	}
	return uuids
}

// TestReaderScope_PVCListGroupCSV: the issue's primary repro — B's PVC
// capacity 987654321 and savings 123.45 USD must not reach an A-only role
// on the list, group, or CSV surface; the client cluster filter composes
// with (never replaces) the server scope.
func TestReaderScope_PVCListGroupCSV(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedReaderScopeOrg(t, ctx)

	// List: A-only sees only cluster A's row (pre-fix: B's row too).
	resp := readerScopeJSON(t, readerClusterAPerms(), "/api/cost-management/v1/recommendations/openshift/pvcs")
	uuids := dataClusterUUIDs(t, resp)
	assert.Equal(t, []string{testutil.TestClusterUUID}, uuids,
		"A-only must see only cluster A's PVC rows (pre-fix: B's capacity 987654321 / savings 123.45 served)")

	// Group by cluster: A-only gets one group — theirs.
	resp = readerScopeJSON(t, readerClusterAPerms(),
		"/api/cost-management/v1/recommendations/openshift/pvcs?group_by[cluster]=*")
	groups, _ := resp["data"].([]interface{})
	aOnlyGroups := 0
	for _, g := range groups {
		if fmt.Sprint(g.(map[string]interface{})["cluster_uuid"]) == readerClusterB {
			aOnlyGroups++
		}
	}
	assert.Equal(t, 0, aOnlyGroups, "A-only must not enumerate cluster B's PVC group")

	// CSV: A-only gets no B rows.
	_, body := readerScopeGET(t, readerClusterAPerms(),
		"/api/cost-management/v1/recommendations/openshift/pvcs?format=csv")
	r := csv.NewReader(strings.NewReader(body))
	records, csvErr := r.ReadAll()
	require.NoError(t, csvErr)
	for _, rec := range records {
		for _, field := range rec {
			assert.NotContains(t, field, readerClusterB,
				"A-only CSV export must not contain cluster B rows")
		}
	}

	// Client filter composes: A-only filtering FOR cluster B gets empty,
	// not B's rows (a client filter is not authorization).
	resp = readerScopeJSON(t, readerClusterAPerms(),
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[cluster]="+readerClusterB)
	meta := resp["meta"].(map[string]interface{})
	assert.Equal(t, float64(0), meta["count"],
		"A-only filtering for cluster B must get empty (pre-fix: the client filter served B's rows)")

	// Positive controls.
	resp = readerScopeJSON(t, map[string][]string{"*": {}}, "/api/cost-management/v1/recommendations/openshift/pvcs")
	assert.Len(t, dataClusterUUIDs(t, resp), 2, "wildcard sees both clusters' PVC rows")
	emptyGrant := readerScopeJSON(t, map[string][]string{"openshift.cluster": {}},
		"/api/cost-management/v1/recommendations/openshift/pvcs")
	assert.Equal(t, float64(0), emptyGrant["meta"].(map[string]interface{})["count"],
		"explicitly empty cluster grant denies")
}

// TestReaderScope_QuotaAndClusterQuota: same matrix on the quota readers.
func TestReaderScope_QuotaAndClusterQuota(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedReaderScopeOrg(t, ctx)

	for _, path := range []string{
		"/api/cost-management/v1/recommendations/openshift/quota",
		"/api/cost-management/v1/recommendations/openshift/cluster-quota",
	} {
		resp := readerScopeJSON(t, readerClusterAPerms(), path)
		for _, cu := range dataClusterUUIDs(t, resp) {
			assert.Equal(t, testutil.TestClusterUUID, cu,
				"A-only must see only cluster A's rows on %s (pre-fix: B's rows served)", path)
		}
		resp = readerScopeJSON(t, map[string][]string{"*": {}}, path)
		assert.Len(t, dataClusterUUIDs(t, resp), 2, "wildcard sees both clusters on %s", path)
	}
}

// TestReaderScope_HCPAdvisories: B's management advisory must not reach an
// A-only role (the issue's excluded management-cluster advisory).
func TestReaderScope_HCPAdvisories(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedReaderScopeOrg(t, ctx)

	resp := readerScopeJSON(t, readerClusterAPerms(), "/api/cost-management/v1/recommendations/openshift/hcp/advisories")
	data, _ := resp["data"].([]interface{})
	assert.Empty(t, data, "A-only must not receive cluster B's management advisory (pre-fix: served)")

	resp = readerScopeJSON(t, map[string][]string{"*": {}}, "/api/cost-management/v1/recommendations/openshift/hcp/advisories")
	data, _ = resp["data"].([]interface{})
	assert.Len(t, data, 1, "wildcard sees the advisory")
}

// TestReaderScope_OOMTimelineAndDetails: out-of-scope resolved IDs return
// 404 byte-identical to unknown IDs (Koku's queryset convention); the OOM
// timeline serves its 7 entries to an authorized caller only.
func TestReaderScope_OOMTimelineAndDetails(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	bContainerID := seedReaderScopeOrg(t, ctx)
	unknownID := "0af7e1c0-0000-4000-8000-000000000000"

	oomPath := func(id string) string {
		return "/api/cost-management/v1/recommendations/openshift/containers/" + id + "/oom-timeline"
	}

	// A-only requesting B's container: 404 identical to an unknown id
	// (pre-fix: 200 with 7 OOM events).
	scopedStatus, scopedBody := readerScopeGET(t, readerClusterAPerms(), oomPath(bContainerID))
	unknownStatus, unknownBody := readerScopeGET(t, readerClusterAPerms(), oomPath(unknownID))
	require.Equal(t, http.StatusNotFound, scopedStatus,
		"out-of-scope OOM timeline must 404 (pre-fix: 200 with cluster B's events; body: %s)", scopedBody)
	require.Equal(t, http.StatusNotFound, unknownStatus)
	assert.Equal(t, unknownBody, scopedBody, "out-of-scope and unknown IDs must be indistinguishable")

	// Wildcard: 200 with the 7 seeded OOM days.
	wildStatus, wildBody := readerScopeGET(t, map[string][]string{"*": {}}, oomPath(bContainerID))
	require.Equal(t, http.StatusOK, wildStatus, "body: %s", wildBody)
	var timeline struct {
		Data []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(wildBody), &timeline))
	assert.Len(t, timeline.Data, 7, "authorized caller sees the 7 OOM days")

	// Detail paths: A-only requesting B's rows 404s identically to unknown.
	detailCases := []struct {
		name string
		path string
	}{
		{"pvc detail", "/api/cost-management/v1/recommendations/openshift/pvcs/detail?cluster_uuid=" + readerClusterB + "&namespace=ns-rs&persistentvolumeclaim=pvc-b"},
		{"quota detail", "/api/cost-management/v1/recommendations/openshift/quota/detail?cluster_uuid=" + readerClusterB + "&namespace=ns-rs&quota_name=quota-rs"},
		{"crq detail", "/api/cost-management/v1/recommendations/openshift/cluster-quota/detail?cluster_uuid=" + readerClusterB + "&cluster_quota_name=crq-rs"},
	}
	for _, tc := range detailCases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := readerScopeGET(t, readerClusterAPerms(), tc.path)
			assert.Equal(t, http.StatusNotFound, status,
				"out-of-scope detail must 404 (pre-fix: 200 with B's data; body: %s)", body)
			wildStatus, wildBody := readerScopeGET(t, map[string][]string{"*": {}}, tc.path)
			assert.Equal(t, http.StatusOK, wildStatus,
				"wildcard resolves the same detail (body: %s)", wildBody)
		})
	}
}

// TestReaderScope_QuotaTrend: the trend path resolves the quota row's
// cluster; out-of-scope 404s like an unknown id.
func TestReaderScope_QuotaTrend(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedReaderScopeOrg(t, ctx)

	pool := database.GetPool()
	bQuotaUUID := "3aaa39c0-0000-4000-8000-00000000000b"
	var exists int
	err := pool.QueryRow(ctx,
		`SELECT 1 FROM quota_recommendation_sets WHERE org_id = $1 AND quota_id = $2`,
		testutil.TestOrgID, bQuotaUUID).Scan(&exists)
	require.NoError(t, err)

	trendPath := "/api/cost-management/v1/recommendations/openshift/quota/" + bQuotaUUID + "/trend"
	scopedStatus, scopedBody := readerScopeGET(t, readerClusterAPerms(), trendPath)
	assert.Equal(t, http.StatusNotFound, scopedStatus,
		"out-of-scope quota trend must 404 (pre-fix: B's trend served; body: %s)", scopedBody)

	wildStatus, wildBody := readerScopeGET(t, map[string][]string{"*": {}}, trendPath)
	assert.Equal(t, http.StatusOK, wildStatus, "wildcard reads the trend (body: %s)", wildBody)
}

// TestReaderScope_MatrixCompletion (#752 review Important 1): the surfaces
// the first pass under-covered — quota/CRQ groups and CSV, settings-only and
// empty grants per surface, and the review-found workload-types reader.
func TestReaderScope_MatrixCompletion(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedReaderScopeOrg(t, ctx)

	// Quota and CRQ groups: A-only sees one group (theirs), with a status
	// check so an error response cannot satisfy the assertion.
	for _, tc := range []struct{ name, path string }{
		{"quota group", "/api/cost-management/v1/recommendations/openshift/quota?group_by[cluster]=*"},
		{"crq group", "/api/cost-management/v1/recommendations/openshift/cluster-quota?group_by[cluster]=*"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := readerScopeGET(t, readerClusterAPerms(), tc.path)
			require.Equal(t, http.StatusOK, status, "body: %s", body)
			var resp struct {
				Data []map[string]interface{} `json:"data"`
			}
			require.NoError(t, json.Unmarshal([]byte(body), &resp))
			for _, g := range resp.Data {
				assert.Equal(t, testutil.TestClusterUUID, fmt.Sprint(g["cluster_uuid"]),
					"A-only must not enumerate cluster B's group")
			}
		})
	}

	// Quota CSV: A-only gets no B rows.
	status, body := readerScopeGET(t, readerClusterAPerms(),
		"/api/cost-management/v1/recommendations/openshift/quota?format=csv")
	require.Equal(t, http.StatusOK, status, "body: %s", body)
	assert.NotContains(t, body, readerClusterB, "A-only quota CSV must not contain cluster B rows")

	// Settings-only and empty grants deny on every list surface.
	for _, tc := range []struct{ name, path string }{
		{"pvc settings-only", "/api/cost-management/v1/recommendations/openshift/pvcs"},
		{"quota settings-only", "/api/cost-management/v1/recommendations/openshift/quota"},
		{"crq settings-only", "/api/cost-management/v1/recommendations/openshift/cluster-quota"},
		{"advisories settings-only", "/api/cost-management/v1/recommendations/openshift/hcp/advisories"},
		{"pvc empty grant", "/api/cost-management/v1/recommendations/openshift/pvcs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			perms := eligibilitySettingsReadPerms()
			if strings.Contains(tc.name, "empty grant") {
				perms = map[string][]string{"openshift.cluster": {}}
			}
			status, body := readerScopeGET(t, perms, tc.path)
			require.Equal(t, http.StatusOK, status, "body: %s", body)
			var resp struct {
				Meta map[string]interface{}   `json:"meta"`
				Data []map[string]interface{} `json:"data"`
			}
			require.NoError(t, json.Unmarshal([]byte(body), &resp))
			if resp.Meta != nil {
				if count, ok := resp.Meta["count"]; ok {
					assert.Equal(t, float64(0), count, "%s must deny", tc.name)
				}
			}
			assert.Empty(t, resp.Data, "%s must serve no rows", tc.name)
		})
	}

	// Workload types (review-found org-only reader): A-only sees only
	// workload types present in their clusters; settings-only sees none.
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO org_container_keys
		(org_id, cluster_uuid, namespace, workload, workload_type, container_name, last_reported)
		VALUES ($1, $2, 'ns-wt', 'wl-b', 'CronJob', 'ctr-b', now())`,
		testutil.TestOrgID, readerClusterB)
	require.NoError(t, err)

	status, body = readerScopeGET(t, readerClusterAPerms(),
		"/api/cost-management/v1/recommendations/openshift/workload-types")
	require.Equal(t, http.StatusOK, status, "body: %s", body)
	assert.NotContains(t, body, "CronJob",
		"A-only must not learn workload types present only in cluster B (review-found reader)")

	status, body = readerScopeGET(t, eligibilitySettingsReadPerms(),
		"/api/cost-management/v1/recommendations/openshift/workload-types")
	require.Equal(t, http.StatusOK, status, "body: %s", body)
	var wtResp struct {
		Data []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &wtResp))
	assert.Empty(t, wtResp.Data, "settings-only sees no workload types")
}

// TestReaderScope_TagWarningNonDisclosure (#752 review Important 2): a
// scoped empty response must not reveal whether a guessed tag key exists
// elsewhere in the org — the data-derived key catalog is cluster-scoped, so
// a B-only tag key reads as unknown to an A-only user.
func TestReaderScope_TagWarningNonDisclosure(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedReaderScopeOrg(t, ctx)
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO org_container_keys
		(org_id, cluster_uuid, namespace, workload, workload_type, container_name, last_reported, resolved_tags)
		VALUES ($1, $2, 'ns-rs', 'wl-rs-b', 'Deployment', 'ctr-rs-b', now(), '{"b-only-tag": "x"}'::jsonb)`,
		testutil.TestOrgID, readerClusterB)
	require.NoError(t, err)

	if !config.TagsFeatureEnabled() {
		t.Skip("tag feature disabled in this config — warning path inert")
	}

	// The tag-filter join needs the tenant tag table (per-org schema), as in
	// the tags integration tests.
	schema := strings.ReplaceAll(testutil.TestOrgID, "-", "_")
	_, err = database.GetPool().Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS `+schema+`;
		CREATE TABLE IF NOT EXISTS `+schema+`.reporting_ocptags_values (
			uuid UUID PRIMARY KEY,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			cluster_ids TEXT[] NOT NULL,
			cluster_aliases TEXT[] NOT NULL DEFAULT '{}',
			namespaces TEXT[] NOT NULL
		)`)
	require.NoError(t, err)

	// A-only filtering for B's tag key and for a nonexistent key must
	// produce the SAME warning (or absence) — no existence oracle.
	bTagStatus, bTagBody := readerScopeGET(t, readerClusterAPerms(),
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[tag:b-only-tag]=x")
	noneStatus, noneBody := readerScopeGET(t, readerClusterAPerms(),
		"/api/cost-management/v1/recommendations/openshift/pvcs?filter[tag:no-such-tag]=x")
	require.Equal(t, http.StatusOK, bTagStatus, "body: %s", bTagBody)
	require.Equal(t, http.StatusOK, noneStatus, "body: %s", noneBody)

	var bTagResp, noneResp struct {
		Meta struct {
			Warnings []string `json:"warnings"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(bTagBody), &bTagResp))
	require.NoError(t, json.Unmarshal([]byte(noneBody), &noneResp))
	// Both keys must receive the SAME TREATMENT — "not in the known tag
	// catalog" — so the scoped user cannot distinguish a B-only key from a
	// nonexistent one. (Pre-fix: the org-wide catalog knew b-only-tag and
	// suppressed its warning, revealing existence.)
	require.NotEmpty(t, bTagResp.Meta.Warnings, "B-only key must warn like an unknown key")
	require.NotEmpty(t, noneResp.Meta.Warnings)
	assert.Contains(t, bTagResp.Meta.Warnings[0], "not in the known tag catalog",
		"B-only tag key must read as unknown to an A-only user (no existence oracle)")
	assert.Contains(t, noneResp.Meta.Warnings[0], "not in the known tag catalog")
}

// TestReaderScope_ForeignOrgNonDisclosing: same cluster UUID in another org
// never discloses through any reader.
func TestReaderScope_ForeignOrgNonDisclosing(t *testing.T) {
	setupEligibilityTest(t)
	ctx := context.Background()
	seedReaderScopeOrg(t, ctx)
	pool := database.GetPool()
	const orgOther = "org-reader-scope-other"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (98, $1) ON CONFLICT DO NOTHING`, orgOther)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO pvc_recommendation_sets
		(org_id, cluster_uuid, namespace, persistentvolumeclaim, capacity_bytes, estimated_savings_cents)
		VALUES ($1, $2, 'ns-rs', 'pvc-foreign', 555, 55)`, orgOther, readerClusterB)
	require.NoError(t, err)

	// Org scoping already holds (positive control on the current behavior);
	// the scope fix must not weaken it.
	resp := readerScopeJSON(t, map[string][]string{"*": {}}, "/api/cost-management/v1/recommendations/openshift/pvcs")
	for _, cu := range dataClusterUUIDs(t, resp) {
		_ = cu // cluster UUIDs may match; row identity is org-scoped — count proves isolation.
	}
	meta := resp["meta"].(map[string]interface{})
	assert.Equal(t, float64(2), meta["count"],
		"only the caller org's two rows are served — the foreign org's row never discloses")
}

// Silence the unused-config import when setupEligibilityTest is refactored.
var _ = config.ResetForTest
