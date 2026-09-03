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

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
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

	// Minimal schemas matching production PRIMARY KEY columns used by cleanupClusterAnalytics
	// (PK-batched DELETE references full composite keys — see sourcesCleaner.go).
	ddls := []string{
		`CREATE TABLE daily_container_digests (
			bucket_date DATE NOT NULL,
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			namespace TEXT NOT NULL,
			workload TEXT NOT NULL,
			workload_type TEXT NOT NULL DEFAULT 'Deployment',
			container_name TEXT NOT NULL,
			PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type, container_name, bucket_date)
		)`,
		`CREATE TABLE daily_namespace_digests (
			bucket_date DATE NOT NULL,
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			namespace TEXT NOT NULL,
			PRIMARY KEY (org_id, cluster_uuid, namespace, bucket_date)
		)`,
		`CREATE TABLE daily_pvc_digests (
			id BIGSERIAL,
			bucket_date DATE NOT NULL,
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			namespace TEXT NOT NULL,
			persistentvolumeclaim TEXT NOT NULL,
			PRIMARY KEY (id, bucket_date)
		)`,
		`CREATE TABLE daily_node_digests (
			bucket_date DATE NOT NULL,
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			node TEXT NOT NULL,
			schedule_type TEXT NOT NULL DEFAULT 'all_hours',
			PRIMARY KEY (org_id, cluster_uuid, node, bucket_date, schedule_type)
		)`,
		`CREATE TABLE gpu_container_digests (
			id BIGSERIAL,
			interval_start TIMESTAMP NOT NULL,
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			namespace TEXT NOT NULL,
			workload TEXT NOT NULL,
			container_name TEXT NOT NULL,
			PRIMARY KEY (id, interval_start)
		)`,
		`CREATE TABLE recommendation_quality (
			measured_at TIMESTAMPTZ NOT NULL,
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			namespace TEXT NOT NULL,
			workload TEXT NOT NULL,
			workload_type TEXT NOT NULL DEFAULT 'Deployment',
			container_name TEXT NOT NULL,
			PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type, container_name, measured_at)
		)`,
		`CREATE TABLE recommendation_history (
			recorded_at TIMESTAMPTZ NOT NULL,
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			namespace TEXT NOT NULL,
			workload TEXT NOT NULL,
			workload_type TEXT NOT NULL DEFAULT 'Deployment',
			container_name TEXT NOT NULL,
			term TEXT NOT NULL,
			engine TEXT NOT NULL,
			PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, recorded_at)
		)`,
		`CREATE TABLE pvc_recommendation_sets (
			id BIGSERIAL PRIMARY KEY,
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			namespace TEXT NOT NULL DEFAULT 'ns',
			persistentvolumeclaim TEXT NOT NULL DEFAULT 'pvc'
		)`,
		`CREATE TABLE recommendation_sets (
			org_id TEXT NOT NULL,
			cluster_uuid TEXT NOT NULL,
			namespace TEXT NOT NULL,
			workload TEXT NOT NULL,
			workload_type TEXT NOT NULL DEFAULT 'Deployment',
			container_name TEXT NOT NULL,
			term TEXT NOT NULL DEFAULT 'short',
			engine TEXT NOT NULL DEFAULT 'cost',
			PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine)
		)`,
		`CREATE TABLE snapshot_inventory (
			id BIGSERIAL PRIMARY KEY,
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			namespace TEXT NOT NULL DEFAULT 'ns',
			snapshot_name TEXT NOT NULL DEFAULT 'snap',
			creation_timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE snapshot_recommendation_sets (
			id BIGSERIAL PRIMARY KEY,
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			namespace TEXT NOT NULL DEFAULT 'ns',
			snapshot_name TEXT NOT NULL DEFAULT 'snap',
			creation_timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE node_recommendations (
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			node TEXT NOT NULL,
			term TEXT NOT NULL DEFAULT 'medium',
			engine TEXT NOT NULL DEFAULT 'cost',
			PRIMARY KEY (org_id, cluster_uuid, node, term, engine)
		)`,
		`CREATE TABLE node_gpu_timeslicing_recommendations (
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			node_name TEXT NOT NULL,
			gpu_model TEXT NOT NULL DEFAULT '',
			term TEXT NOT NULL,
			recommended_replicas INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (org_id, cluster_uuid, node_name, gpu_model, term)
		)`,
		`CREATE TABLE node_gpu_timeslicing_recommendation_history (
			id BIGSERIAL PRIMARY KEY,
			org_id TEXT NOT NULL,
			cluster_uuid UUID NOT NULL,
			node_name TEXT NOT NULL,
			gpu_model TEXT NOT NULL DEFAULT '',
			term TEXT NOT NULL,
			recommended_replicas INTEGER NOT NULL DEFAULT 1,
			confidence REAL NOT NULL DEFAULT 0,
			candidate_count INTEGER NOT NULL DEFAULT 0,
			impacted_count INTEGER NOT NULL DEFAULT 0
		)`,
		// Kruize-era tables referenced by the background-delete steps.
		`CREATE TABLE clusters (
			id BIGSERIAL PRIMARY KEY,
			cluster_uuid UUID NOT NULL UNIQUE,
			cluster_alias TEXT,
			source_id TEXT,
			tenant_id BIGINT
		)`,
		`CREATE TABLE workloads (
			id BIGSERIAL PRIMARY KEY,
			cluster_id BIGINT REFERENCES clusters(id) ON DELETE CASCADE,
			namespace TEXT,
			workload_name TEXT,
			workload_type TEXT,
			experiment_name TEXT
		)`,
		`CREATE TABLE workload_metrics (
			id BIGSERIAL PRIMARY KEY,
			workload_id BIGINT REFERENCES workloads(id) ON DELETE CASCADE,
			metric_name TEXT
		)`,
		`CREATE TABLE historical_recommendation_sets (
			id BIGSERIAL PRIMARY KEY,
			workload_id BIGINT REFERENCES workloads(id) ON DELETE CASCADE,
			recommendation TEXT
		)`,
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
	const testDigestDay = "2026-01-01"
	exec(`INSERT INTO daily_container_digests (bucket_date, org_id, cluster_uuid, namespace, workload, workload_type, container_name) VALUES (?::date, ?, ?::uuid, 'ns', 'wl', 'Deployment', 'ctr')`, testDigestDay, org, cluster)
	exec(`INSERT INTO daily_namespace_digests (bucket_date, org_id, cluster_uuid, namespace) VALUES (?::date, ?, ?::uuid, 'ns')`, testDigestDay, org, cluster)
	exec(`INSERT INTO daily_pvc_digests (bucket_date, org_id, cluster_uuid, namespace, persistentvolumeclaim) VALUES (?::date, ?, ?::uuid, 'ns', 'pvc')`, testDigestDay, org, cluster)
	exec(`INSERT INTO daily_node_digests (bucket_date, org_id, cluster_uuid, node) VALUES (?::date, ?, ?::uuid, 'node1')`, testDigestDay, org, cluster)
	exec(`INSERT INTO gpu_container_digests (interval_start, org_id, cluster_uuid, namespace, workload, container_name) VALUES (?::timestamp, ?, ?::uuid, 'ns', 'wl', 'ctr')`, testDigestDay+" 00:00:00", org, cluster)
	exec(`INSERT INTO recommendation_quality (measured_at, org_id, cluster_uuid, namespace, workload, workload_type, container_name) VALUES (?::timestamptz, ?, ?::uuid, 'ns', 'wl', 'Deployment', 'ctr')`, testDigestDay+" 00:00:00Z", org, cluster)
	exec(`INSERT INTO recommendation_history (recorded_at, org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine) VALUES (?::timestamptz, ?, ?::uuid, 'ns', 'wl', 'Deployment', 'ctr', 'short', 'cost')`, testDigestDay+" 00:00:00Z", org, cluster)
	exec(`INSERT INTO pvc_recommendation_sets (org_id, cluster_uuid) VALUES (?, ?::uuid)`, org, cluster)
	exec(`INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine) VALUES (?, ?, 'ns', 'wl', 'Deployment', 'ctr', 'short', 'cost')`, org, cluster)
	exec(`INSERT INTO snapshot_inventory (org_id, cluster_uuid) VALUES (?, ?::uuid)`, org, cluster)
	exec(`INSERT INTO snapshot_recommendation_sets (org_id, cluster_uuid) VALUES (?, ?::uuid)`, org, cluster)
	exec(`INSERT INTO node_recommendations (org_id, cluster_uuid, node, term) VALUES (?, ?::uuid, 'node1', 'medium')`, org, cluster)
	// Kruize-era tables
	exec(`INSERT INTO clusters (cluster_uuid, cluster_alias, source_id, tenant_id) VALUES (?::uuid, 'test', 'src-1', 1)`, cluster)
	exec(`INSERT INTO workloads (cluster_id, namespace, workload_name, workload_type, experiment_name) VALUES ((SELECT id FROM clusters WHERE cluster_uuid = ?::uuid), 'ns', 'wl', 'deployment', 'exp-1')`, cluster)
	exec(`INSERT INTO workload_metrics (workload_id, metric_name) VALUES ((SELECT id FROM workloads WHERE cluster_id = (SELECT id FROM clusters WHERE cluster_uuid = ?::uuid) LIMIT 1), 'cpuUsage')`, cluster)
	exec(`INSERT INTO historical_recommendation_sets (workload_id, recommendation) VALUES ((SELECT id FROM workloads WHERE cluster_id = (SELECT id FROM clusters WHERE cluster_uuid = ?::uuid) LIMIT 1), '{}')`, cluster)

	require.NoError(t, cleanupClusterAnalytics(gdb, org, cluster))

	assert.Equal(t, int64(0), countRows(t, gdb, "daily_container_digests", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "daily_namespace_digests", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "daily_pvc_digests", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "daily_node_digests", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "gpu_container_digests", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "recommendation_quality", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "recommendation_history", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "pvc_recommendation_sets", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "recommendation_sets", "org_id = ? AND cluster_uuid = ?", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "snapshot_inventory", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "snapshot_recommendation_sets", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "node_recommendations", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "workload_metrics", "1=1"))
	assert.Equal(t, int64(0), countRows(t, gdb, "historical_recommendation_sets", "1=1"))
	assert.Equal(t, int64(0), countRows(t, gdb, "workloads", "cluster_id = (SELECT id FROM clusters WHERE cluster_uuid = ?::uuid)", cluster))
}

