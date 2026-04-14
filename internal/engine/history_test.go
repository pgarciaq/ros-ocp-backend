package engine

import (
	"context"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteRecommendationHistory_InsertsSnapshot(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	EnsureHistoryPartitions(ctx, pool)

	recs := []ContainerRec{
		{
			OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
			WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer,
			Term: "short", Engine: "cost",
			RecCPURequestMC: 100, RecCPULimitMC: 200,
			RecMemRequestKiB: 1024, RecMemLimitKiB: 2048,
			NotificationCodes: []int16{1, 2}, ConfidenceLevel: 0.85,
		},
	}

	err := WriteRecommendationHistory(ctx, pool, recs, "test-binary-v1")
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_history WHERE org_id = $1`,
		testutil.TestOrgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var cpuReq, memReq int64
	var source string
	err = pool.QueryRow(ctx,
		`SELECT rec_cpu_request_millicores, rec_memory_request_kib, source_binary
		 FROM recommendation_history WHERE org_id = $1 AND term = $2 AND engine = $3`,
		testutil.TestOrgID, "short", "cost").Scan(&cpuReq, &memReq, &source)
	require.NoError(t, err)
	assert.Equal(t, int64(100), cpuReq)
	assert.Equal(t, int64(1024), memReq)
	assert.Equal(t, "test-binary-v1", source)
}

func TestWriteRecommendationHistory_MultipleTermsAndEngines(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	EnsureHistoryPartitions(ctx, pool)

	recs := []ContainerRec{
		{OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID, Namespace: "ns1", Workload: "deploy1", ContainerName: "main", Term: "short", Engine: "cost", RecCPURequestMC: 100, RecMemRequestKiB: 1024},
		{OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID, Namespace: "ns1", Workload: "deploy1", ContainerName: "main", Term: "short", Engine: "performance", RecCPURequestMC: 150, RecMemRequestKiB: 2048},
		{OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID, Namespace: "ns1", Workload: "deploy1", ContainerName: "main", Term: "medium", Engine: "cost", RecCPURequestMC: 120, RecMemRequestKiB: 1100},
		{OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID, Namespace: "ns1", Workload: "deploy1", ContainerName: "main", Term: "medium", Engine: "performance", RecCPURequestMC: 180, RecMemRequestKiB: 2200},
		{OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID, Namespace: "ns1", Workload: "deploy1", ContainerName: "main", Term: "long", Engine: "cost", RecCPURequestMC: 110, RecMemRequestKiB: 1050},
		{OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID, Namespace: "ns1", Workload: "deploy1", ContainerName: "main", Term: "long", Engine: "performance", RecCPURequestMC: 170, RecMemRequestKiB: 2100},
	}

	err := WriteRecommendationHistory(ctx, pool, recs, "v2")
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_history WHERE org_id = $1`,
		testutil.TestOrgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 6, count)
}

func TestWriteRecommendationHistory_EmptySlice(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	err := WriteRecommendationHistory(ctx, pool, nil, "v1")
	require.NoError(t, err)
}

func TestEnsureHistoryPartitions_Idempotent(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	EnsureHistoryPartitions(ctx, pool)
	EnsureHistoryPartitions(ctx, pool)

	var count int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_class WHERE relname LIKE 'recommendation_history_%' AND relkind = 'r'`).Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 3)
}
