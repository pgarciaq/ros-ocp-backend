package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunRetentionSweep_DropsOldNamespaceSamplePartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Create a partition 8 months in the past
	old := time.Now().UTC().AddDate(0, -8, 0)
	monthStart := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("namespace_usage_samples_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF namespace_usage_samples FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err)

	// Verify partition exists
	partitions, err := listPartitions(ctx, pool, "namespace_usage_samples")
	require.NoError(t, err)
	found := false
	for _, p := range partitions {
		if p == partName {
			found = true
			break
		}
	}
	require.True(t, found, "old partition should exist before sweep")

	// Run retention with 6-month window
	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	// Verify partition was dropped
	partitions, err = listPartitions(ctx, pool, "namespace_usage_samples")
	require.NoError(t, err)
	for _, p := range partitions {
		assert.NotEqual(t, partName, p, "old partition should have been dropped")
	}
}

func TestRunRetentionSweep_KeepsRecentNamespaceSamplePartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Create a partition for the current month
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("namespace_usage_samples_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF namespace_usage_samples FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err)

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	// Verify partition was NOT dropped
	partitions, err := listPartitions(ctx, pool, "namespace_usage_samples")
	require.NoError(t, err)
	found := false
	for _, p := range partitions {
		if p == partName {
			found = true
			break
		}
	}
	assert.True(t, found, "current month partition should be kept")
}

func TestRunRetentionSweep_DropsOldHistoryPartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Create a partition 4 months in the past (within 6-month general retention
	// but outside 90-day history retention)
	old := time.Now().UTC().AddDate(0, -4, 0)
	monthStart := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("recommendation_history_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF recommendation_history FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err)

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	partitions, err := listPartitions(ctx, pool, "recommendation_history")
	require.NoError(t, err)
	for _, p := range partitions {
		assert.NotEqual(t, partName, p, "4-month-old history partition should have been dropped (90-day retention)")
	}
}

func TestRunRetentionSweep_DropsOldGPUDigestPartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	old := time.Now().UTC().AddDate(0, -8, 0)
	monthStart := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("gpu_container_digests_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF gpu_container_digests FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err)

	partitions, err := listPartitions(ctx, pool, "gpu_container_digests")
	require.NoError(t, err)
	found := false
	for _, p := range partitions {
		if p == partName {
			found = true
			break
		}
	}
	require.True(t, found, "old GPU digest partition should exist before sweep")

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	partitions, err = listPartitions(ctx, pool, "gpu_container_digests")
	require.NoError(t, err)
	for _, p := range partitions {
		assert.NotEqual(t, partName, p, "old GPU digest partition should have been dropped")
	}
}

func TestRunRetentionSweep_DropsOldDailyContainerDigestPartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	old := time.Now().UTC().AddDate(0, -8, 0)
	monthStart := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("daily_container_digests_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_container_digests FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err)

	partitions, err := listPartitions(ctx, pool, "daily_container_digests")
	require.NoError(t, err)
	found := false
	for _, p := range partitions {
		if p == partName {
			found = true
			break
		}
	}
	require.True(t, found, "old daily container digest partition should exist before sweep")

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	partitions, err = listPartitions(ctx, pool, "daily_container_digests")
	require.NoError(t, err)
	for _, p := range partitions {
		assert.NotEqual(t, partName, p, "old daily container digest partition should have been dropped")
	}
}

func TestRunRetentionSweep_PurgesStaleRecommendations(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	staleClusterUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	// Insert a stale recommendation older than 30 days
	oldDate := time.Now().UTC().AddDate(0, 0, -45)
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine, stale, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		"org-stale-test", staleClusterUUID, "ns-test", "deploy-test", "Deployment",
		"container-test", "medium", "cost", true, oldDate,
	)
	require.NoError(t, err)

	// Insert a stale recommendation that is recent (should NOT be deleted)
	recentDate := time.Now().UTC().AddDate(0, 0, -5)
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine, stale, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		"org-stale-test", staleClusterUUID, "ns-test", "deploy-test", "Deployment",
		"container-recent", "medium", "cost", true, recentDate,
	)
	require.NoError(t, err)

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	// Old stale recommendation should be deleted
	var countOld int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM recommendation_sets WHERE container_name = 'container-test' AND org_id = 'org-stale-test'",
	).Scan(&countOld)
	require.NoError(t, err)
	assert.Equal(t, 0, countOld, "old stale recommendation should be purged")

	// Recent stale recommendation should still exist
	var countRecent int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM recommendation_sets WHERE container_name = 'container-recent' AND org_id = 'org-stale-test'",
	).Scan(&countRecent)
	require.NoError(t, err)
	assert.Equal(t, 1, countRecent, "recent stale recommendation should be kept")
}

