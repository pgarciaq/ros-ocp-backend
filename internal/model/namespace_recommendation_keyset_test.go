package model_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
)

func seedNativeNamespacePagination(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	end := testutil.BaseDate.AddDate(0, 0, 6)
	for _, ns := range []string{"ns-alpha", "ns-beta", "ns-gamma"} {
		testutil.SeedNamespaceDigestSeries(t, pool, ns, 7, 200, 10, 524288, 1024)
	}

	results, err := engine.RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 6)
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, results))
}

func TestGetNativeNamespaceRecommendations_KeysetPagination(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	seedNativeNamespacePagination(t, pool)
	t.Cleanup(func() { database.DB = nil })

	opts := listoptions.ListOptions{
		Limit:    1,
		OrderBy:  listoptions.DefaultNsRecsDBColumn,
		OrderHow: listoptions.OrderDesc,
	}
	page1, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, page1.Results, 1)
	require.GreaterOrEqual(t, page1.Count, 2)
	require.True(t, page1.HasNext)
	require.NotNil(t, page1.LastAnchor)

	opts.HasCursor = true
	opts.AfterNamespaceName = page1.LastAnchor.Namespace
	opts.AfterNSClusterUUID = page1.LastAnchor.ClusterUUID
	opts.AfterNSSortPresent = true
	opts.AfterNSSortValue = page1.LastAnchor.SortValue

	page2, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, page2.Results, 1)
	assert.NotEqual(t, page1.Results[0].ID, page2.Results[0].ID)
}

func TestGetNativeNamespaceRecommendations_KeysetTiedSortColumn(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	seedNativeNamespacePagination(t, pool)

	// Identical last_reported_at forces tie-breaking on (cluster_uuid, namespace_name).
	_, err := pool.Exec(ctx, `UPDATE clusters SET last_reported_at = '2020-01-01T00:00:00Z' WHERE cluster_uuid = $1`, testutil.TestClusterUUID)
	require.NoError(t, err)

	opts := listoptions.ListOptions{
		Limit:    1,
		OrderBy:  listoptions.DefaultNsRecsDBColumn,
		OrderHow: listoptions.OrderDesc,
	}
	seen := map[string]struct{}{}
	for i := 0; i < 3; i++ {
		page, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}}, money.DefaultCurrency)
		require.NoError(t, err)
		require.Len(t, page.Results, 1)
		id := page.Results[0].ID
		_, dup := seen[id]
		assert.False(t, dup, "namespace %s appeared on multiple keyset pages", id)
		seen[id] = struct{}{}
		if !page.HasNext {
			break
		}
		require.NotNil(t, page.LastAnchor)
		opts.HasCursor = true
		opts.AfterNamespaceName = page.LastAnchor.Namespace
		opts.AfterNSClusterUUID = page.LastAnchor.ClusterUUID
		opts.AfterNSSortPresent = true
		opts.AfterNSSortValue = page.LastAnchor.SortValue
	}
	assert.GreaterOrEqual(t, len(seen), 2)
}

// TestGetNativeNamespaceRecommendations_DetailFilterEligibility is #842's
// namespace RED (branch-review finding 3): the namespace keys path applies
// detail-only predicates after pagination, so counts and HasNext lie. Three
// namespaces — ns-a short-term only, ns-b and ns-c long-term only. Filtering
// term=long with limit 1 must count 2, return ns-b, and report HasNext
// (ns-c remains); the baseline counts 3, selects ns-a's key, returns
// nothing, and reports HasNext=false while matches exist.
func TestGetNativeNamespaceRecommendations_DetailFilterEligibility(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	var recs []namespace.NamespaceRec
	for _, spec := range []struct {
		ns   string
		term string
	}{
		{"ns-a", "short"},
		{"ns-b", "long"},
		{"ns-c", "long"},
	} {
		recs = append(recs, namespace.NamespaceRec{
			OrgID:           testutil.TestOrgID,
			ClusterUUID:     testutil.TestClusterUUID,
			Namespace:       spec.ns,
			Term:            spec.term,
			Engine:          "cost",
			RecCPURequestMC: 100,
		})
	}
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, recs))
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, testutil.TestOrgID))

	// ns.stale=false routes through the org_namespace_keys path.
	queryParams := map[string]interface{}{
		"ns.stale = ?": false,
		"ns.term IN ?": []string{"long"},
	}
	opts := listoptions.ListOptions{
		Limit:    1,
		OrderBy:  "namespace_recommendation_sets.namespace_name",
		OrderHow: listoptions.OrderAsc,
	}
	page1, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, queryParams, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)

	assert.Equal(t, 2, page1.Count,
		"the count must apply the term filter — only ns-b and ns-c carry long-term rows (#842)")
	require.Len(t, page1.Results, 1,
		"the page must select an ELIGIBLE key — ns-b, not the nonmatching ns-a (#842)")
	assert.Equal(t, "ns-b", page1.Results[0].Project)
	assert.True(t, page1.HasNext,
		"HasNext must be true — ns-c's long-term row remains beyond the page (#842)")
}

