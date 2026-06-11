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
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
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
	page1, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}})
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

	page2, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}})
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
		page, err := model.GetNativeNamespaceRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}})
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
