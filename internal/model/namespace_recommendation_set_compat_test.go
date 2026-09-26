package model_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// seedNativeCompatNamespaces inserts deterministic native namespace rows
// matching the engine's write shape: ns-full as a full six-row sextet
// (short/medium/long × cost/performance), ns-partial as a pinned-less pair
// (performance rows only, no cost engine) to exercise skip-and-count.
func seedNativeCompatNamespaces(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, $2, 'compat-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, orgID, clusterUUID)
	require.NoError(t, err)

	type rowSpec struct {
		ns, term, engine string
		recCPU, recMem   int64
	}
	specs := []rowSpec{
		{"ns-full", "short", "cost", 500, 524288},
		{"ns-full", "short", "performance", 2000, 2097152},
		{"ns-full", "medium", "cost", 400, 419430},
		{"ns-full", "medium", "performance", 1500, 1572864},
		{"ns-full", "long", "cost", 300, 314572},
		{"ns-full", "long", "performance", 1200, 1258291},
		{"ns-partial", "short", "performance", 2500, 2621440},
		{"ns-partial", "medium", "performance", 2400, 2516582},
	}
	start := map[string]string{
		"short":  "2024-01-14T00:00:00Z",
		"medium": "2024-01-08T00:00:00Z",
		"long":   "2023-12-31T00:00:00Z",
	}
	const end = "2024-01-15T00:00:00Z"
	for _, s := range specs {
		nsID := model.NativeNamespaceID(clusterUUID, s.ns)
		codes := "{}"
		if s.ns == "ns-full" && s.term == "short" && s.engine == "cost" {
			codes = "{1,77}"
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO namespace_recommendation_sets (
				org_id, cluster_uuid, namespace_name, namespace_id, term, engine,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				current_cpu_request_millicores, current_cpu_limit_millicores,
				current_memory_request_kib, current_memory_limit_kib,
				monitoring_start_time, monitoring_end_time, notification_codes,
				confidence_level, updated_at
			) VALUES ($1, $2::uuid, $3, $4::uuid, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
				$15::timestamptz, $16::timestamptz, $17, $18, now())`,
			orgID, clusterUUID, s.ns, nsID, s.term, s.engine,
			s.recCPU, s.recCPU+1000,
			s.recMem, s.recMem+524288,
			1000, 2000,
			1048576, 2097152,
			start[s.term], end, codes,
			0.92,
		)
		require.NoError(t, err)
	}
}

// TestNamespaceRecommendationSetsCompatSynthesis is the #599 Phase 5 RED
// contract for the pure-compat (Kruize-mode) list path: native namespace
// rows must be served by synthesizing legacy blobs. Pre-fix the compat
// query JOINs the empty workloads table, so zero rows surface for native
// data (native rows reference clusters, not workloads).
func TestNamespaceRecommendationSetsCompatSynthesis(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	seedNativeCompatNamespaces(t, pool)

	before := promtestutil.ToFloat64(metrics.CompatCollapseSkipped.WithLabelValues("namespace"))

	var ns model.NamespaceRecommendationSet
	results, count, err := ns.GetNamespaceRecommendationSets(
		testutil.TestOrgID,
		listoptions.ListOptions{Limit: 10, OrderBy: listoptions.DefaultNsRecsDBColumn, OrderHow: listoptions.OrderDesc},
		nil,
		map[string][]string{"*": {}},
	)
	require.NoError(t, err)
	require.Equal(t, 1, count, "collapse count must cover namespaces with a pinned short/cost row")
	require.Len(t, results, 1, "one full sextet must surface; pre-fix JOIN workloads yields none")

	after := promtestutil.ToFloat64(metrics.CompatCollapseSkipped.WithLabelValues("namespace"))
	assert.Equal(t, 1.0, after-before, "pinned-less namespace must be skipped and counted")

	res := results[0]
	assert.Equal(t, "ns-full", res.Project)
	assert.Equal(t, testutil.TestClusterUUID, res.ClusterUUID)
	assert.Equal(t, model.NativeNamespaceID(testutil.TestClusterUUID, "ns-full"), res.ID,
		"compat list must serve namespace_id as id")

	var blob map[string]interface{}
	require.NoError(t, json.Unmarshal(res.Recommendations, &blob))
	assert.Equal(t, "2024-01-15T00:00:00.000Z", blob["monitoring_end_time"],
		"end_time must come from the row column")

	current := blob["current"].(map[string]interface{})["requests"].(map[string]interface{})
	assert.InDelta(t, 1.0, current["cpu"].(map[string]interface{})["amount"], 1e-9,
		"current must come from the short/cost row columns")

	terms := blob["recommendation_terms"].(map[string]interface{})
	for _, key := range []string{"short_term", "medium_term", "long_term"} {
		assert.Contains(t, terms, key, "_term keys expected after normalizeTerm")
	}
	cost := terms["short_term"].(map[string]interface{})["recommendation_engines"].(map[string]interface{})["cost"].(map[string]interface{})
	cfg := cost["config"].(map[string]interface{})["requests"].(map[string]interface{})
	assert.InDelta(t, 0.5, cfg["cpu"].(map[string]interface{})["amount"], 1e-9)
	variation := cost["variation"].(map[string]interface{})["requests"].(map[string]interface{})
	assert.InDelta(t, -0.5, variation["cpu"].(map[string]interface{})["amount"], 1e-9)
	notifs := cost["notifications"].(map[string]interface{})
	assert.Contains(t, notifs, "1")
	assert.Contains(t, notifs, "77")
}

// TestNamespaceRecommendationSetCompatDetailByNamespaceID is the #599 Phase 5
// RED contract for the pure-compat detail path: resolution must be
// namespace_id-first (native rows serve namespace_id as the public id), and
// the pinned (short/cost) row must be returned with the blob still empty —
// the handler synthesizes from siblings. Pre-fix the model filters on the
// numeric primary key, so a namespace_id lookup raises a uuid cast error
// instead of resolving.
func TestNamespaceRecommendationSetCompatDetailByNamespaceID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	seedNativeCompatNamespaces(t, pool)

	nsID := model.NativeNamespaceID(testutil.TestClusterUUID, "ns-full")
	var ns model.NamespaceRecommendationSet
	res, err := ns.GetNamespaceRecommendationSetByID(
		testutil.TestOrgID,
		nsID,
		map[string][]string{"*": {}},
	)
	require.NoError(t, err)
	assert.Equal(t, "ns-full", res.Project)
	assert.Equal(t, nsID, res.ID, "detail must serve namespace_id as id")
	assert.Equal(t, testutil.TestClusterUUID, res.ClusterUUID)
	assert.Equal(t, "short", res.SynthDBRow.Term, "detail must pin the short/cost row")
	assert.Equal(t, "cost", res.SynthDBRow.Engine)
	assert.Empty(t, res.Recommendations, "native rows carry no stored blob; handler synthesizes")
}

// TestNamespaceRecommendationSetCompatDetailPKFallback locks the legacy leg of
// the detail resolution: pre-namespace_id rows (namespace_id NULL) are served
// by their numeric primary key. Pre-fix the numeric id is compared against the
// namespace_id column (uuid cast error) or fails to match.
func TestNamespaceRecommendationSetCompatDetailPKFallback(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	seedNativeCompatNamespaces(t, pool)

	ctx := context.Background()
	var pk string
	err := pool.QueryRow(ctx, `
		INSERT INTO namespace_recommendation_sets (
			org_id, cluster_uuid, namespace_name, namespace_id, term, engine,
			rec_cpu_request_millicores, rec_memory_request_kib,
			current_cpu_request_millicores, current_memory_request_kib,
			monitoring_start_time, monitoring_end_time, notification_codes,
			updated_at
		) VALUES ($1, $2::uuid, 'ns-legacy', NULL, 'short', 'cost',
			700, 734003, 1000, 1048576,
			'2024-01-14T00:00:00Z', '2024-01-15T00:00:00Z', '{}', now())
		RETURNING id`,
		testutil.TestOrgID, testutil.TestClusterUUID,
	).Scan(&pk)
	require.NoError(t, err, "legacy row (namespace_id NULL) must insert")

	var ns model.NamespaceRecommendationSet
	res, err := ns.GetNamespaceRecommendationSetByID(
		testutil.TestOrgID,
		pk,
		map[string][]string{"*": {}},
	)
	require.NoError(t, err)
	assert.Equal(t, "ns-legacy", res.Project)
	assert.Equal(t, pk, res.ID,
		"legacy rows without namespace_id must serve their primary key as id")
}

// seedTiedNamespaceClusters inserts namespaces whose default sort key ties:
// every cluster shares the same last_reported_at, so page order is decided
// entirely by the tiebreak. Namespaces are inserted in reverse-lexicographic
// order per cluster so a stable (cluster_uuid, namespace_name) tiebreak
// visibly overrides any id/insertion-based order.
func seedTiedNamespaceClusters(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	orgID := testutil.TestOrgID

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)

	const tiedReported = "2024-02-01T00:00:00Z"
	clusters := []struct {
		uuid  string
		alias string
	}{
		{"11111111-2222-4333-8444-555555555555", "tie-a"},
		{"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "tie-b"},
		{"ffffffff-0000-4111-8222-333333333333", "tie-c"},
	}
	for _, cl := range clusters {
		_, err := pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
			VALUES (1, $1, $2, $3, 'src-tie', $4) ON CONFLICT DO NOTHING`, orgID, cl.uuid, cl.alias, tiedReported)
		require.NoError(t, err)
		for _, ns := range []string{"zulu", "mike", "alpha"} {
			nsID := model.NativeNamespaceID(cl.uuid, ns)
			_, err := pool.Exec(ctx, `
				INSERT INTO namespace_recommendation_sets (
					org_id, cluster_uuid, namespace_name, namespace_id, term, engine,
					rec_cpu_request_millicores, rec_memory_request_kib,
					current_cpu_request_millicores, current_memory_request_kib,
					monitoring_start_time, monitoring_end_time, notification_codes,
					updated_at
				) VALUES ($1, $2::uuid, $3, $4::uuid, 'short', 'cost',
					500, 524288, 1000, 1048576,
					'2024-01-14T00:00:00Z', '2024-01-15T00:00:00Z', '{}', now())`,
				orgID, cl.uuid, ns, nsID,
			)
			require.NoError(t, err)
		}
	}
}

