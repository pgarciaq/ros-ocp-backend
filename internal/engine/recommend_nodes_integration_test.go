package engine_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestNodeRecommendationPipeline_Integration exercises the full node
// recommendation pipeline end-to-end with a real PostgreSQL database:
//
//  1. Seed daily_node_digests with known data
//  2. Run QueryNodeDigests to verify retrieval
//  3. Run RecommendNodes to compute recommendations
//  4. Run PersistNodeRecommendations to write results
//  5. Re-query and verify the recommendations are stored correctly
//
// This test is skipped in short mode (-short flag) since it requires Docker.
func TestNodeRecommendationPipeline_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'node-test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -30)
	end := start.AddDate(0, 0, 29)

	seedNodeDigests(t, pool, orgID, clusterUUID, start, 30)

	t.Run("QueryNodeDigests returns seeded data", func(t *testing.T) {
		digests, err := engine.QueryNodeDigests(ctx, pool, orgID, clusterUUID, start, end.AddDate(0, 0, 1))
		require.NoError(t, err)
		require.NotEmpty(t, digests)

		nodeNames := map[string]bool{}
		for _, d := range digests {
			nodeNames[d.Node] = true
		}
		assert.True(t, nodeNames["underutilized-node"], "should find underutilized-node")
		assert.True(t, nodeNames["overcommitted-node"], "should find overcommitted-node")
		assert.True(t, nodeNames["healthy-node"], "should find healthy-node")
	})

	t.Run("RecommendNodes produces correct classifications", func(t *testing.T) {
		digests, err := engine.QueryNodeDigests(ctx, pool, orgID, clusterUUID, start, end.AddDate(0, 0, 1))
		require.NoError(t, err)

		cfg := engine.NodeRecConfig{
			UnderutilThresholdBP:  engine.ThresholdToBasisPoints(0.30),
			OvercommitThresholdBP: engine.RatioToBasisPoints(1.0),
			AllocatableFactor:   0.90,
		}
		terms := []engine.TermConfig{
			{Name: "medium", WindowDays: 30, MinDataDays: 3},
		}
		nodeSettings := engine.DefaultNodeThresholdSettings()
		recs := engine.RecommendNodes(digests, cfg, nodeSettings, terms)
		require.NotEmpty(t, recs)

		recByNode := map[string]engine.NodeRec{}
		for _, r := range recs {
			if r.Engine == "cost" {
				recByNode[r.Node] = r
			}
		}

		if underutil, ok := recByNode["underutilized-node"]; ok {
			assert.Equal(t, "underutilized", underutil.Category, "underutilized-node should have category underutilized")
			assert.Contains(t, underutil.NotificationCodes, int16(11))
		} else {
			t.Error("expected underutilized-node in recommendations")
		}

		if overcommit, ok := recByNode["overcommitted-node"]; ok {
			assert.Equal(t, "overcommitted", overcommit.Category, "overcommitted-node should have category overcommitted")
			assert.Contains(t, overcommit.NotificationCodes, int16(12))
		} else {
			t.Error("expected overcommitted-node in recommendations")
		}

		if healthy, ok := recByNode["healthy-node"]; ok {
			assert.Equal(t, "optimized", healthy.Category)
			assert.Empty(t, healthy.NotificationCodes)
		} else {
			t.Error("expected healthy-node in recommendations")
		}
	})

	t.Run("PersistNodeRecommendations writes and upserts", func(t *testing.T) {
		digests, err := engine.QueryNodeDigests(ctx, pool, orgID, clusterUUID, start, end.AddDate(0, 0, 1))
		require.NoError(t, err)

		cfg := engine.NodeRecConfig{
			UnderutilThresholdBP:  engine.ThresholdToBasisPoints(0.30),
			OvercommitThresholdBP: engine.RatioToBasisPoints(1.0),
			AllocatableFactor:   0.90,
		}
		terms := []engine.TermConfig{
			{Name: "medium", WindowDays: 30, MinDataDays: 3},
		}
		nodeSettings := engine.DefaultNodeThresholdSettings()
		recs := engine.RecommendNodes(digests, cfg, nodeSettings, terms)
		require.NotEmpty(t, recs)

		validTerms := []string{"medium"}
		err = engine.PersistNodeRecommendations(ctx, pool, orgID, clusterUUID, recs, validTerms)
		require.NoError(t, err)

		var count int
		err = pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM node_recommendations WHERE org_id = $1 AND cluster_uuid = $2`,
			orgID, clusterUUID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, len(recs), count)

		var isUnderutilized bool
		err = pool.QueryRow(ctx,
			`SELECT is_underutilized FROM node_recommendations WHERE org_id = $1 AND node = $2 AND engine = 'cost'`,
			orgID, "underutilized-node").Scan(&isUnderutilized)
		require.NoError(t, err)
		assert.True(t, isUnderutilized)

		// Upsert: run again, should not fail or duplicate
		err = engine.PersistNodeRecommendations(ctx, pool, orgID, clusterUUID, recs, validTerms)
		require.NoError(t, err)

		err = pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM node_recommendations WHERE org_id = $1 AND cluster_uuid = $2`,
			orgID, clusterUUID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, len(recs), count, "upsert should not duplicate rows")

		var instanceType *string
		err = pool.QueryRow(ctx,
			`SELECT instance_type FROM node_recommendations
			 WHERE org_id = $1 AND node = $2 AND engine = 'cost' AND term = 'medium'`,
			orgID, "underutilized-node").Scan(&instanceType)
		require.NoError(t, err)
		// seedNodeDigests does not set instance_type; column should be empty/null.
		if instanceType != nil {
			assert.Empty(t, *instanceType)
		}
	})

	t.Run("PersistNodeRecommendations stores instance_type from digests", func(t *testing.T) {
		_, err := pool.Exec(ctx, `DELETE FROM node_recommendations WHERE org_id = $1 AND cluster_uuid = $2`,
			orgID, clusterUUID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `DELETE FROM daily_node_digests WHERE org_id = $1 AND cluster_uuid = $2`,
			orgID, clusterUUID)
		require.NoError(t, err)

		ensureDailyNodeDigestPartitions(t, pool, start, 4)
		for i := 0; i < 4; i++ {
			date := start.AddDate(0, 0, i)
			_, err = pool.Exec(ctx, `
				INSERT INTO daily_node_digests (
					bucket_date, org_id, cluster_uuid, node,
					cpu_usage_p50_mc, cpu_usage_p95_mc,
					mem_usage_p50_kib, mem_usage_p95_kib,
					max_cpu_allocatable_mc, max_mem_allocatable_kib,
					max_cpu_requests_mc, max_mem_requests_kib,
					max_pod_count, instance_type, sample_count
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
				ON CONFLICT (org_id, cluster_uuid, node, bucket_date) DO NOTHING`,
				date, orgID, clusterUUID, "typed-node",
				int64(800), int64(1200), int64(3200), int64(4800),
				int64(8000), int64(33554432), int64(2000), int64(8388608),
				int64(5), "m5.2xlarge", int64(24),
			)
			require.NoError(t, err)
		}

		digests, err := engine.QueryNodeDigests(ctx, pool, orgID, clusterUUID, start, start.AddDate(0, 0, 5))
		require.NoError(t, err)
		recs := engine.RecommendNodes(digests, engine.NodeRecConfig{
			UnderutilThresholdBP:  engine.ThresholdToBasisPoints(0.30),
			OvercommitThresholdBP: engine.RatioToBasisPoints(1.0),
			AllocatableFactor:   0.90,
		}, engine.DefaultNodeThresholdSettings(), []engine.TermConfig{
			{Name: "medium", WindowDays: 30, MinDataDays: 3},
		})
		require.NotEmpty(t, recs)
		require.NoError(t, engine.PersistNodeRecommendations(ctx, pool, orgID, clusterUUID, recs, []string{"medium"}))

		var stored string
		err = pool.QueryRow(ctx,
			`SELECT COALESCE(instance_type, '') FROM node_recommendations
			 WHERE org_id = $1 AND node = $2 AND engine = 'cost' AND term = 'medium'`,
			orgID, "typed-node").Scan(&stored)
		require.NoError(t, err)
		assert.Equal(t, "m5.2xlarge", stored)
	})
}

