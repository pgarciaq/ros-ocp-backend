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

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #814 cycle 1's multi-value world: resolved_tags hold sorted arrays —
// every fixture below seeds via raw SQL so the tests pin the STORAGE
// contract (and compile against the pre-#814 tree, for the baseline RED).

// seedMultiValueTagWorld seeds three namespaces on the test cluster:
//   - multi-ns:  environment = ["staging","production"] (unsorted array —
//     also pins the enrichment projection's defensive sort)
//   - scalar-ns: environment = "production" (the pre-#814 scalar legacy)
//   - other-ns:  environment = "dev"
//
// Each has one org_container_keys row and one matching recommendation_sets
// row (medium/cost, savings 10000 cents), so the container-list filter
// surfaces them.
func seedMultiValueTagWorld(t *testing.T, ctx context.Context) {
	t.Helper()
	for _, ns := range []string{"multi-ns", "scalar-ns", "other-ns"} {
		var tags string
		switch ns {
		case "multi-ns":
			// Unsorted arrays pin the enrichment projection's defensive sort;
			// the tier key is unique to this namespace, isolating it in filters.
			tags = `{"environment":["staging","production"],"team":"platform","tier":["silver","gold"]}`
		case "scalar-ns":
			tags = `{"environment":"production"}`
		default:
			tags = `{"environment":"dev"}`
		}
		_, err := database.Pool.Exec(ctx, `
			INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name, resolved_tags)
			VALUES ($1, $2, $3, 'w-`+ns+`', 'Deployment', 'c-`+ns+`', $4::jsonb)
			ON CONFLICT (org_id, namespace, workload, container_name)
			DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags, cluster_uuid = EXCLUDED.cluster_uuid`,
			testutil.TestOrgID, testutil.TestClusterUUID, ns, tags)
		require.NoError(t, err)
		_, err = database.Pool.Exec(ctx, `
			INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
			VALUES ($1, $2, $3, 'w-`+ns+`', 'Deployment', 'c-`+ns+`', 'medium', 'cost', false, '{}', 10000, now())
			ON CONFLICT DO NOTHING`,
			testutil.TestOrgID, testutil.TestClusterUUID, ns)
		require.NoError(t, err)
	}
}

func multiValueListProjects(t *testing.T, app *echo.Echo, identity, query string) []string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?limit=50"+query, nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	data, _ := body["data"].([]interface{})
	out := make([]string, 0, len(data))
	for _, raw := range data {
		item, _ := raw.(map[string]interface{})
		if p, ok := item["project"].(string); ok {
			out = append(out, p)
		}
	}
	return out
}

// TestTagFilters_MultiValueStoredArray_MatchesAnyOf: an array-valued tag
// matches a filter on ANY of its values (D2 containment), and a scalar
// legacy row matches the same filter (D4 transition tolerance) — both in
// one query. Baseline: the array row does not match (the scalar `->>`
// extraction returns JSON text, not a value) — and at HEAD the sync 400s,
// so an array row could not even exist.
func TestTagFilters_MultiValueStoredArray_MatchesAnyOf(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()
	// The base fixture adds test-ns (environment=production, team=platform)
	// and other-ns (environment=staging) to every world — expected sets below
	// include them.
	seedMultiValueTagWorld(t, ctx)

	// production: the array row (containment), the scalar legacy row, and
	// the base fixture's scalar test-ns.
	assert.ElementsMatch(t, []string{"multi-ns", "scalar-ns", testutil.TestNamespace},
		multiValueListProjects(t, app, identity, "&filter%5Btag%3Aenvironment%5D=production"),
		"production must match the array-valued row (containment) and the scalar legacy rows")

	// staging: the array row and the base fixture's scalar other-ns.
	assert.ElementsMatch(t, []string{"multi-ns", "other-ns"},
		multiValueListProjects(t, app, identity, "&filter%5Btag%3Aenvironment%5D=staging"),
		"staging must match the array-valued row")

	// Any-of across a multi-value filter: production,staging — the union.
	assert.ElementsMatch(t, []string{"multi-ns", "scalar-ns", testutil.TestNamespace, "other-ns"},
		multiValueListProjects(t, app, identity, "&filter%5Btag%3Aenvironment%5D=production,staging"),
		"the comma any-of filter must union both shapes' matches")

	// Control: a value nobody holds matches nothing.
	assert.Empty(t,
		multiValueListProjects(t, app, identity, "&filter%5Btag%3Aenvironment%5D=qa"))

	// The other key on the array row still filters by its scalar value —
	// both multi-ns and the base fixture's test-ns carry team=platform.
	assert.ElementsMatch(t, []string{"multi-ns", testutil.TestNamespace},
		multiValueListProjects(t, app, identity, "&filter%5Btag%3Ateam%5D=platform"))
}

