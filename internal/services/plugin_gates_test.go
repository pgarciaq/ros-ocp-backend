package services

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// Generation gates (#591): running with the plugin disabled must return
// nil before touching the DB (no pool, no queries). These tests are
// DB-free by construction: the guard sits on the first line.
func TestRunStorageRecommendations_DisabledPluginSkips(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "pvc")

	msg := types.KafkaMsg{}
	msg.Metadata.Org_id = "org-gate-test"
	msg.Metadata.Cluster_uuid = "00000000-0000-0000-0000-000000000001"
	require.NoError(t, runStorageRecommendations(context.Background(), msg),
		"disabled pvc plugin must skip generation without error (and without DB)")
}

func TestRunNodeRecommendations_DisabledPluginSkips(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "node")

	// Nil pool/appCfg/costData: the guard returns before any dereference.
	require.NoError(t, runNodeRecommendations(context.Background(), nil, "org-gate-test",
		"00000000-0000-0000-0000-000000000001", time.Now(), time.Now(), nil, nil),
		"disabled node plugin must skip generation without error (and without DB)")
}

func TestRunSnapshotRecommendations_DisabledPluginSkips(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "snapshot")

	msg := types.KafkaMsg{}
	msg.Metadata.Org_id = "org-gate-test"
	msg.Metadata.Cluster_uuid = "00000000-0000-0000-0000-000000000001"
	require.NoError(t, runSnapshotRecommendations(context.Background(), msg),
		"disabled snapshot plugin must skip generation without error (and without DB)")
}

// seedOversizedPVCDigests writes 10 daily buckets at ~10% utilization: the
// enabled path must produce an oversized rec (gauge), the disabled path
// must write nothing.
func seedOversizedPVCDigests(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, ns, pvc string) {
	t.Helper()
	now := time.Now().UTC()
	testutil.EnsureMonthlyPartition(t, pool, "daily_pvc_digests", now)
	testutil.EnsureMonthlyPartition(t, pool, "daily_pvc_digests", now.AddDate(0, 0, -10))
	for d := 10; d >= 1; d-- {
		day := now.AddDate(0, 0, -d).Format("2006-01-02")
		_, err := pool.Exec(ctx, `INSERT INTO daily_pvc_digests
			(bucket_date, org_id, cluster_uuid, namespace, persistentvolumeclaim, storageclass,
			 capacity_bytes, request_bytes, usage_bytes_min, usage_bytes_max, usage_bytes_avg, sample_count)
			VALUES ($1, $2, $3, $4, $5, 'standard',
			 107374182400, 107374182400, 9663676416, 11811160064, 10737418240, 96)`,
			day, orgID, clusterUUID, ns, pvc)
		require.NoError(t, err)
	}
}

func pvcRecCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pvc_recommendation_sets WHERE org_id = $1`, orgID).Scan(&n))
	return n
}

// TestRunStorageRecommendations_DisabledPluginWritesNothing is the behavioral
// proof the DB-free contract test cannot give (the enabled path succeeds
// vacuously without digests): with seeded digests, enabled writes recs
// (gauge — fails if the fixture is wrong), disabled writes none.
func TestRunStorageRecommendations_DisabledPluginWritesNothing(t *testing.T) {
	config.ResetForTest()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID

	msg := types.KafkaMsg{}
	msg.Metadata.Org_id = orgID
	msg.Metadata.Cluster_uuid = clusterUUID

	// Gauge: enabled path must produce recs from the fixture.
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	seedOversizedPVCDigests(t, ctx, pool, orgID, clusterUUID, "gate-ns", "gate-pvc")
	require.NoError(t, runStorageRecommendations(ctx, msg))
	require.NotZero(t, pvcRecCount(t, ctx, pool, orgID),
		"gauge: enabled path must write recs from seeded digests")

	// Test: disabled path writes nothing (fresh org isolates the runs).
	disabledOrg := orgID + "-disabled"
	seedOversizedPVCDigests(t, ctx, pool, disabledOrg, clusterUUID, "gate-ns-disabled", "gate-pvc-disabled")
	msg.Metadata.Org_id = disabledOrg
	// GetConfig caches: re-reset so the new env takes effect.
	config.ResetForTest()
	t.Setenv("ROS_DISABLED_PLUGINS", "pvc")
	require.NoError(t, runStorageRecommendations(ctx, msg))
	assert.Zero(t, pvcRecCount(t, ctx, pool, disabledOrg),
		"disabled pvc plugin must write no recommendations")
}
