package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const (
	testSnapshotFreshHours      = 6
	testSnapshotStaleGraceHours = 48
)

func TestReconcileSnapshotRecommendations_NoInventoryInGraceWindow_DeletesStaleRows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID
	cluster := testutil.TestClusterUUID

	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name,
			creation_timestamp, age_days, updated_at
		) VALUES ($1, $2::uuid, 'ns', 'snap1', now(), 0, now())`,
		orgID, cluster)
	require.NoError(t, err)

	// No snapshot_inventory rows within staleGraceHours → abandoned cluster path:
	// reconcile runs and removes ROS snapshot recommendations not in fresh inventory.
	removed, err := ReconcileSnapshotRecommendations(ctx, pool, orgID, cluster, testSnapshotFreshHours, testSnapshotStaleGraceHours)
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)

	var cnt int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM snapshot_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2::uuid`,
		orgID, cluster).Scan(&cnt)
	require.NoError(t, err)
	require.Equal(t, 0, cnt)
}

func TestReconcileSnapshotRecommendations_StaleButWithinGrace_NoDeletes(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID
	cluster := testutil.TestClusterUUID

	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name,
			creation_timestamp, age_days, updated_at
		) VALUES ($1, $2::uuid, 'ns', 'orphan_rec_only', now(), 0, now())`,
		orgID, cluster)
	require.NoError(t, err)

	// Inventory exists within 48h but not within the 6h fresh window → transient gap; skip.
	_, err = pool.Exec(ctx, `
		INSERT INTO snapshot_inventory (
			org_id, cluster_uuid, namespace, snapshot_name,
			creation_timestamp, ingested_at
		) VALUES ($1, $2::uuid, 'ns', 'live', now(), NOW() - INTERVAL '8 hours')`,
		orgID, cluster)
	require.NoError(t, err)

	removed, err := ReconcileSnapshotRecommendations(ctx, pool, orgID, cluster, testSnapshotFreshHours, testSnapshotStaleGraceHours)
	require.NoError(t, err)
	require.Equal(t, int64(0), removed)

	var cnt int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM snapshot_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2::uuid`,
		orgID, cluster).Scan(&cnt)
	require.NoError(t, err)
	require.Equal(t, 1, cnt)
}

func TestReconcileSnapshotRecommendations_RemovesMissingFromFreshInventory(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID
	cluster := testutil.TestClusterUUID

	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name,
			creation_timestamp, age_days, updated_at
		) VALUES
			($1, $2::uuid, 'ns', 'gone', now(), 1, now()),
			($1, $2::uuid, 'ns', 'kept', now(), 1, now())`,
		orgID, cluster)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO snapshot_inventory (
			org_id, cluster_uuid, namespace, snapshot_name,
			creation_timestamp, ingested_at
		) VALUES ($1, $2::uuid, 'ns', 'kept', now(), now())`,
		orgID, cluster)
	require.NoError(t, err)

	removed, err := ReconcileSnapshotRecommendations(ctx, pool, orgID, cluster, testSnapshotFreshHours, testSnapshotStaleGraceHours)
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)

	var names []string
	rows, err := pool.Query(ctx,
		`SELECT snapshot_name FROM snapshot_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2::uuid ORDER BY snapshot_name`,
		orgID, cluster)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		names = append(names, n)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"kept"}, names)
}
