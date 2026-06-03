package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestProcessContainerBatch_analyticsDegradedOnFailure mirrors report_processor.go batch handling:
// recommendations persist and pipelineDegraded is set when history writes fail.
func TestProcessContainerBatch_analyticsDegradedOnFailure(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	batch := []engine.ContainerRec{
		{
			OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
			WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer,
			Term: "short", Engine: "cost",
			RecCPURequestMC: 100, RecCPULimitMC: 200,
			RecMemRequestKiB: 1024, RecMemLimitKiB: 2048,
		},
	}

	require.NoError(t, engine.WriteRecommendations(ctx, pool, batch))

	engine.SetBatchWriteHistoryForTest(func(context.Context, *pgxpool.Pool, []engine.ContainerRec, string) error {
		return fmt.Errorf("simulated history write failure")
	})
	t.Cleanup(func() { engine.SetBatchWriteHistoryForTest(nil) })

	pipelineDegraded := engine.ContainerBatchAnalyticsDegraded(ctx, pool, batch, nil, "")
	require.True(t, pipelineDegraded, "pipelineDegraded should be set when history write fails")

	var recCount int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1`,
		testutil.TestOrgID,
	).Scan(&recCount)
	require.NoError(t, err)
	assert.Greater(t, recCount, 0, "recommendations must remain after history failure")
}
