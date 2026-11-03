package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #790: filter[cluster]=<alias> returned 503 on five endpoints because they
// bound the raw filter value against a uuid column (SQLSTATE 22P02). These
// regressions pin the shared predicate: both filter forms return 200 with
// correctly-scoped rows; an unknown alias is 200+empty (Koku's contract);
// and the client filter composes with — never bypasses — the #752
// server-owned cluster scope.

const aliasFilterClusterB = "99999999-9999-9999-9999-999999999999"
const aliasFilterAliasA = "alpha-a"
const aliasFilterAliasB = "beta-b"

// seedAliasFilterOrg seeds two clusters in one org, each with a DISTINCT
// alias, each carrying one row on all five surfaces.
func seedAliasFilterOrg(t *testing.T, ctx context.Context) {
	t.Helper()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	for _, cl := range []struct{ uuid, alias string }{
		{testutil.TestClusterUUID, aliasFilterAliasA},
		{aliasFilterClusterB, aliasFilterAliasB},
	} {
		_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
			VALUES (1, $2, $1, $3, 'src-af', now()) ON CONFLICT DO NOTHING`, cl.uuid, testutil.TestOrgID, cl.alias)
		require.NoError(t, err)
	}

	for _, cu := range []string{testutil.TestClusterUUID, aliasFilterClusterB} {
		_, err = pool.Exec(ctx, `INSERT INTO node_recommendations
			(org_id, cluster_uuid, node, term, engine, cpu_util_p95, mem_util_p95, idle_state, machineset_name, instance_type,
			 node_count_reduction, estimated_savings_cents, updated_at)
			VALUES ($1, $2, 'node-af', 'medium', 'cost', 0.50, 0.55, 'active', 'ms-af', 'm5.large', 0, 100, now())`,
			testutil.TestOrgID, cu)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `INSERT INTO pvc_recommendation_sets
			(org_id, cluster_uuid, namespace, persistentvolumeclaim, capacity_bytes, estimated_savings_cents)
			VALUES ($1, $2, 'ns-af', 'pvc-af', 1000, 100)`,
			testutil.TestOrgID, cu)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `INSERT INTO quota_recommendation_sets
			(org_id, cluster_uuid, namespace, quota_name, recommendation_type, risk_level)
			VALUES ($1, $2, 'ns-af', 'quota-af', 'decrease', 'low')`,
			testutil.TestOrgID, cu)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `INSERT INTO cluster_quota_recommendation_sets
			(org_id, cluster_uuid, cluster_quota_name, recommendation_type, risk_level)
			VALUES ($1, $2, 'crq-af', 'decrease', 'low')`,
			testutil.TestOrgID, cu)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `INSERT INTO snapshot_recommendation_sets
			(org_id, cluster_uuid, namespace, snapshot_name, recommendation_type, estimated_cost_cents, age_days, creation_timestamp)
			VALUES ($1, $2, 'ns-af', 'snap-af', 'delete', 500, 10, now())`,
			testutil.TestOrgID, cu)
		require.NoError(t, err)
	}
}

func aliasFilterApp(perms map[string][]string) *echo.Echo {
	return permsApp(perms, func(v1 *echo.Group) {
		v1.GET("/recommendations/openshift/nodes", api.GetNodeUtilizationRecs)
		v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)
		v1.GET("/recommendations/openshift/snapshots", api.GetSnapshotRecommendations)
		v1.GET("/recommendations/openshift/quota", api.GetQuotaRecommendations)
		v1.GET("/recommendations/openshift/cluster-quota", api.GetClusterQuotaRecommendations)
	})
}

func aliasFilterGET(t *testing.T, perms map[string][]string, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	aliasFilterApp(perms).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// aliasFilterCases runs the four-case matrix against one endpoint path
// template ({cluster} is replaced by the filter value).
func aliasFilterCases(t *testing.T, basePath, clusterParam string) {
	t.Helper()
	filterPath := func(val string) string {
		return basePath + "?" + clusterParam + "=" + val
	}

	// Alias form: 200, only the aliased cluster's rows — asserted by ROW
	// IDENTITY, not just count (#790 review: a count of one with the wrong
	// cluster passed the old assertion).
	status, body := aliasFilterGET(t, nil, filterPath(aliasFilterAliasB))
	require.Equal(t, http.StatusOK, status,
		"filter by alias must be 200 (pre-fix: 503 SQLSTATE 22P02; body: %s)", body)
	rows := dataRows(t, body)
	require.Len(t, rows, 1, "alias filter scopes to one cluster's rows")
	assert.Equal(t, aliasFilterClusterB, fmt.Sprint(rows[0]["cluster_uuid"]),
		"the returned row is the aliased cluster's (row identity, not just count)")

	// Partial and case-varied aliases match (ILIKE substring — the working
	// endpoints' include-mode semantics; pre-fix-divergence: exact match).
	status, body = aliasFilterGET(t, nil, filterPath("beta"))
	require.Equal(t, http.StatusOK, status, "partial alias: body: %s", body)
	assert.Len(t, dataRows(t, body), 1, "partial alias matches (ILIKE substring, like the container endpoints)")
	status, body = aliasFilterGET(t, nil, filterPath("BETA-B"))
	require.Equal(t, http.StatusOK, status, "case-varied alias: body: %s", body)
	assert.Len(t, dataRows(t, body), 1, "case-varied alias matches (ILIKE is case-insensitive)")

	// UUID form: 200, same rows (positive control — already works).
	status, body = aliasFilterGET(t, nil, filterPath(aliasFilterClusterB))
	require.Equal(t, http.StatusOK, status, "body: %s", body)
	rows = dataRows(t, body)
	require.Len(t, rows, 1, "UUID filter form unchanged")
	assert.Equal(t, aliasFilterClusterB, fmt.Sprint(rows[0]["cluster_uuid"]),
		"UUID filter returns the same cluster's row")

	// Unknown alias: 200 + empty (Koku contract), never 503, never 4xx.
	status, body = aliasFilterGET(t, nil, filterPath("no-such-cluster"))
	require.Equal(t, http.StatusOK, status,
		"unknown alias must be 200+empty, not 503/4xx (body: %s)", body)
	assert.Empty(t, dataRows(t, body), "unknown alias matches nothing")
}

// dataRows extracts the data array from a list response.
func dataRows(t *testing.T, body string) []map[string]interface{} {
	t.Helper()
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	if resp.Data == nil {
		return nil
	}
	return resp.Data
}

// TestClusterAliasFilter_CompositionWithScope: an A-only RBAC grant
// filtering for B's alias gets 200 + empty — the client filter composes
// with, never bypasses, the #752 server-owned cluster scope.
func TestClusterAliasFilter_CompositionWithScope(t *testing.T) {
	t.Setenv("RBAC_ENABLE", "true")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})
	ctx := context.Background()
	seedAliasFilterOrg(t, ctx)

	paths := []string{
		"/api/cost-management/v1/recommendations/openshift/nodes",
		"/api/cost-management/v1/recommendations/openshift/pvcs",
		"/api/cost-management/v1/recommendations/openshift/snapshots",
		"/api/cost-management/v1/recommendations/openshift/quota",
		"/api/cost-management/v1/recommendations/openshift/cluster-quota",
	}
	for _, path := range paths {
		t.Run(path[strings.LastIndex(path, "/")+1:], func(t *testing.T) {
			// Positive control FIRST (#790 review: without it, an
			// implementation denying everything passes the denial case):
			// A-alias + A-grant → the granted cluster's row.
			status, body := aliasFilterGET(t,
				map[string][]string{"openshift.cluster": {testutil.TestClusterUUID}},
				path+"?filter%5Bcluster%5D="+aliasFilterAliasA)
			require.Equal(t, http.StatusOK, status, "body: %s", body)
			rows := dataRows(t, body)
			require.NotEmpty(t, rows, "A-alias + A-grant must return the granted cluster's row")
			assert.Equal(t, testutil.TestClusterUUID, fmt.Sprint(rows[0]["cluster_uuid"]),
				"the positive control returns the granted cluster (RBAC does not deny everything)")

			// Denial case: A-only grant + B's alias → 200 + empty — the
			// client filter composes with, never bypasses, the scope.
			status, body = aliasFilterGET(t,
				map[string][]string{"openshift.cluster": {testutil.TestClusterUUID}},
				path+"?filter%5Bcluster%5D="+aliasFilterAliasB)
			require.Equal(t, http.StatusOK, status, "body: %s", body)
			assert.Empty(t, dataRows(t, body),
				"an A-only grant filtering for B's alias gets empty — filter/scope AND-compose")
		})
	}
}

// TestClusterAliasFilter_CrossTenantCollision: the same cluster_uuid
// registered under a second tenant with a DIFFERENT alias — filtering by
// the other tenant's alias must return nothing for this org (#790 review
// Important 1: the org-scoped subquery closes cross-tenant alias
// inference; pre-fix-shape: an unscoped subquery would have matched the
// UUID and returned this org's row for it).
func TestClusterAliasFilter_CrossTenantCollision(t *testing.T) {
	setupAliasFilterTest(t)
	ctx := context.Background()
	seedAliasFilterOrg(t, ctx)
	pool := database.GetPool()
	const orgOther = "org-alias-collision-other"
	const otherAlias = "foreign-tenant-alias"
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (99, $1) ON CONFLICT DO NOTHING`, orgOther)
	require.NoError(t, err)
	// The SAME cluster UUID as our cluster B, registered under the other
	// tenant with a distinct alias (the #552 colliding-UUID shape).
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (99, $2, $1, $3, 'src-collide', now()) ON CONFLICT DO NOTHING`, aliasFilterClusterB, orgOther, otherAlias)
	require.NoError(t, err)

	for _, path := range []string{
		"/api/cost-management/v1/recommendations/openshift/nodes",
		"/api/cost-management/v1/recommendations/openshift/pvcs",
		"/api/cost-management/v1/recommendations/openshift/snapshots",
		"/api/cost-management/v1/recommendations/openshift/quota",
		"/api/cost-management/v1/recommendations/openshift/cluster-quota",
	} {
		t.Run(path[strings.LastIndex(path, "/")+1:], func(t *testing.T) {
			status, body := aliasFilterGET(t, nil, path+"?filter%5Bcluster%5D="+otherAlias)
			require.Equal(t, http.StatusOK, status, "body: %s", body)
			assert.Empty(t, dataRows(t, body),
				"the other tenant's alias must not resolve this org's colliding-UUID row (org-scoped subquery)")
		})
	}
}

func setupAliasFilterTest(t *testing.T) {
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
}

// TestClusterAliasFilter_FiveEndpoints: the #790 repro matrix per endpoint.
func TestClusterAliasFilter_FiveEndpoints(t *testing.T) {
	setupAliasFilterTest(t)
	ctx := context.Background()
	seedAliasFilterOrg(t, ctx)

	cases := []struct {
		name         string
		basePath     string
		clusterParam string
	}{
		{"nodes", "/api/cost-management/v1/recommendations/openshift/nodes", "filter%5Bcluster%5D"},
		{"pvcs", "/api/cost-management/v1/recommendations/openshift/pvcs", "filter%5Bcluster%5D"},
		{"snapshots", "/api/cost-management/v1/recommendations/openshift/snapshots", "filter%5Bcluster%5D"},
		{"quota", "/api/cost-management/v1/recommendations/openshift/quota", "filter%5Bcluster%5D"},
		{"cluster-quota", "/api/cost-management/v1/recommendations/openshift/cluster-quota", "filter%5Bcluster%5D"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			aliasFilterCases(t, tc.basePath, tc.clusterParam)
		})
	}
}

// TestClusterAliasFilter_UnfilteredBaseline: both clusters visible without
// a filter (the control that makes the scoped counts meaningful).
func TestClusterAliasFilter_UnfilteredBaseline(t *testing.T) {
	setupAliasFilterTest(t)
	ctx := context.Background()
	seedAliasFilterOrg(t, ctx)

	for _, path := range []string{
		"/api/cost-management/v1/recommendations/openshift/nodes",
		"/api/cost-management/v1/recommendations/openshift/pvcs",
		"/api/cost-management/v1/recommendations/openshift/snapshots",
		"/api/cost-management/v1/recommendations/openshift/quota",
		"/api/cost-management/v1/recommendations/openshift/cluster-quota",
	} {
		status, body := aliasFilterGET(t, nil, path)
		require.Equal(t, http.StatusOK, status, "path %s body: %s", path, body)
		assert.Contains(t, body, aliasFilterClusterB,
			"unfiltered baseline includes cluster B rows (path %s)", path)
		assert.Contains(t, body, testutil.TestClusterUUID,
			"unfiltered baseline includes cluster A rows (path %s)", path)
	}
}
