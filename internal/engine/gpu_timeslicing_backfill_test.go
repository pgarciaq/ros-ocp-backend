package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestBackfillNodeGPUTimeslicingRecs_ProcessesCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := "org-gpu-ts-backfill"
	clusterUUID := testutil.TestClusterUUID
	nodeName := "backfill-gpu-node"
	start := testutil.RecentStart()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, orgID)
	require.NoError(t, err)
	var tenantID int
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM rh_accounts WHERE org_id = $1`, orgID).Scan(&tenantID))
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2, 'backfill-cluster', 'src-bf', now()) ON CONFLICT DO NOTHING`, tenantID, clusterUUID)
	require.NoError(t, err)

	for i := 0; i < 7; i++ {
		for _, c := range []string{"a", "b", "c"} {
			testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
				OrgID:         orgID,
				IntervalStart: start.AddDate(0, 0, i), ClusterUUID: clusterUUID,
				Namespace: "ml", Workload: "wl-" + c, WorkloadType: "deployment",
				ContainerName: c, GPUModelName: "NVIDIA T4", NodeName: nodeName,
				SMActiveAvg: 0.10, DRAMActiveAvg: 0.05, FBUsageMaxMiB: 2000, FBUsageAvgMiB: 1000,
			})
		}
	}

	orgs, clusters, err := engine.BackfillNodeGPUTimeslicingRecs(ctx, pool, orgID, clusterUUID)
	require.NoError(t, err)
	assert.Equal(t, 1, orgs)
	assert.Equal(t, 1, clusters)

	var count int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM node_gpu_timeslicing_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2`, orgID, clusterUUID).Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 0)
}