func TestNodeIdleState_PipelineIntegration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'idle-test-cluster', 'src-idle', now()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	ensureDailyNodeDigestPartitions(t, pool, start, 4)

	type nodeProfile struct {
		node             string
		cpuUsageP95      int64
		memUsageP95      int64
		maxCPUAllocMC    int64
		maxMemAllocKiB   int64
		maxCPURequestMC  int64
		maxMemRequestKiB int64
		maxPodCount      int64
	}

	profiles := []nodeProfile{
		{node: "zombie-node", cpuUsageP95: 100, memUsageP95: 1000, maxCPUAllocMC: 16000, maxMemAllocKiB: 65536, maxCPURequestMC: 500, maxMemRequestKiB: 4096, maxPodCount: 2},
		{node: "idle-node", cpuUsageP95: 800, memUsageP95: 4000, maxCPUAllocMC: 16000, maxMemAllocKiB: 65536, maxCPURequestMC: 2000, maxMemRequestKiB: 16384, maxPodCount: 8},
		{node: "active-node", cpuUsageP95: 4000, memUsageP95: 16000, maxCPUAllocMC: 16000, maxMemAllocKiB: 65536, maxCPURequestMC: 4000, maxMemRequestKiB: 32768, maxPodCount: 8},
	}

	for _, p := range profiles {
		for i := 0; i < 4; i++ {
			date := start.AddDate(0, 0, i)
			_, err = pool.Exec(ctx, `
				INSERT INTO daily_node_digests (
					bucket_date, org_id, cluster_uuid, node,
					cpu_usage_p50_mc, cpu_usage_p95_mc,
					mem_usage_p50_kib, mem_usage_p95_kib,
					max_cpu_allocatable_mc, max_mem_allocatable_kib,
					max_cpu_requests_mc, max_mem_requests_kib,
					max_pod_count, sample_count
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
				ON CONFLICT (org_id, cluster_uuid, node, bucket_date) DO NOTHING`,
				date, orgID, clusterUUID, p.node,
				p.cpuUsageP95-50, p.cpuUsageP95,
				p.memUsageP95-200, p.memUsageP95,
				p.maxCPUAllocMC, p.maxMemAllocKiB,
				p.maxCPURequestMC, p.maxMemRequestKiB,
				p.maxPodCount, int64(24),
			)
			require.NoError(t, err)
		}
	}

	end := start.AddDate(0, 0, 4)
	digests, err := engine.QueryNodeDigests(ctx, pool, orgID, clusterUUID, start, end)
	require.NoError(t, err)

	cfg := engine.NodeRecConfigFromThresholds(engine.DefaultNodeThresholdSettings())
	terms := []engine.TermConfig{{Name: "medium", WindowDays: 30, MinDataDays: 3}}
	recs := engine.RecommendNodes(digests, cfg, engine.DefaultNodeThresholdSettings(), terms)
	require.NotEmpty(t, recs)

	recByNode := map[string]engine.NodeRec{}
	for _, r := range recs {
		if r.Engine == "cost" && r.Term == "medium" {
			recByNode[r.Node] = r
		}
	}

	require.Equal(t, engine.IdleStateZombie, recByNode["zombie-node"].IdleState)
	assert.Contains(t, recByNode["zombie-node"].NotificationCodes, engine.NotifNodeIdle)
	require.Equal(t, engine.IdleStateIdle, recByNode["idle-node"].IdleState)
	assert.Contains(t, recByNode["idle-node"].NotificationCodes, engine.NotifNodeIdle)
	require.Equal(t, engine.IdleStateActive, recByNode["active-node"].IdleState)
	assert.Equal(t, "underutilized", recByNode["active-node"].Category)

	err = engine.PersistNodeRecommendations(ctx, pool, orgID, clusterUUID, recs, []string{"medium"})
	require.NoError(t, err)

	var idleState string
	err = pool.QueryRow(ctx,
		`SELECT idle_state FROM node_recommendations WHERE org_id = $1 AND node = $2 AND engine = 'cost'`,
		orgID, "zombie-node").Scan(&idleState)
	require.NoError(t, err)
	assert.Equal(t, "zombie", idleState)
}

