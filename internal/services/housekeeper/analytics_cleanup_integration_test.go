package housekeeper

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupAnalyticsCleanupPG(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	ctx := context.Background()

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("housekeeper_analytics_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	gdb, err := gorm.Open(gormpostgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	ddls := []string{
		`CREATE TABLE daily_container_digests (org_id text, cluster_uuid uuid)`,
		`CREATE TABLE daily_namespace_digests (org_id text, cluster_uuid uuid)`,
		`CREATE TABLE daily_pvc_digests (org_id text, cluster_uuid uuid)`,
		`CREATE TABLE daily_node_digests (org_id text, cluster_uuid uuid)`,
		`CREATE TABLE gpu_container_digests (cluster_uuid uuid)`,
		`CREATE TABLE recommendation_sets (org_id text, cluster_uuid text)`,
		`CREATE TABLE snapshot_inventory (org_id text, cluster_uuid uuid)`,
		`CREATE TABLE snapshot_recommendation_sets (org_id text, cluster_uuid uuid)`,
		`CREATE TABLE node_recommendations (org_id text, cluster_uuid uuid)`,
	}
	for _, ddl := range ddls {
		require.NoError(t, gdb.Exec(ddl).Error)
	}

	cleanup := func() {
		_ = pgContainer.Terminate(ctx)
	}
	return gdb, cleanup
}

func countRows(t *testing.T, db *gorm.DB, table, where string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM "+table+" WHERE "+where, args...).Scan(&n).Error)
	return n
}

func TestCleanupClusterAnalytics_DeletesAllExpectedTables(t *testing.T) {
	gdb, cleanup := setupAnalyticsCleanupPG(t)
	defer cleanup()

	org := "1234567"
	cluster := uuid.New().String()

	exec := func(sql string, args ...any) {
		require.NoError(t, gdb.Exec(sql, args...).Error)
	}
	exec(`INSERT INTO daily_container_digests (org_id, cluster_uuid) VALUES (?, ?::uuid)`, org, cluster)
	exec(`INSERT INTO daily_namespace_digests (org_id, cluster_uuid) VALUES (?, ?::uuid)`, org, cluster)
	exec(`INSERT INTO daily_pvc_digests (org_id, cluster_uuid) VALUES (?, ?::uuid)`, org, cluster)
	exec(`INSERT INTO daily_node_digests (org_id, cluster_uuid) VALUES (?, ?::uuid)`, org, cluster)
	exec(`INSERT INTO gpu_container_digests (cluster_uuid) VALUES (?::uuid)`, cluster)
	exec(`INSERT INTO recommendation_sets (org_id, cluster_uuid) VALUES (?, ?)`, org, cluster)
	exec(`INSERT INTO snapshot_inventory (org_id, cluster_uuid) VALUES (?, ?::uuid)`, org, cluster)
	exec(`INSERT INTO snapshot_recommendation_sets (org_id, cluster_uuid) VALUES (?, ?::uuid)`, org, cluster)
	exec(`INSERT INTO node_recommendations (org_id, cluster_uuid) VALUES (?, ?::uuid)`, org, cluster)

	require.NoError(t, cleanupClusterAnalytics(gdb, org, cluster))

	assert.Equal(t, int64(0), countRows(t, gdb, "daily_container_digests", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "daily_namespace_digests", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "daily_pvc_digests", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "daily_node_digests", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "gpu_container_digests", "cluster_uuid = ?::uuid", cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "recommendation_sets", "org_id = ? AND cluster_uuid = ?", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "snapshot_inventory", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "snapshot_recommendation_sets", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "node_recommendations", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
}

func TestCleanupClusterAnalytics_BatchingDeletesLargeReplica(t *testing.T) {
	gdb, cleanup := setupAnalyticsCleanupPG(t)
	defer cleanup()

	org := "9999999"
	cluster := uuid.New().String()

	require.NoError(t, gdb.Exec(`
		INSERT INTO gpu_container_digests (cluster_uuid)
		SELECT ?::uuid FROM generate_series(1, ?)`, cluster, analyticsCleanupBatchSize*2+7).Error)

	require.NoError(t, cleanupClusterAnalytics(gdb, org, cluster))

	assert.Equal(t, int64(0), countRows(t, gdb, "gpu_container_digests", "cluster_uuid = ?::uuid", cluster))
}

func TestCleanupClusterAnalytics_ScopedToOrgAndCluster(t *testing.T) {
	gdb, cleanup := setupAnalyticsCleanupPG(t)
	defer cleanup()

	orgKeep := "1111111"
	orgDel := "2222222"
	clusterKeep := uuid.New().String()
	clusterDel := uuid.New().String()

	require.NoError(t, gdb.Exec(
		`INSERT INTO daily_container_digests (org_id, cluster_uuid) VALUES (?, ?::uuid)`, orgKeep, clusterKeep).Error)
	require.NoError(t, gdb.Exec(
		`INSERT INTO daily_container_digests (org_id, cluster_uuid) VALUES (?, ?::uuid)`, orgDel, clusterDel).Error)

	require.NoError(t, cleanupClusterAnalytics(gdb, orgDel, clusterDel))

	assert.Equal(t, int64(1), countRows(t, gdb, "daily_container_digests", "org_id = ? AND cluster_uuid = ?::uuid", orgKeep, clusterKeep))
	assert.Equal(t, int64(0), countRows(t, gdb, "daily_container_digests", "org_id = ? AND cluster_uuid = ?::uuid", orgDel, clusterDel))
}

func TestCleanupClusterAnalytics_EmptyCluster_NoError(t *testing.T) {
	gdb, cleanup := setupAnalyticsCleanupPG(t)
	defer cleanup()

	require.NoError(t, cleanupClusterAnalytics(gdb, "4242424", uuid.New().String()))
}
