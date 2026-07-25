package engine_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/gpu"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestNodeGPUTimeslicingHistory_ListAndPagination(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-gpu-ts-hist-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID
	nodeName := "gpu-node-hist"
	gpuModel := "NVIDIA-L4"

	for i := 0; i < 3; i++ {
		_, err := pool.Exec(ctx, `
			INSERT INTO node_gpu_timeslicing_recommendation_history (
				org_id, cluster_uuid, node_name, gpu_model, term,
				recommended_replicas, confidence, candidate_count, impacted_count
			) VALUES ($1, $2::uuid, $3, $4, 'medium', $5, 0.8, 2, 1)`,
			orgID, clusterUUID, nodeName, gpuModel, i+2,
		)
		require.NoError(t, err)
	}

	rows, total, err := gpu.ListNodeGPUTimeslicingRecommendationHistory(
		ctx, pool, orgID, clusterUUID, nodeName, gpuModel, "", "recorded_at", "desc", 2, 0,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, rows, 2)
	assert.Equal(t, nodeName, rows[0].NodeName)
	assert.Equal(t, gpuModel, rows[0].GPUModel)
}

func TestNodeGPUTimeslicingHistory_FilterTerm(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-gpu-ts-term-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID
	nodeName := "gpu-node-term"

	for _, term := range []string{"short", "medium"} {
		_, err := pool.Exec(ctx, `
			INSERT INTO node_gpu_timeslicing_recommendation_history (
				org_id, cluster_uuid, node_name, gpu_model, term,
				recommended_replicas, confidence, candidate_count, impacted_count
			) VALUES ($1, $2::uuid, $3, 'L4', $4, 4, 0.7, 1, 0)`,
			orgID, clusterUUID, nodeName, term,
		)
		require.NoError(t, err)
	}

	rows, total, err := gpu.ListNodeGPUTimeslicingRecommendationHistory(
		ctx, pool, orgID, clusterUUID, nodeName, "", "short", "recorded_at", "desc", 10, 0,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	assert.Equal(t, "short", rows[0].Term)
}

func TestNodeGPUTimeslicingHistory_Retention(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID

	_, err := pool.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendation_history (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, candidate_count, impacted_count,
			recorded_at
		) VALUES ($1, $2::uuid, 'old-node', 'L4', 'medium', 2, 0.5, 1, 0, NOW() - INTERVAL '120 days')`,
		orgID, clusterUUID,
	)
	require.NoError(t, err)
	require.NoError(t, gpu.PruneNodeGPUTimeslicingRecommendationHistory(ctx, pool))

	var count int64
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM node_gpu_timeslicing_recommendation_history
		WHERE node_name = 'old-node'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestAppendNodeGPUTimeslicingHistory_OnPersist(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-gpu-ts-append-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID

	rec := &gpu.TimeslicingRec{
		NodeName:            "persist-node",
		ClusterUUID:         clusterUUID,
		GPUModel:            "L4",
		Term:                "medium",
		RecommendedReplicas: 4,
		Confidence:          0.75,
		NotificationCodes:   []int16{},
		CandidateContainers: []gpu.GPUContainerRef{{Namespace: "ns", Workload: "wl", Container: "c1"}},
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	require.NoError(t, gpu.UpsertNodeGPUTimeslicingRec(ctx, tx, orgID, clusterUUID, rec, nil))
	require.NoError(t, gpu.AppendNodeGPUTimeslicingHistory(ctx, tx, orgID, clusterUUID, []*gpu.TimeslicingRec{rec}))
	require.NoError(t, tx.Commit(ctx))

	var count int64
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM node_gpu_timeslicing_recommendation_history
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND node_name = $3`,
		orgID, clusterUUID, rec.NodeName,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}
