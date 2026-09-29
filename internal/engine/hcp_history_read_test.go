package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

func hcpHistReadOpts() listoptions.ListOptions {
	return listoptions.ListOptions{Limit: 10, OrderBy: "h.recorded_at", OrderHow: listoptions.OrderDesc}
}

// TestHistoryRead_PreMigrationOmitsHCID proves the #635 version gate: on a
// 199-schema database the history read returns 200-equivalent rows without
// the frozen-ID column instead of erroring.
func TestHistoryRead_PreMigrationOmitsHCID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsTo(t, connStr, 199)
	pool := topoTestPool(t, connStr)
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() {
		// OpenTestGORM binds a process-once stdlib pool that keeps pgx
		// connections checked out: close it before the helper-registered
		// pool.Close runs, or teardown hangs forever. Shared-pool tests
		// never close their pool, so only dedicated-container tests need this.
		if sqlDB, err := database.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		database.DB = nil
	})
	ctx := context.Background()

	require.NoError(t, pgrec.EnsureIngestClusterRow(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, topoTestCluster, time.Now().UTC()))
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_history (
			recorded_at, org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine
		) VALUES (now(), $1, $2, 'clusters-hc1', 'etcd', 'deployment', 'etcd', 'short', 'cost')`,
		topoTestOrg, topoTestCluster)
	require.NoError(t, err)

	rows, count, err := model.GetRecommendationHistory(topoTestOrg, hcpHistReadOpts(), map[string]interface{}{}, map[string][]string{})
	require.NoError(t, err, "pre-migration history read must not error")
	require.Equal(t, 1, count)
	require.Len(t, rows, 1)
	assert.Equal(t, "", rows[0].HostedClusterID, "pre-migration rows carry no association")
}

// TestHistoryRead_PostMigrationExposesHCID proves the same read exposes the
// frozen ID once migration 000200 lands.
func TestHistoryRead_PostMigrationExposesHCID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-hcp-hread"

	require.NoError(t, pgrec.EnsureIngestClusterRow(ctx, pool, orgID, "src-hread", testutil.TestClusterUUID, "hread", time.Now().UTC()))
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_history (
			recorded_at, org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine, hosted_cluster_id
		) VALUES (now(), $1, $2, 'clusters-hc1', 'etcd', 'deployment', 'etcd', 'short', 'cost', 'aaa-111')`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	rows, count, err := model.GetRecommendationHistory(orgID, hcpHistReadOpts(), map[string]interface{}{}, map[string][]string{})
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Len(t, rows, 1)
	assert.Equal(t, "aaa-111", rows[0].HostedClusterID)
}