// seedNodeDigests inserts test data into daily_node_digests for three nodes
// with distinct profiles: underutilized, overcommitted, and healthy.
func seedNodeDigests(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string, start time.Time, days int) {
	t.Helper()

	ensureDailyNodeDigestPartitions(t, pool, start, days)

	type nodeProfile struct {
		node             string
		cpuUsageP50      int64
		cpuUsageP95      int64
		memUsageP50      int64
		memUsageP95      int64
		maxCPUAllocMC    int64
		maxMemAllocKiB   int64
		maxCPURequestMC  int64
		maxMemRequestKiB int64
		maxPodCount      int64
	}

	profiles := []nodeProfile{
		{
			node: "underutilized-node",
			// Very low usage (10-15% of 8000mc / 32M KiB)
			cpuUsageP50: 800, cpuUsageP95: 1200,
			memUsageP50: 3200, memUsageP95: 4800,
			maxCPUAllocMC: 8000, maxMemAllocKiB: 33554432,
			maxCPURequestMC: 2000, maxMemRequestKiB: 8388608,
			maxPodCount: 5,
		},
		{
			node: "overcommitted-node",
			// Requests exceed allocatable (overcommit ratio > 1.0)
			cpuUsageP50: 3500, cpuUsageP95: 3800,
			memUsageP50: 14000000, memUsageP95: 15000000,
			maxCPUAllocMC: 4000, maxMemAllocKiB: 16777216,
			maxCPURequestMC: 5000, maxMemRequestKiB: 20000000,
			maxPodCount: 25,
		},
		{
			node: "healthy-node",
			// Moderate usage (50-60% of allocatable), requests below allocatable
			cpuUsageP50: 4000, cpuUsageP95: 4800,
			memUsageP50: 16000000, memUsageP95: 19000000,
			maxCPUAllocMC: 8000, maxMemAllocKiB: 33554432,
			maxCPURequestMC: 6000, maxMemRequestKiB: 25000000,
			maxPodCount: 15,
		},
	}

	ctx := context.Background()
	for _, p := range profiles {
		for i := 0; i < days; i++ {
			date := start.AddDate(0, 0, i)
			if _, err := pool.Exec(ctx, `
				INSERT INTO daily_node_digests (
					bucket_date, org_id, cluster_uuid, node,
					cpu_usage_p50_mc, cpu_usage_p95_mc,
					mem_usage_p50_kib, mem_usage_p95_kib,
					max_cpu_allocatable_mc, max_mem_allocatable_kib,
					max_cpu_requests_mc, max_mem_requests_kib,
					max_pod_count, sample_count
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
				ON CONFLICT (org_id, cluster_uuid, node, bucket_date) DO UPDATE SET
					cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc`,
				date, orgID, clusterUUID, p.node,
				p.cpuUsageP50+int64(i*10), p.cpuUsageP95+int64(i*15),
				p.memUsageP50+int64(i*100), p.memUsageP95+int64(i*150),
				p.maxCPUAllocMC, p.maxMemAllocKiB,
				p.maxCPURequestMC, p.maxMemRequestKiB,
				p.maxPodCount, int64(24),
			); err != nil {
				t.Fatalf("seedNodeDigests: insert failed for %s day %d: %v", p.node, i, err)
			}
		}
	}
}