func TestCleanupClusterAnalytics_BatchingDeletesLargeReplica(t *testing.T) {
	gdb, cleanup := setupAnalyticsCleanupPG(t)
	defer cleanup()

	org := "9999999"
	cluster := uuid.New().String()

	require.NoError(t, gdb.Exec(`
		INSERT INTO gpu_container_digests (interval_start, org_id, cluster_uuid, namespace, workload, container_name)
		SELECT NOW(), ?, ?::uuid, 'ns', 'wl', 'ctr' FROM generate_series(1, ?)`, org, cluster, analyticsCleanupBatchSize*2+7).Error)

	require.NoError(t, cleanupClusterAnalytics(gdb, org, cluster))

	assert.Equal(t, int64(0), countRows(t, gdb, "gpu_container_digests", "org_id = ? AND cluster_uuid = ?::uuid", org, cluster))
}

func TestCleanupClusterAnalytics_ScopedToOrgAndCluster(t *testing.T) {
	gdb, cleanup := setupAnalyticsCleanupPG(t)
	defer cleanup()

	orgKeep := "1111111"
	orgDel := "2222222"
	clusterKeep := uuid.New().String()
	clusterDel := uuid.New().String()

	require.NoError(t, gdb.Exec(
		`INSERT INTO daily_container_digests (bucket_date, org_id, cluster_uuid, namespace, workload, container_name) VALUES ('2026-01-01'::date, ?, ?::uuid, 'ns', 'wl', 'ctr')`, orgKeep, clusterKeep).Error)
	require.NoError(t, gdb.Exec(
		`INSERT INTO daily_container_digests (bucket_date, org_id, cluster_uuid, namespace, workload, container_name) VALUES ('2026-01-01'::date, ?, ?::uuid, 'ns', 'wl', 'ctr')`, orgDel, clusterDel).Error)
	sharedCluster := uuid.New().String()
	require.NoError(t, gdb.Exec(
		`INSERT INTO gpu_container_digests (interval_start, org_id, cluster_uuid, namespace, workload, container_name) VALUES ('2026-01-01 00:00:00'::timestamp, ?, ?::uuid, 'ns', 'wl', 'ctr')`, orgKeep, sharedCluster).Error)
	require.NoError(t, gdb.Exec(
		`INSERT INTO gpu_container_digests (interval_start, org_id, cluster_uuid, namespace, workload, container_name) VALUES ('2026-01-01 00:00:00'::timestamp, ?, ?::uuid, 'ns', 'wl', 'ctr')`, orgDel, sharedCluster).Error)

	require.NoError(t, cleanupClusterAnalytics(gdb, orgDel, clusterDel))
	require.NoError(t, cleanupClusterAnalytics(gdb, orgDel, sharedCluster))

	assert.Equal(t, int64(1), countRows(t, gdb, "daily_container_digests", "org_id = ? AND cluster_uuid = ?::uuid", orgKeep, clusterKeep))
	assert.Equal(t, int64(0), countRows(t, gdb, "daily_container_digests", "org_id = ? AND cluster_uuid = ?::uuid", orgDel, clusterDel))
	assert.Equal(t, int64(1), countRows(t, gdb, "gpu_container_digests", "org_id = ? AND cluster_uuid = ?::uuid", orgKeep, sharedCluster))
	assert.Equal(t, int64(0), countRows(t, gdb, "gpu_container_digests", "org_id = ? AND cluster_uuid = ?::uuid", orgDel, sharedCluster))
}

func TestCleanupClusterAnalytics_EmptyCluster_NoError(t *testing.T) {
	gdb, cleanup := setupAnalyticsCleanupPG(t)
	defer cleanup()

	require.NoError(t, cleanupClusterAnalytics(gdb, "4242424", uuid.New().String()))
}

func TestCleanupClusterAnalytics_InvalidatesFleetCache(t *testing.T) {
	config.ResetForTest()
	fleetsummary.ResetForTest()
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "3600")
	fleetsummary.ResetForTest()

	gdb, cleanup := setupAnalyticsCleanupPG(t)
	defer cleanup()

	org := "1234567"
	cluster := uuid.New().String()
	fleetsummary.Put(org, false, nil, fleetsummary.CachedSummary{
		TotalContainers:     42,
		Currency:            "USD",
		TotalMonthlySavings: money.FormatUSDToAmount(0, "USD"),
	})
	_, ok := fleetsummary.Get(org, false, nil)
	require.True(t, ok)

	require.NoError(t, cleanupClusterAnalytics(gdb, org, cluster))

	_, ok = fleetsummary.Get(org, false, nil)
	assert.False(t, ok, "sources cleanup should invalidate fleet summary cache for the org")
}