func TestRunRetentionSweep_InvalidatesFleetCacheForPurgedOrgs(t *testing.T) {
	config.ResetForTest()
	fleetsummary.ResetForTest()
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "3600")
	fleetsummary.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-stale-cache"
	staleClusterUUID := "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"

	fleetsummary.Put(orgID, false, nil, fleetsummary.CachedSummary{
		TotalContainers:     99,
		Currency:            "USD",
		TotalMonthlySavings: money.FormatUSDToAmount(0, "USD"),
	})
	_, ok := fleetsummary.Get(orgID, false, nil)
	require.True(t, ok)

	oldDate := time.Now().UTC().AddDate(0, 0, -45)
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine, stale, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		orgID, staleClusterUUID, "ns-test", "deploy-test", "Deployment",
		"container-cache", "medium", "cost", true, oldDate,
	)
	require.NoError(t, err)

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	_, ok = fleetsummary.Get(orgID, false, nil)
	assert.False(t, ok, "retention purge should invalidate fleet summary cache for affected org")
}

func TestRunRetentionSweep_PurgesOldNodeRecommendations(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	oldDate := time.Now().UTC().AddDate(0, -8, 0)
	recentDate := time.Now().UTC().AddDate(0, 0, -5)
	clusterUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	_, err := pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, updated_at)
		VALUES ($1, $2::uuid, 'old-node', 'medium', 'cost', $3),
		       ($1, $2::uuid, 'recent-node', 'medium', 'cost', $4)`,
		"org-node-retention", clusterUUID, oldDate, recentDate,
	)
	require.NoError(t, err)

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	var countOld int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM node_recommendations WHERE org_id = $1 AND node = 'old-node'",
		"org-node-retention",
	).Scan(&countOld)
	require.NoError(t, err)
	assert.Equal(t, 0, countOld, "old node recommendation should be purged")

	var countRecent int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM node_recommendations WHERE org_id = $1 AND node = 'recent-node'",
		"org-node-retention",
	).Scan(&countRecent)
	require.NoError(t, err)
	assert.Equal(t, 1, countRecent, "recent node recommendation should be kept")
}

func TestRunRetentionSweep_PurgesOldNamespaceRecommendationSets(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	oldDate := time.Now().UTC().AddDate(0, -8, 0)
	recentDate := time.Now().UTC().AddDate(0, 0, -5)
	clusterUUID := "dddddddd-eeee-ffff-0000-111111111111"

	_, err := pool.Exec(ctx, `
		INSERT INTO namespace_recommendation_sets (
			org_id, cluster_uuid, namespace_name, term, engine, schedule_type,
			rec_cpu_request_millicores, rec_cpu_limit_millicores,
			rec_memory_request_kib, rec_memory_limit_kib,
			current_cpu_request_millicores, current_cpu_limit_millicores,
			current_memory_request_kib, current_memory_limit_kib,
			variation_cpu_request_pct, variation_cpu_limit_pct,
			variation_memory_request_pct, variation_memory_limit_pct,
			confidence_level, notification_codes,
			monitoring_start_time, monitoring_end_time, updated_at
		) VALUES
			('org-ns-retention', $1::uuid, 'old-ns', 'medium', 'cost', 'all_hours',
			 1000, 2000, 1048576, 2097152, 1000, 2000, 1048576, 2097152,
			 0, 0, 0, 0, 0.9, '{}', $2, $2, $2),
			('org-ns-retention', $1::uuid, 'recent-ns', 'medium', 'cost', 'all_hours',
			 1000, 2000, 1048576, 2097152, 1000, 2000, 1048576, 2097152,
			 0, 0, 0, 0, 0.9, '{}', $3, $3, $3)`,
		clusterUUID, oldDate, recentDate,
	)
	require.NoError(t, err)

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	var countOld int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM namespace_recommendation_sets WHERE org_id = $1 AND namespace_name = 'old-ns'",
		"org-ns-retention",
	).Scan(&countOld)
	require.NoError(t, err)
	assert.Equal(t, 0, countOld, "old namespace recommendation set should be purged")

	var countRecent int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM namespace_recommendation_sets WHERE org_id = $1 AND namespace_name = 'recent-ns'",
		"org-ns-retention",
	).Scan(&countRecent)
	require.NoError(t, err)
	assert.Equal(t, 1, countRecent, "recent namespace recommendation set should be kept")
}

func TestRunRetentionSweep_PurgesOldPVCRecommendationSets(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	oldDate := time.Now().UTC().AddDate(0, -8, 0)
	recentDate := time.Now().UTC().AddDate(0, 0, -5)
	clusterUUID := "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"

	_, err := pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, updated_at
		) VALUES
			('org-pvc-retention', $1::uuid, 'ns', 'old-pvc', $2),
			('org-pvc-retention', $1::uuid, 'ns', 'recent-pvc', $3)`,
		clusterUUID, oldDate, recentDate,
	)
	require.NoError(t, err)

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	var countOld int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM pvc_recommendation_sets WHERE org_id = $1 AND persistentvolumeclaim = 'old-pvc'",
		"org-pvc-retention",
	).Scan(&countOld)
	require.NoError(t, err)
	assert.Equal(t, 0, countOld, "old PVC recommendation set should be purged")

	var countRecent int
	err = pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM pvc_recommendation_sets WHERE org_id = $1 AND persistentvolumeclaim = 'recent-pvc'",
		"org-pvc-retention",
	).Scan(&countRecent)
	require.NoError(t, err)
	assert.Equal(t, 1, countRecent, "recent PVC recommendation set should be kept")
}

