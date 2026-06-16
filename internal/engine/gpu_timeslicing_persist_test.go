package engine

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestFloat32USDCentsPtr(t *testing.T) {
	t.Run("converts USD to cents", func(t *testing.T) {
		v := float32(225.50)
		cents := float32USDCentsPtr(&v)
		require.NotNil(t, cents)
		assert.Equal(t, int64(22550), *cents)
	})
	t.Run("nil when input nil", func(t *testing.T) {
		assert.Nil(t, float32USDCentsPtr(nil))
	})
	t.Run("matches money.USDToCents", func(t *testing.T) {
		v := float32(675.0)
		cents := float32USDCentsPtr(&v)
		require.NotNil(t, cents)
		assert.Equal(t, money.USDToCents(675.0), *cents)
	})
}

func seedGPUTimeslicingPersistFixtures(
	t *testing.T,
	pool *pgxpool.Pool,
	orgID, clusterUUID, nodeName string,
) {
	t.Helper()
	ctx := context.Background()
	start := testutil.RecentStart()

	containers := []struct {
		ns, wl, cn string
		smAvg      float64
		tensorAvg  float64
		dramAvg    float64
	}{
		{"ml-team", "training-a", "gpu-worker-a", 0.12, 0.05, 0.05},
		{"ml-team", "training-b", "gpu-worker-b", 0.08, 0.05, 0.05},
		{"ml-team", "inference", "gpu-worker-c", 0.15, 0.05, 0.05},
		{"ml-team", "heavy-job", "gpu-heavy", 0.78, 0.30, 0.45},
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
				FBUsageMaxMiB:       2000,
				FBUsageAvgMiB:       1200,
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

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (org_id) VALUES ($1)
		 ON CONFLICT (org_id) DO NOTHING`, orgID)
	require.NoError(t, err)
	var tenantID int
	err = pool.QueryRow(ctx, `SELECT id FROM rh_accounts WHERE org_id = $1`, orgID).Scan(&tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES ($1, $2, 'gpu-ts-persist', 'src-ts', now()) ON CONFLICT DO NOTHING`, tenantID, clusterUUID)
	require.NoError(t, err)

	require.NoError(t, MarkContainersWithGPU(ctx, pool, orgID, clusterUUID))
	require.NoError(t, StoreGPUClassifications(ctx, pool, orgID, clusterUUID, []TermConfig{
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168},
	}, nil))
}

func TestComputeAndPersistNodeGPUTimeSlicingRecs_Upsert(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID
	nodeName := "gpu-ts-persist-node"

	seedGPUTimeslicingPersistFixtures(t, pool, orgID, clusterUUID, nodeName)

	costData := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"gpu_cost_per_month": {Infrastructure: 250.0, Supplementary: 50.0},
		},
	}
	terms := []TermConfig{{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168}}

	err := ComputeAndPersistNodeGPUTimeSlicingRecs(ctx, pool, orgID, clusterUUID, terms, costData)
	require.NoError(t, err)

	var (
		replicas        int
		confidence      float32
		candidateCount  int
		impactedCount   int
		savingsCents    *int64
		perGPUCents     *int64
		candidateJSON   string
	)
	err = pool.QueryRow(ctx, `
		SELECT recommended_replicas, confidence, candidate_count, impacted_count,
		       estimated_savings_cents, savings_per_gpu_cents,
		       candidate_containers::text
		FROM node_gpu_timeslicing_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND node_name = $3 AND gpu_model ILIKE '%T4%' AND term = 'medium'`,
		orgID, clusterUUID, nodeName,
	).Scan(&replicas, &confidence, &candidateCount, &impactedCount, &savingsCents, &perGPUCents, &candidateJSON)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, replicas, 2)
	assert.LessOrEqual(t, replicas, 8)
	assert.Greater(t, confidence, float32(0))
	assert.Equal(t, 3, candidateCount)
	assert.Equal(t, 1, impactedCount)
	require.NotNil(t, savingsCents)
	assert.Greater(t, *savingsCents, int64(0))
	require.NotNil(t, perGPUCents)
	assert.Contains(t, candidateJSON, "gpu-worker-a")
	assert.NotContains(t, candidateJSON, "gpu-heavy")

	// Upsert idempotency: second run should not duplicate live rows.
	err = ComputeAndPersistNodeGPUTimeSlicingRecs(ctx, pool, orgID, clusterUUID, terms, costData)
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM node_gpu_timeslicing_recommendations WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestComputeAndPersistNodeGPUTimeSlicingRecs_NoCostData(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-gpu-ts-nocost"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "gpu-ts-nocost-node"

	seedGPUTimeslicingPersistFixtures(t, pool, orgID, clusterUUID, nodeName)

	terms := []TermConfig{{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168}}
	require.NoError(t, ComputeAndPersistNodeGPUTimeSlicingRecs(ctx, pool, orgID, clusterUUID, terms, nil))

	var savingsCents, perGPUCents *int64
	err := pool.QueryRow(ctx, `
		SELECT estimated_savings_cents, savings_per_gpu_cents
		FROM node_gpu_timeslicing_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND node_name = $3 AND term = 'medium'`,
		orgID, clusterUUID, nodeName,
	).Scan(&savingsCents, &perGPUCents)
	require.NoError(t, err)
	assert.Nil(t, savingsCents)
	assert.Nil(t, perGPUCents)
}

