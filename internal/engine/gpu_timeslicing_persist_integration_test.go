package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestGPUTimeslicingPersistPipeline_Integration exercises seed → classify → persist → verify
// live rows, history, and recommendation_sets denormalization with realistic DCGM data.
func TestGPUTimeslicingPersistPipeline_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := "org-gpu-ts-integration"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "gpu-ts-integration-node"
	start := testutil.RecentStart()
	terms := engine.DefaultTermsForPlugin("gpu")

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, orgID)
	require.NoError(t, err)
	var tenantID int
	err = pool.QueryRow(ctx, `SELECT id FROM rh_accounts WHERE org_id = $1`, orgID).Scan(&tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES ($1, $2, 'gpu-ts-integration', 'src-int', now()) ON CONFLICT DO NOTHING`, tenantID, clusterUUID)
	require.NoError(t, err)

	containers := []struct {
		ns, wl, cn string
		smAvg      float64
		tensorAvg  float64
		dramAvg    float64
		fbMax      float64
	}{
		{"ml-team", "training-a", "gpu-worker-a", 0.12, 0.05, 0.05, 2000},
		{"ml-team", "training-b", "gpu-worker-b", 0.08, 0.05, 0.05, 1500},
		{"ml-team", "inference", "gpu-worker-c", 0.15, 0.05, 0.05, 3000},
		{"ml-team", "heavy-job", "gpu-heavy", 0.78, 0.30, 0.45, 10000},
	}
	for _, c := range containers {
		testutil.SeedGPURecommendationSet(t, pool, orgID, clusterUUID, c.ns, c.wl, c.cn, "medium")
		for i := 0; i < 7; i++ {
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				IntervalStart:       start.AddDate(0, 0, i),
				ClusterUUID:         clusterUUID,
				Namespace:           c.ns,
				Workload:            c.wl,
				WorkloadType:        "deployment",
				ContainerName:       c.cn,
				GPUModelName:        "NVIDIA T4",
				NodeName:            nodeName,
				FBUsageMinMiB:       500,
				FBUsageMaxMiB:       c.fbMax,
				FBUsageAvgMiB:       c.fbMax / 2,
				TensorPipeActiveMin: c.tensorAvg - 0.03,
				TensorPipeActiveMax: c.tensorAvg + 0.05,
				TensorPipeActiveAvg: c.tensorAvg,
				DRAMActiveMin:       c.dramAvg - 0.02,
				DRAMActiveMax:       c.dramAvg + 0.08,
				DRAMActiveAvg:       c.dramAvg,
				SMActiveMin:         c.smAvg - 0.03,
				SMActiveMax:         c.smAvg + 0.05,
				SMActiveAvg:         c.smAvg,
			})
		}
	}

	require.NoError(t, engine.MarkContainersWithGPU(ctx, pool, orgID, clusterUUID))
	require.NoError(t, engine.StoreGPUClassifications(ctx, pool, orgID, clusterUUID, terms, nil))

	costData := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"gpu_cost_per_month": {Infrastructure: 300.0, Supplementary: 0},
		},
	}
	require.NoError(t, engine.ComputeAndPersistNodeGPUTimeSlicingRecs(
		ctx, pool, orgID, clusterUUID, terms, costData,
	))

	t.Run("live table has actionable recommendation", func(t *testing.T) {
		var replicas, candidateCount, impactedCount int
		var savingsCents *int64
		err := pool.QueryRow(ctx, `
			SELECT recommended_replicas, candidate_count, impacted_count, estimated_savings_cents
			FROM node_gpu_timeslicing_recommendations
			WHERE org_id = $1 AND cluster_uuid = $2 AND node_name = $3
			  AND gpu_model ILIKE '%T4%' AND term = 'medium'`,
			orgID, clusterUUID, nodeName,
		).Scan(&replicas, &candidateCount, &impactedCount, &savingsCents)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, replicas, 2)
		assert.Equal(t, 3, candidateCount)
		assert.Equal(t, 1, impactedCount)
		require.NotNil(t, savingsCents)
		assert.Greater(t, *savingsCents, int64(0))
	})

	t.Run("history row appended", func(t *testing.T) {
		var count int64
		err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM node_gpu_timeslicing_recommendation_history
			WHERE org_id = $1 AND cluster_uuid = $2 AND node_name = $3 AND term = 'medium'`,
			orgID, clusterUUID, nodeName).Scan(&count)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(1))
	})

	t.Run("recommendation_sets cross-reference set for candidates", func(t *testing.T) {
		var tsNode string
		var tsReplicas int
		err := pool.QueryRow(ctx, `
			SELECT time_slicing_node, time_slicing_replicas
			FROM recommendation_sets
			WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = 'ml-team'
			  AND workload = 'training-b' AND container_name = 'gpu-worker-b' AND term = 'medium'`,
			orgID, clusterUUID,
		).Scan(&tsNode, &tsReplicas)
		require.NoError(t, err)
		assert.Equal(t, nodeName, tsNode)
		assert.Greater(t, tsReplicas, 0)
	})
}