// TestTagFilters_MultiValue_NamespaceList covers the org_namespace_keys
// (onk) filter site: the array-valued namespace matches any of its values,
// the scalar legacy namespace still matches.
func TestTagFilters_MultiValue_NamespaceList(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()

	_, err := database.Pool.Exec(ctx, `
		INSERT INTO org_namespace_keys (org_id, cluster_uuid, namespace_name, last_reported, resolved_tags)
		VALUES ($1, $2, 'multi-ns', NOW(), '{"environment":["staging","production"]}'::jsonb),
		       ($1, $2, 'scalar-ns', NOW(), '{"environment":"production"}'::jsonb),
		       ($1, $2, 'other-ns', NOW(), '{"environment":"dev"}'::jsonb)
		ON CONFLICT (org_id, cluster_uuid, namespace_name)
		DO UPDATE SET resolved_tags = EXCLUDED.resolved_tags`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)
	for _, ns := range []string{"multi-ns", "scalar-ns", "other-ns"} {
		_, err = database.Pool.Exec(ctx, `
			INSERT INTO namespace_recommendation_sets (
				org_id, cluster_uuid, namespace_name, term, engine, stale,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				current_cpu_request_millicores, current_cpu_limit_millicores,
				current_memory_request_kib, current_memory_limit_kib,
				variation_cpu_request_pct, variation_cpu_limit_pct,
				variation_memory_request_pct, variation_memory_limit_pct,
				confidence_level, notification_codes, monitoring_start_time, monitoring_end_time, updated_at
			) VALUES ($1, $2, $3, 'medium', 'cost', false,
				4000, 8000, 8388608, 16777216, 5000, 10000, 10485760, 20971520,
				-10, -10, -10, -10, 0.9, '{}', NOW() - 7 * interval '1 day', NOW(), NOW())
			ON CONFLICT DO NOTHING`,
			testutil.TestOrgID, testutil.TestClusterUUID, ns)
		require.NoError(t, err)
	}

	listNS := func(filterValue string) []string {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/namespaces?filter%5Btag%3Aenvironment%5D="+filterValue, nil)
		req.Header.Set("X-Rh-Identity", identity)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		data, _ := body["data"].([]interface{})
		out := make([]string, 0, len(data))
		for _, raw := range data {
			item, _ := raw.(map[string]interface{})
			if p, ok := item["project"].(string); ok {
				out = append(out, p)
			}
		}
		return out
	}

	assert.ElementsMatch(t, []string{"multi-ns", "scalar-ns"}, listNS("production"),
		"the onk site must match the array row and the scalar legacy row")
	assert.ElementsMatch(t, []string{"multi-ns"}, listNS("staging"))
}

// TestTagEnrichment_MultiValueProjection pins the D5 phase-1 projection:
// the response's tags stay map[string]string (koku-ui and IQE untouched),
// an array-valued key projects to its alphabetically-first value — even
// when the stored array is unsorted (the projection sorts defensively, not
// trusting write discipline).
func TestTagEnrichment_MultiValueProjection(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()
	seedMultiValueTagWorld(t, ctx)

	// Fetch unfiltered (not via a tag filter) so the RED pins the enrichment
	// mechanism itself — the pre-#814 unmarshal dropping array-valued tags —
	// independent of the filter rewrite.
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift?limit=50", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Data []struct {
			Project string            `json:"project"`
			Tags    map[string]string `json:"tags"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	var row = -1
	for i := range body.Data {
		if body.Data[i].Project == "multi-ns" {
			row = i
			break
		}
	}
	require.GreaterOrEqual(t, row, 0, "the multi-ns row must be present unfiltered")
	require.NotNil(t, body.Data[row].Tags, "the enrichment must not drop array-valued tags (the pre-#814 unmarshal did)")
	assert.Equal(t, "production", body.Data[row].Tags["environment"],
		"the unsorted array [staging,production] projects to its alphabetically-first value")
	assert.Equal(t, "gold", body.Data[row].Tags["tier"],
		"the unsorted array [silver,gold] projects to its alphabetically-first value")
	assert.Equal(t, "platform", body.Data[row].Tags["team"])
}

// TestTagFilters_MultiValue_NodeUtilizationList executes the raw-$N
// exists-clause builder (tagValueContainmentPgx) against array-valued rows —
// the #814 review found the pgx path had only shape assertions and
// scalar-row executions; an array-arm regression there would ship green.
// The node-utilization clause is cluster-scoped (no namespace expression):
// any namespace in the cluster holding the tag value makes the node match.
func TestTagFilters_MultiValue_NodeUtilizationList(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()
	seedMultiValueTagWorld(t, ctx)

	_, err := database.Pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, category,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95, cpu_overcommit_ratio, pod_count, trend_slope,
			notification_codes, estimated_savings_cents, updated_at)
		VALUES ($1, $2, 'node-multi', 'medium', 'cost', 'underutilized', 10, 20, 10, 20, 1, 1, 0, '{}', 30000, now())
		ON CONFLICT DO NOTHING`, testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	listNodes := func(filterQuery string) int {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/nodes?"+filterQuery, nil)
		req.Header.Set("X-Rh-Identity", identity)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		data, _ := body["data"].([]interface{})
		return len(data)
	}

	// tier=gold is held ONLY by the array row (multi-ns) — the base
	// fixture's scalar rows cannot satisfy it, so a match proves the
	// containment arm executed through the raw-$N clause.
	assert.NotEmpty(t, listNodes("filter%5Btag%3Atier%5D=gold"),
		"the raw-$N exists clause must match the array-valued row (containment arm)")
	assert.NotEmpty(t, listNodes("filter%5Btag%3Aenvironment%5D=production"),
		"the raw-$N exists clause must match the scalar legacy rows too")
	assert.Empty(t, listNodes("filter%5Btag%3Aenvironment%5D=qa"), "negative control: a value nobody holds")
}