// TestGetNativeNamespaceRecommendations_CategoryFilterWorks is #841's
// namespace RED: "ns.category IN ?" is accepted but unclassified — the
// filter is silently dropped.
func TestGetNativeNamespaceRecommendations_CategoryFilterWorks(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	var recs []namespace.NamespaceRec
	for _, spec := range []struct {
		ns string
	}{
		{"ns-a"}, {"ns-b"},
	} {
		recs = append(recs, namespace.NamespaceRec{
			OrgID:           testutil.TestOrgID,
			ClusterUUID:     testutil.TestClusterUUID,
			Namespace:       spec.ns,
			Term:            "short",
			Engine:          "cost",
			RecCPURequestMC: 100,
		})
	}
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, recs))
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, testutil.TestOrgID))
	_, err = pool.Exec(ctx, `UPDATE namespace_recommendation_sets SET category = 'zombie' WHERE namespace_name = $1 AND org_id = $2`, "ns-a", testutil.TestOrgID)
	require.NoError(t, err)

	queryParams := map[string]interface{}{
		"ns.stale = ?":     false,
		"ns.category IN ?": []string{"zombie"},
	}
	opts := listoptions.ListOptions{Limit: 10, OrderBy: "namespace_recommendation_sets.namespace_name", OrderHow: listoptions.OrderAsc}
	page, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, queryParams, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	assert.Equal(t, 1, page.Count, "the namespace category filter must gate the count (#841)")
	require.Len(t, page.Results, 1, "the namespace category filter must actually filter — baseline drops it (#841)")
	assert.Equal(t, "ns-a", page.Results[0].Project)
}