func TestRunRetentionSweep_InvalidatesFleetCacheForPurgedNodeRecommendations(t *testing.T) {
	config.ResetForTest()
	fleetsummary.ResetForTest()
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "3600")
	fleetsummary.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-node-cache"
	clusterUUID := "cccccccc-dddd-eeee-ffff-000000000001"
	oldDate := time.Now().UTC().AddDate(0, -8, 0)

	fleetsummary.Put(orgID, false, nil, fleetsummary.CachedSummary{
		TotalContainers:     42,
		Currency:            "USD",
		TotalMonthlySavings: money.FormatUSDToAmount(0, "USD"),
	})
	_, ok := fleetsummary.Get(orgID, false, nil)
	require.True(t, ok)

	_, err := pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, updated_at)
		VALUES ($1, $2::uuid, 'cache-node', 'medium', 'cost', $3)`,
		orgID, clusterUUID, oldDate,
	)
	require.NoError(t, err)

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	_, ok = fleetsummary.Get(orgID, false, nil)
	assert.False(t, ok, "node recommendation purge should invalidate fleet summary cache for affected org")
}

func TestExtractYearMonth(t *testing.T) {
	tests := []struct {
		partName    string
		parentTable string
		expected    string
	}{
		{"namespace_usage_samples_202603", "namespace_usage_samples", "202603"},
		{"container_usage_samples_202601", "container_usage_samples", "202601"},
		{"daily_namespace_digests_202605", "daily_namespace_digests", "202605"},
		{"daily_container_digests_202604", "daily_container_digests", "202604"},
		{"gpu_container_digests_202607", "gpu_container_digests", "202607"},
		{"unrelated_table_202603", "namespace_usage_samples", ""},
		{"namespace_usage_samples_2026", "namespace_usage_samples", ""},
	}

	for _, tt := range tests {
		t.Run(tt.partName, func(t *testing.T) {
			got := extractYearMonth(tt.partName, tt.parentTable)
			assert.Equal(t, tt.expected, got)
		})
	}
}
