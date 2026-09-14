package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// Regression test for #526: the pre-000187 GPU digest indexes must be gone.
// pg_stat showed zero scans on both across a full ingest/API/rec workload
// while idx_gpu_container_digests_org_cluster_sched_start (000187) and the
// natural key serve all tenant-scoped reads. DROP INDEX on the partitioned
// parent removes the per-partition children too.
func TestMigrationDropsUnusedGPUDigestIndexes(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	var count int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_indexes
		WHERE indexname IN (
			'idx_ros_gpu_digest_cluster_interval',
			'idx_gpu_digest_cluster_interval_node'
		)`,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "unused pre-000187 GPU digest indexes must be dropped (see #526)")

	var successor int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_indexes
		WHERE indexname = 'idx_gpu_container_digests_org_cluster_sched_start'`,
	).Scan(&successor)
	require.NoError(t, err)
	assert.Equal(t, 1, successor, "000187 successor index must remain")
}
