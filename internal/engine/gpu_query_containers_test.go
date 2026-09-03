package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/gpu"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestQueryGPURecommendationsForContainers_FiltersByKeys(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID
	now := time.Now().UTC().Truncate(24 * time.Hour)
	start := now.AddDate(0, 0, -7)
	terms := []core.TermConfig{
		{Name: "short_term", WindowDays: 1},
		{Name: "medium_term", WindowDays: 7},
		{Name: "long_term", WindowDays: 15},
	}

	for _, spec := range []struct {
		workload, container string
		smAvg               float64
	}{
		{testutil.TestWorkload, testutil.TestContainer, 0.65},
		{"other-workload", "other-container", 0.12},
	} {
		testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
			OrgID:         testutil.TestOrgID,
			IntervalStart: now,
			ClusterUUID:   clusterUUID,
			Namespace:     testutil.TestNamespace,
			Workload:      spec.workload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: spec.container,
			GPUModelName:  "NVIDIA A100-SXM4-40GB",
			NodeName:      "gpu-node-1",
			SMActiveAvg:   spec.smAvg,
		})
	}

	recs, nodeMap, _, err := gpu.QueryGPURecommendationsForContainers(ctx, pool, orgID, clusterUUID, []gpu.PageGPUKey{{
		ClusterUUID:   clusterUUID,
		Namespace:     testutil.TestNamespace,
		Workload:      testutil.TestWorkload,
		ContainerName: testutil.TestContainer,
	}}, start, now, terms, nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	key := gpu.GPUContainerKey{Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload, ContainerName: testutil.TestContainer}
	require.Contains(t, recs, key)
	assert.NotEmpty(t, nodeMap[key])
}

func TestQueryGPURecommendationsForContainers_EmptyKeys(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -7)

	recs, nodeMap, nodeLastSeen, err := gpu.QueryGPURecommendationsForContainers(
		ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, nil, start, now, []core.TermConfig{
			{Name: "short_term", WindowDays: 1},
			{Name: "medium_term", WindowDays: 7},
			{Name: "long_term", WindowDays: 15},
		}, nil,
	)
	require.NoError(t, err)
	assert.Nil(t, recs)
	assert.Nil(t, nodeMap)
	assert.Nil(t, nodeLastSeen)
}

func TestQueryGPURecommendations_ScopesByOrgID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	clusterUUID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	now := time.Now().UTC().Truncate(24 * time.Hour)
	start := now.AddDate(0, 0, -7)
	terms := []core.TermConfig{
		{Name: "short_term", WindowDays: 1},
		{Name: "medium_term", WindowDays: 7},
		{Name: "long_term", WindowDays: 15},
	}

	testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
		OrgID: "org-gpu-a", IntervalStart: now, ClusterUUID: clusterUUID,
		Namespace: "ml", Workload: "train", WorkloadType: "deployment",
		ContainerName: "gpu-a", GPUModelName: "A100", NodeName: "node-a", SMActiveAvg: 0.10,
	})
	testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
		OrgID: "org-gpu-b", IntervalStart: now, ClusterUUID: clusterUUID,
		Namespace: "ml", Workload: "train", WorkloadType: "deployment",
		ContainerName: "gpu-b", GPUModelName: "A100", NodeName: "node-b", SMActiveAvg: 0.90,
	})

	recsA, nodeMapA, _, err := gpu.QueryGPURecommendations(ctx, pool, "org-gpu-a", clusterUUID, start, now, terms, nil)
	require.NoError(t, err)
	require.Len(t, recsA, 1)
	assert.Contains(t, recsA, gpu.GPUContainerKey{Namespace: "ml", Workload: "train", ContainerName: "gpu-a"})
	assert.NotContains(t, recsA, gpu.GPUContainerKey{Namespace: "ml", Workload: "train", ContainerName: "gpu-b"})
	assert.Equal(t, "node-a", nodeMapA[gpu.GPUContainerKey{Namespace: "ml", Workload: "train", ContainerName: "gpu-a"}])

	recsB, _, _, err := gpu.QueryGPURecommendations(ctx, pool, "org-gpu-b", clusterUUID, start, now, terms, nil)
	require.NoError(t, err)
	require.Len(t, recsB, 1)
	assert.Contains(t, recsB, gpu.GPUContainerKey{Namespace: "ml", Workload: "train", ContainerName: "gpu-b"})

	clusters, triples, err := gpu.CountOrgGPUClusterStats(ctx, pool, "org-gpu-a", []string{clusterUUID})
	require.NoError(t, err)
	assert.Equal(t, 1, clusters)
	assert.Equal(t, 1, triples)
}