// TestNamespaceRecommendationSetsCompatTiebreakMatchesNativeKeyset is the
// #608 RED contract: under tied sort keys (the default last_reported_at is
// one value per cluster) the compat list's element order must equal the
// native keyset order (sort key, then cluster_uuid, namespace_name).
// Pre-fix the compat tiebreak is namespace_recommendation_sets.id ASC (a
// per-row random uuid), so the tied region's order is arbitrary rather than
// (cluster_uuid, namespace_name).
func TestNamespaceRecommendationSetsCompatTiebreakMatchesNativeKeyset(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	seedTiedNamespaceClusters(t, pool)
	ctx := context.Background()
	orgID := testutil.TestOrgID

	// Native keyset order (namespace_recommendation_pagination.go):
	// sort key DESC NULLS LAST, then (cluster_uuid, namespace_name) ASC.
	rows, err := pool.Query(ctx, `
		SELECT nrs.cluster_uuid, nrs.namespace_name
		FROM namespace_recommendation_sets nrs
		JOIN clusters c ON c.cluster_uuid = nrs.cluster_uuid AND c.org_id = nrs.org_id
		WHERE nrs.org_id = $1 AND nrs.term = 'short' AND nrs.engine = 'cost'
		ORDER BY c.last_reported_at DESC NULLS LAST, nrs.cluster_uuid ASC, nrs.namespace_name ASC`,
		orgID)
	require.NoError(t, err)
	var want []string
	for rows.Next() {
		var cu, ns string
		require.NoError(t, rows.Scan(&cu, &ns))
		want = append(want, cu+"\x00"+ns)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, 9, len(want), "3 clusters x 3 namespaces, all pinned")

	var nsSet model.NamespaceRecommendationSet
	results, count, err := nsSet.GetNamespaceRecommendationSets(
		orgID,
		listoptions.ListOptions{Limit: 100, OrderBy: listoptions.DefaultNsRecsDBColumn, OrderHow: listoptions.OrderDesc},
		nil,
		map[string][]string{"*": {}},
	)
	require.NoError(t, err)
	require.Equal(t, len(want), int(count), "collapse count must cover every seeded namespace")
	require.Len(t, results, len(want))

	got := make([]string, 0, len(results))
	for _, r := range results {
		got = append(got, r.ClusterUUID+"\x00"+r.Project)
	}
	assert.Equal(t, want, got, "compat list must match native keyset order under tied sort keys")

	// Offset page walk over the tied list: zero overlap between pages,
	// stable counts, and pages concatenate to the native-ordered sequence.
	const pageSize = 2
	for off := 0; off < len(want); off += pageSize {
		paged, _, err := nsSet.GetNamespaceRecommendationSets(
			orgID,
			listoptions.ListOptions{Limit: pageSize, Offset: off, OrderBy: listoptions.DefaultNsRecsDBColumn, OrderHow: listoptions.OrderDesc},
			nil,
			map[string][]string{"*": {}},
		)
		require.NoError(t, err)
		require.Len(t, paged, min(pageSize, len(want)-off), "page %d must be full except possibly the last", off/pageSize)
		for i, r := range paged {
			assert.Equal(t, want[off+i], r.ClusterUUID+"\x00"+r.Project,
				"offset page %d row %d must slice the native-ordered sequence", off/pageSize, i)
		}
	}
}