// TestGetNativeNamespaceRecommendations_NumericSortOrder is #843's RED
// (branch-review finding 4): the namespace keys path casts the sort value
// to TEXT, so numeric sorts with different digit lengths order
// lexicographically — descending savings returned 2, 100, 10 instead of
// 100, 10, 2 (Sol's executed probe). Seed namespaces with savings values
// chosen so the text order differs from the numeric order.
func TestGetNativeNamespaceRecommendations_NumericSortOrder(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	// Savings values whose text and numeric orderings differ:
	// 2 cents, 10 cents, 100 cents. Text DESC: "2" > "10" > "100" (wrong).
	// Numeric DESC: 100 > 10 > 2 (correct).
	var recs []namespace.NamespaceRec
	for _, spec := range []struct {
		ns      string
		savings int64 // cents
	}{
		{"ns-two", 2},
		{"ns-ten", 10},
		{"ns-hundred", 100},
	} {
		rec := namespace.NamespaceRec{
			OrgID:           testutil.TestOrgID,
			ClusterUUID:     testutil.TestClusterUUID,
			Namespace:       spec.ns,
			Term:            "short",
			Engine:          "cost",
			RecCPURequestMC: 100,
		}
		recs = append(recs, rec)
	}
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, recs))
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, testutil.TestOrgID))
	for _, spec := range []struct {
		ns      string
		savings int64
	}{{"ns-two", 2}, {"ns-ten", 10}, {"ns-hundred", 100}} {
		_, err := pool.Exec(ctx, `UPDATE namespace_recommendation_sets SET estimated_savings_cents = $1 WHERE namespace_name = $2 AND org_id = $3`, spec.savings, spec.ns, testutil.TestOrgID)
		require.NoError(t, err)
	}

	queryParams := map[string]interface{}{"ns.stale = ?": false}
	opts := listoptions.ListOptions{
		Limit:    10,
		OrderBy:  "namespace_recommendation_sets.estimated_savings_cents",
		OrderHow: listoptions.OrderDesc,
	}
	page, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, queryParams, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, page.Results, 3)

	var order []string
	for _, r := range page.Results {
		order = append(order, r.Project)
	}
	assert.Equal(t, []string{"ns-hundred", "ns-ten", "ns-two"}, order,
		"numeric savings must sort numerically: 100 > 10 > 2 — the text-cast page key sorts lexicographically (#843)")

	// Cursor continuation with limit 1: pages must walk the numeric order.
	opts.Limit = 1
	opts.HasCursor = false
	p1, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, queryParams, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, p1.Results, 1)
	assert.Equal(t, "ns-hundred", p1.Results[0].Project)

	opts.HasCursor = true
	opts.AfterNSSortPresent = true
	opts.AfterNSSortValue = p1.LastAnchor.SortValue
	opts.AfterNSClusterUUID = p1.LastAnchor.ClusterUUID
	opts.AfterNamespaceName = p1.LastAnchor.Namespace
	p2, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, queryParams, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, p2.Results, 1)
	assert.Equal(t, "ns-ten", p2.Results[0].Project,
		"the cursor continuation must follow the NUMERIC order — 10 before 2 (#843)")

	// Page 3 after ns-ten (10): must reach ns-two (2). With TEXT seeking,
	// "2" > "10" is true lexicographically, so a text comparison would
	// omit ns-two — the page-two assertion alone cannot distinguish
	// (#843 review F3).
	opts.AfterNSSortValue = p2.LastAnchor.SortValue
	opts.AfterNSClusterUUID = p2.LastAnchor.ClusterUUID
	opts.AfterNamespaceName = p2.LastAnchor.Namespace
	p3, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, queryParams, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, p3.Results, 1)
	assert.Equal(t, "ns-two", p3.Results[0].Project,
		"page 3 must reach ns-two — text seeking would omit it: '2' > '10' lexicographically (#843 review F3)")
	assert.False(t, p3.HasNext)
}

// TestGetNativeNamespaceRecommendations_LegacyRequestSortNumeric is #843
// review F1's regression: the legacy request columns (cpu_request_current,
// memory_request_current) are numeric but match no suffix — sorting by them
// was still lexicographic.
func TestGetNativeNamespaceRecommendations_LegacyRequestSortNumeric(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	var recs []namespace.NamespaceRec
	for _, spec := range []struct {
		ns string
	}{
		{"ns-a"}, {"ns-b"}, {"ns-c"},
	} {
		recs = append(recs, namespace.NamespaceRec{
			OrgID:           testutil.TestOrgID,
			ClusterUUID:     testutil.TestClusterUUID,
			Namespace:       spec.ns,
			Term:            "short",
			Engine:          "cost",
			RecCPURequestMC: 100,
		})
	}
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, recs))
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, testutil.TestOrgID))
	// Seed the legacy column with values whose text and numeric order differ.
	for _, spec := range []struct {
		ns string
		v  int64
	}{{"ns-a", 2}, {"ns-b", 10}, {"ns-c", 100}} {
		_, err := pool.Exec(ctx, `UPDATE namespace_recommendation_sets SET cpu_request_current = $1 WHERE namespace_name = $2 AND org_id = $3`, spec.v, spec.ns, testutil.TestOrgID)
		require.NoError(t, err)
	}

	queryParams := map[string]interface{}{"ns.stale = ?": false}
	opts := listoptions.ListOptions{
		Limit:    10,
		OrderBy:  "namespace_recommendation_sets.cpu_request_current",
		OrderHow: listoptions.OrderDesc,
	}
	page, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, queryParams, map[string][]string{"*": {}}, money.DefaultCurrency)
	require.NoError(t, err)
	require.Len(t, page.Results, 3)
	var order []string
	for _, r := range page.Results {
		order = append(order, r.Project)
	}
	assert.Equal(t, []string{"ns-c", "ns-b", "ns-a"}, order,
		"legacy request columns must sort numerically — the _current suffix is now detected (#843 review F1)")
}