// TestPersistNodeRecommendations_StaleTermCleanup verifies that rows with terms
// no longer in the active config are deleted after upsert.
func TestPersistNodeRecommendations_StaleTermCleanup(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID

	// Insert a recommendation with term "obsolete" directly (simulating a prior run).
	_, err := pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, is_underutilized, is_overcommitted, pod_count, updated_at)
		VALUES ($1, $2, 'stale-node', 'obsolete', 'cost', 50, 80, 60, 90, 1.0, false, false, 10, now())
		ON CONFLICT (org_id, cluster_uuid, node, term, engine) DO UPDATE SET updated_at = now()`,
		orgID, clusterUUID)
	require.NoError(t, err)

	// Persist new recommendations with active terms only.
	recs := []engine.NodeRec{
		{
			Node: "stale-node", Term: "short_term", Engine: "cost",
			CPUUtilP50: 40, CPUUtilP95: 70, MemUtilP50: 50, MemUtilP95: 80,
			CPUOvercommitRatio: 0.8, Category: "optimized",
			PodCount: 5,
		},
	}
	validTerms := []string{"short_term", "medium_term", "long_term"}
	err = engine.PersistNodeRecommendations(ctx, pool, orgID, clusterUUID, recs, validTerms)
	require.NoError(t, err)

	// Verify: "obsolete" term should be deleted, "short_term" should exist.
	var count int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM node_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND node = 'stale-node' AND term = 'obsolete'`,
		orgID, clusterUUID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "obsolete term should be cleaned up")

	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM node_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND node = 'stale-node' AND term = 'short_term'`,
		orgID, clusterUUID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "active term should exist")
}

// ensureDailyNodeDigestPartitions creates monthly RANGE partitions required for
// inserts into daily_node_digests (parent table is PARTITION BY RANGE(bucket_date)).
func ensureDailyNodeDigestPartitions(t *testing.T, pool *pgxpool.Pool, start time.Time, days int) {
	t.Helper()
	ctx := context.Background()
	if days < 1 {
		return
	}
	lastDate := start.AddDate(0, 0, days-1)
	firstMonth := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	lastMonth := time.Date(lastDate.Year(), lastDate.Month(), 1, 0, 0, 0, 0, time.UTC)
	for m := firstMonth; !m.After(lastMonth); m = m.AddDate(0, 1, 0) {
		monthEnd := m.AddDate(0, 1, 0)
		partName := fmt.Sprintf("daily_node_digests_%s", m.Format("200601"))
		_, err := pool.Exec(ctx, fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_node_digests FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			m.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		))
		require.NoError(t, err, "create partition %s", partName)
	}
}