// TestNamespaceRecommendationSetsClearsStoredPctsOnSynthesis pins #616 at
// the model seam: when the list synthesizes a blob for a row carrying stray
// legacy pcts, the returned result must carry empty pcts so every
// downstream UpdateRecommendationJSON call takes the recompute path
// (skipRequests off) instead of injecting the stale marker.
func TestNamespaceRecommendationSetsClearsStoredPctsOnSynthesis(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, org_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, $2, 'compat-pctlist-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, orgID, clusterUUID)
	require.NoError(t, err)

	nsID := model.NativeNamespaceID(clusterUUID, "ns-pctlist")
	for _, te := range [][2]string{
		{"short", "cost"}, {"short", "performance"},
		{"medium", "cost"}, {"medium", "performance"},
		{"long", "cost"}, {"long", "performance"},
	} {
		cpuPct := `NULL`
		memPct := `NULL`
		if te[0] == "short" && te[1] == "cost" {
			cpuPct = `9999.0`
			memPct = `9999.0`
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO namespace_recommendation_sets (
				org_id, cluster_uuid, namespace_name, namespace_id, term, engine,
				rec_cpu_request_millicores, rec_memory_request_kib,
				current_cpu_request_millicores, current_memory_request_kib,
				monitoring_start_time, monitoring_end_time, notification_codes,
				cpu_variation_short_cost_pct, memory_variation_short_cost_pct, updated_at
			) VALUES ($1, $2::uuid, 'ns-pctlist', $3::uuid, $4, $5,
				500, 524288, 1000, 1048576,
				'2024-01-14T00:00:00Z', '2024-01-15T00:00:00Z', '{}', `+cpuPct+`, `+memPct+`, now())`,
			orgID, clusterUUID, nsID, te[0], te[1])
		require.NoError(t, err)
	}

	var ns model.NamespaceRecommendationSet
	results, _, err := ns.GetNamespaceRecommendationSets(
		orgID,
		listoptions.ListOptions{Limit: 10, OrderBy: listoptions.DefaultNsRecsDBColumn, OrderHow: listoptions.OrderDesc},
		nil,
		map[string][]string{"*": {}},
	)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.False(t, results[0].StoredVariationPcts.HasValues(),
		"synthesized rows must carry empty pcts (stray 9999 marker must not survive)")

	var blob map[string]interface{}
	require.NoError(t, json.Unmarshal(results[0].Recommendations, &blob))
	terms, _ := blob["recommendation_terms"].(map[string]interface{})
	short, _ := terms["short_term"].(map[string]interface{})
	cost, _ := short["recommendation_engines"].(map[string]interface{})["cost"].(map[string]interface{})
	_, hasVariation := cost["variation"]
	assert.True(t, hasVariation, "synthesized blob must carry absolute-delta variation for the reader recompute")
}
