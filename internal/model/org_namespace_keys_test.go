package model_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const testOrgNamespaceKeysOrg = "org-namespace-keys-test"

func insertNamespaceRecommendationSetRow(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, namespaceName, term, engine, scheduleType string,
	updatedAt time.Time,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO namespace_recommendation_sets (
			org_id, cluster_uuid, namespace_name, term, engine,
			schedule_type, updated_at,
			monitoring_start_time, monitoring_end_time, recommendations
		) VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $7, $7, '{}'::jsonb)`,
		orgID, clusterUUID, namespaceName, term, engine, scheduleType, updatedAt,
	)
	require.NoError(t, err)
}

func TestRefreshOrgNamespaceKeys_InsertsNewKeys(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgNamespaceKeysOrg + "-insert"
	clusterUUID := testutil.TestClusterUUID
	updatedAt := time.Now().UTC().Add(-2 * time.Hour)

	insertNamespaceRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-alpha", "short", "cost", "all_hours", updatedAt)

	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	var count int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_namespace_keys WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var namespaceName string
	err = pool.QueryRow(ctx, `
		SELECT namespace_name FROM org_namespace_keys
		WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID,
	).Scan(&namespaceName)
	require.NoError(t, err)
	assert.Equal(t, "ns-alpha", namespaceName)
}