func TestComputeAndPersistNodeGPUTimeSlicingRecs_StaleTermDeletion(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-gpu-ts-stale"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "gpu-ts-stale-node"

	seedGPUTimeslicingPersistFixtures(t, pool, orgID, clusterUUID, nodeName)

	_, err := pool.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendations (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, confidence_level,
			candidate_count, impacted_count
		) VALUES ($1, $2, $3, 'NVIDIA T4', 'short', 4, 0.7, 0.7, 2, 0)
		ON CONFLICT (org_id, cluster_uuid, node_name, gpu_model, term) DO UPDATE SET updated_at = now()`,
		orgID, clusterUUID, nodeName)
	require.NoError(t, err)

	terms := []TermConfig{{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168}}
	require.NoError(t, ComputeAndPersistNodeGPUTimeSlicingRecs(ctx, pool, orgID, clusterUUID, terms, nil))

	var shortCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM node_gpu_timeslicing_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND term = 'short'`, orgID, clusterUUID).Scan(&shortCount)
	require.NoError(t, err)
	assert.Equal(t, 0, shortCount, "stale short term row should be deleted")

	var mediumCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM node_gpu_timeslicing_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND term = 'medium'`, orgID, clusterUUID).Scan(&mediumCount)
	require.NoError(t, err)
	assert.Equal(t, 1, mediumCount)
}

func TestComputeAndPersistNodeGPUTimeSlicingRecs_CandidateDenormalization(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-gpu-ts-denorm"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "gpu-ts-denorm-node"

	seedGPUTimeslicingPersistFixtures(t, pool, orgID, clusterUUID, nodeName)

	terms := []TermConfig{{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168}}
	require.NoError(t, ComputeAndPersistNodeGPUTimeSlicingRecs(ctx, pool, orgID, clusterUUID, terms, nil))

	var (
		tsNode     string
		tsReplicas int
	)
	err := pool.QueryRow(ctx, `
		SELECT time_slicing_node, time_slicing_replicas
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = 'ml-team'
		  AND workload = 'training-a' AND container_name = 'gpu-worker-a' AND term = 'medium'`,
		orgID, clusterUUID,
	).Scan(&tsNode, &tsReplicas)
	require.NoError(t, err)
	assert.Equal(t, nodeName, tsNode)
	assert.Greater(t, tsReplicas, 0)

	err = pool.QueryRow(ctx, `
		SELECT time_slicing_node, time_slicing_replicas
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'gpu-heavy' AND term = 'medium'`,
		orgID, clusterUUID,
	).Scan(&tsNode, &tsReplicas)
	require.NoError(t, err)
	assert.Empty(t, tsNode)
	assert.Equal(t, 0, tsReplicas)
}

func TestComputeAndPersistNodeGPUTimeSlicingRecs_HistoryAppend(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-gpu-ts-hist-persist"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "gpu-ts-hist-node"

	seedGPUTimeslicingPersistFixtures(t, pool, orgID, clusterUUID, nodeName)

	terms := []TermConfig{{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168}}
	require.NoError(t, ComputeAndPersistNodeGPUTimeSlicingRecs(ctx, pool, orgID, clusterUUID, terms, nil))

	var (
		liveReplicas int
		histReplicas int
		histCount    int64
	)
	err := pool.QueryRow(ctx, `
		SELECT recommended_replicas FROM node_gpu_timeslicing_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND node_name = $3 AND term = 'medium'`,
		orgID, clusterUUID, nodeName).Scan(&liveReplicas)
	require.NoError(t, err)

	err = pool.QueryRow(ctx, `
		SELECT COUNT(*), MAX(recommended_replicas)
		FROM node_gpu_timeslicing_recommendation_history
		WHERE org_id = $1 AND cluster_uuid = $2 AND node_name = $3 AND term = 'medium'`,
		orgID, clusterUUID, nodeName).Scan(&histCount, &histReplicas)
	require.NoError(t, err)
	assert.Equal(t, int64(1), histCount)
	assert.Equal(t, liveReplicas, histReplicas)
}