// TestTagFilters_MultiValue_ClusterQuotaList executes the comma-namespaces
// exists-clause variant (tagFilterAPIExistsClauseCommaNamespaces) against an
// array-valued row — the second raw-$N site the review found unexecuted on
// array storage.
func TestTagFilters_MultiValue_ClusterQuotaList(t *testing.T) {
	withTagsEnabled(t)

	app, identity, ctx, cleanup := setupTagsIntegrationApp(t)
	defer cleanup()
	seedMultiValueTagWorld(t, ctx)

	_, err := database.Pool.Exec(ctx, `
		INSERT INTO cluster_quota_recommendation_sets (
			org_id, cluster_uuid, cluster_quota_name,
			cpu_request_hard, cpu_request_used, cpu_request_recommended,
			recommendation_type, risk_level, namespaces,
			utilization_cpu_request_percent,
			savings_cpu_cores_freed, savings_memory_bytes_freed,
			savings_storage_bytes_freed, savings_pods_freed,
			estimated_savings_cents, notification_codes
		) VALUES ($1, $2, 'crq-multi', 100000, 25000, 36000, 'tighten', 'low', 'multi-ns', 25,
			2, 1073741824, 5368709120, 5, 4200, '{}')
		ON CONFLICT (org_id, cluster_uuid, cluster_quota_name) DO UPDATE SET
			namespaces = EXCLUDED.namespaces`,
		testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	listQuota := func(filterValue string) int {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/cluster-quota?filter%5Btag%3Aenvironment%5D="+filterValue, nil)
		req.Header.Set("X-Rh-Identity", identity)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		data, _ := body["data"].([]interface{})
		return len(data)
	}

	assert.Equal(t, 1, listQuota("staging"),
		"the comma-namespaces clause must match the array-valued row via multi-ns")
	assert.Equal(t, 1, listQuota("production"))
	assert.Empty(t, listQuota("qa"), "negative control")
}

// TestMatchingContainerKeys_APIMode_MultiValueRows executes the third raw
// site (MatchingContainerKeys' api branch in tag_filter_keys.go) against
// array storage — model-level, since the endpoint path (GPU time-slicing
// node recs) needs the full digest fixture.
func TestMatchingContainerKeys_APIMode_MultiValueRows(t *testing.T) {
	withTagsEnabled(t)

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedMultiValueTagWorld(t, ctx)

	keysFor := func(values ...string) map[string]struct{} {
		filters := []model.TagFilter{{Key: "environment", Values: values}}
		keys, err := model.MatchingContainerKeys(ctx, pool, testutil.TestOrgID, filters)
		require.NoError(t, err)
		return keys
	}

	assert.Equal(t,
		map[string]struct{}{
			testutil.TestClusterUUID + "\x00multi-ns\x00w-multi-ns\x00c-multi-ns": {},
		},
		keysFor("staging"),
		"the pgx api branch must match the array-valued row via containment")
	assert.Equal(t,
		map[string]struct{}{
			testutil.TestClusterUUID + "\x00multi-ns\x00w-multi-ns\x00c-multi-ns":    {},
			testutil.TestClusterUUID + "\x00scalar-ns\x00w-scalar-ns\x00c-scalar-ns": {},
		},
		keysFor("production"),
		"both shapes: the array row (containment) and the scalar legacy row")
	assert.Empty(t, keysFor("qa"), "negative control")
}
