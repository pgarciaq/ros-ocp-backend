package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestContainerBatchAnalyticsDegraded_HistoryFailurePreservesRecommendations(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	batch := []ContainerRec{
		{
			OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
			WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer,
			Term: "short", Engine: "cost",
			RecCPURequestMC: 100, RecCPULimitMC: 200,
			RecMemRequestKiB: 1024, RecMemLimitKiB: 2048,
		},
	}

	require.NoError(t, WriteRecommendations(ctx, pool, batch))

	origHistory := batchWriteHistory
	t.Cleanup(func() { batchWriteHistory = origHistory })
	batchWriteHistory = func(context.Context, *pgxpool.Pool, []ContainerRec, string) error {
		return fmt.Errorf("simulated history write failure")
	}

	degraded := ContainerBatchAnalyticsDegraded(ctx, pool, batch, nil, "")
	require.True(t, degraded, "history failure should mark analytics degraded")

	var recCount int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`,
		testutil.TestOrgID, testutil.TestClusterUUID,
	).Scan(&recCount)
	require.NoError(t, err)
	assert.Greater(t, recCount, 0, "recommendations must remain after history failure")
}

func TestContainerBatchAnalyticsDegraded_QualityFailurePreservesRecommendations(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	batch := []ContainerRec{
		{
			OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
			WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer,
			Term: "short", Engine: "cost",
			RecCPURequestMC: 200, RecCPULimitMC: 220,
			RecMemRequestKiB: 2048, RecMemLimitKiB: 4096,
		},
	}
	require.NoError(t, WriteRecommendations(ctx, pool, batch))

	key := containerKey{
		Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
		WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer,
	}
	oldRecs := map[string]map[containerKey]OldRecommendation{
		"cost": {key: {RecCPURequestMC: 100, RecMemRequestKiB: 1024}},
		"performance": {},
	}

	origQuality := batchWriteQuality
	t.Cleanup(func() { batchWriteQuality = origQuality })
	batchWriteQuality = func(
		context.Context, *pgxpool.Pool, []ContainerRec,
		map[string]map[containerKey]OldRecommendation, map[containerKey]int64,
	) error {
		return errors.New("simulated quality write failure")
	}

	degraded := ContainerBatchAnalyticsDegraded(ctx, pool, batch, oldRecs, "")
	require.True(t, degraded, "quality failure should mark analytics degraded")

	var recCount int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`,
		testutil.TestOrgID, testutil.TestClusterUUID,
	).Scan(&recCount)
	require.NoError(t, err)
	assert.Greater(t, recCount, 0, "recommendations must remain after quality failure")
}

func TestNamespaceBatchAnalyticsDegraded_HistoryFailurePreservesRecommendations(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-analytics-degraded", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)

	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	require.NoError(t, WriteNamespaceRecommendations(ctx, pool, results))

	origHistory := namespaceWriteHistory
	t.Cleanup(func() { namespaceWriteHistory = origHistory })
	namespaceWriteHistory = func(context.Context, *pgxpool.Pool, []NamespaceRec) error {
		return fmt.Errorf("simulated namespace history write failure")
	}

	degraded := WriteNamespaceBatchAnalytics(ctx, pool, results, nil)
	require.True(t, degraded, "namespace history failure should mark analytics degraded")

	var recCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM namespace_recommendation_sets
		 WHERE org_id = $1 AND namespace_name = $2 AND term IS NOT NULL`,
		testutil.TestOrgID, "ns-analytics-degraded",
	).Scan(&recCount)
	require.NoError(t, err)
	assert.Greater(t, recCount, 0, "namespace recommendations must remain after history failure")
}