func TestRefreshOrgNamespaceKeys_IncludesClusterUUIDInPK(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgNamespaceKeysOrg + "-pk"
	cluster1 := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	cluster2 := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	now := time.Now().UTC()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (900, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (900, $1, 'cluster-a', 'src-a', now()), (900, $2, 'cluster-b', 'src-b', now()) ON CONFLICT DO NOTHING`,
		cluster1, cluster2)
	require.NoError(t, err)

	insertNamespaceRecommendationSetRow(t, ctx, pool, orgID, cluster1, "production", "short", "cost", "all_hours", now)
	insertNamespaceRecommendationSetRow(t, ctx, pool, orgID, cluster2, "production", "short", "cost", "all_hours", now)

	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_namespace_keys WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "same namespace in two clusters should produce two keys")
}

func TestRefreshOrgNamespaceKeys_RemovesStaleKeys(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgNamespaceKeysOrg + "-remove"
	clusterUUID := testutil.TestClusterUUID
	now := time.Now().UTC()

	insertNamespaceRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-alpha", "short", "cost", "all_hours", now)
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	// Remove all recommendation rows for this namespace (simulating stale data).
	_, err := pool.Exec(ctx, `
		DELETE FROM namespace_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace_name = $3`,
		orgID, clusterUUID, "ns-alpha",
	)
	require.NoError(t, err)

	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_namespace_keys WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRefreshOrgNamespaceKeys_IgnoresNullTerm(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgNamespaceKeysOrg + "-nullterm"
	clusterUUID := testutil.TestClusterUUID
	now := time.Now().UTC()

	// Insert a row with NULL term (should be excluded from keys).
	_, err := pool.Exec(ctx, `
		INSERT INTO namespace_recommendation_sets (
			org_id, cluster_uuid, namespace_name, term, engine,
			schedule_type, updated_at,
			monitoring_start_time, monitoring_end_time, recommendations
		) VALUES ($1, $2::uuid, $3, NULL, NULL, 'all_hours', $4, $4, $4, '{}'::jsonb)`,
		orgID, clusterUUID, "ns-pending", now,
	)
	require.NoError(t, err)

	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_namespace_keys WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "rows with NULL term should not produce keys")
}

func TestRefreshOrgNamespaceKeys_UpdatesLastReported(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgNamespaceKeysOrg + "-updated"
	clusterUUID := testutil.TestClusterUUID
	older := time.Now().UTC().Add(-48 * time.Hour)
	newer := time.Now().UTC().Add(-1 * time.Hour)

	insertNamespaceRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-alpha", "short", "cost", "all_hours", older)
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	insertNamespaceRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-alpha", "medium", "cost", "all_hours", newer)
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	var lastReported time.Time
	err := pool.QueryRow(ctx, `
		SELECT last_reported FROM org_namespace_keys
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace_name = $3`,
		orgID, clusterUUID, "ns-alpha",
	).Scan(&lastReported)
	require.NoError(t, err)
	assert.WithinDuration(t, newer, lastReported, time.Second)
}

func TestRefreshOrgNamespaceKeys_PreservesResolvedTags(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgNamespaceKeysOrg + "-tags"
	clusterUUID := testutil.TestClusterUUID
	now := time.Now().UTC()

	insertNamespaceRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-alpha", "short", "cost", "all_hours", now)
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	_, err := pool.Exec(ctx, `
		UPDATE org_namespace_keys
		SET resolved_tags = '{"env":"staging"}'::jsonb
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace_name = $3`,
		orgID, clusterUUID, "ns-alpha",
	)
	require.NoError(t, err)

	// Update the existing row's updated_at to trigger a last_reported refresh.
	_, err = pool.Exec(ctx, `
		UPDATE namespace_recommendation_sets SET updated_at = $1
		WHERE org_id = $2 AND cluster_uuid = $3::uuid AND namespace_name = $4`,
		now.Add(time.Minute), orgID, clusterUUID, "ns-alpha",
	)
	require.NoError(t, err)
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	var tags string
	err = pool.QueryRow(ctx, `
		SELECT resolved_tags::text FROM org_namespace_keys
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace_name = $3`,
		orgID, clusterUUID, "ns-alpha",
	).Scan(&tags)
	require.NoError(t, err)
	assert.Equal(t, `{"env": "staging"}`, tags)
}

func TestRefreshOrgNamespaceKeys_ExcludesStaleRows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgNamespaceKeysOrg + "-stale"
	clusterUUID := testutil.TestClusterUUID
	now := time.Now().UTC()

	// Insert a non-stale row (should be included).
	insertNamespaceRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-fresh", "short", "cost", "all_hours", now)

	// Insert a stale row (should be excluded).
	_, err := pool.Exec(ctx, `
		INSERT INTO namespace_recommendation_sets (
			org_id, cluster_uuid, namespace_name, term, engine,
			schedule_type, updated_at, stale,
			monitoring_start_time, monitoring_end_time, recommendations
		) VALUES ($1, $2::uuid, $3, 'short', 'cost', 'all_hours', $4, true, $4, $4, '{}'::jsonb)`,
		orgID, clusterUUID, "ns-stale", now,
	)
	require.NoError(t, err)

	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_namespace_keys WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "only non-stale rows should produce keys")

	var namespaceName string
	err = pool.QueryRow(ctx, `
		SELECT namespace_name FROM org_namespace_keys WHERE org_id = $1`, orgID).Scan(&namespaceName)
	require.NoError(t, err)
	assert.Equal(t, "ns-fresh", namespaceName)
}

func TestRefreshOrgNamespaceKeys_DeletesKeyWhenBecomesStale(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgNamespaceKeysOrg + "-becomes-stale"
	clusterUUID := testutil.TestClusterUUID
	now := time.Now().UTC()

	insertNamespaceRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-alpha", "short", "cost", "all_hours", now)
	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	var count int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_namespace_keys WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Mark the namespace as stale.
	_, err = pool.Exec(ctx, `
		UPDATE namespace_recommendation_sets SET stale = true
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace_name = $3`,
		orgID, clusterUUID, "ns-alpha",
	)
	require.NoError(t, err)

	require.NoError(t, model.RefreshOrgNamespaceKeys(ctx, pool, orgID))

	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_namespace_keys WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "stale namespace should be removed from keys")
}

func TestRefreshOrgNamespaceKeys_EmptyOrgID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	err := model.RefreshOrgNamespaceKeys(ctx, pool, "")
	assert.NoError(t, err, "empty orgID should be a no-op")
}
