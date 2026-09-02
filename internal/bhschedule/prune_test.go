package bhschedule

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestPruneGPUBusinessHours_FiltersOrgID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	day := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	orgA := "org-gpu-prune-a"
	orgB := "org-gpu-prune-b"
	clusterA := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	clusterB := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	seed := func(orgID, cluster, ns, sched string) {
		testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
			IntervalStart: day,
			OrgID:         orgID,
			ClusterUUID:   cluster,
			Namespace:     ns,
			Workload:      "train",
			WorkloadType:  "deployment",
			ContainerName: "gpu",
			GPUModelName:  "A100",
			ScheduleType:  sched,
		})
	}
	seed(orgA, clusterA, "ml", "business_hours")
	seed(orgA, clusterA, "ml", "all_hours")
	seed(orgA, clusterA, "other", "business_hours")
	seed(orgB, clusterB, "ml", "business_hours")

	require.NoError(t, PruneNamespaceBusinessHoursDigests(ctx, pool, orgA, clusterA, "ml"))
	assert.Equal(t, 0, gpuCount(t, pool, orgA, clusterA, "ml", "business_hours"))
	assert.Equal(t, 1, gpuCount(t, pool, orgA, clusterA, "ml", "all_hours"))
	assert.Equal(t, 1, gpuCount(t, pool, orgA, clusterA, "other", "business_hours"))
	assert.Equal(t, 1, gpuCount(t, pool, orgB, clusterB, "ml", "business_hours"))

	require.NoError(t, PruneClusterBusinessHoursDigests(ctx, pool, orgA, clusterA))
	assert.Equal(t, 0, gpuCount(t, pool, orgA, clusterA, "other", "business_hours"))
	assert.Equal(t, 1, gpuCount(t, pool, orgA, clusterA, "ml", "all_hours"))
	assert.Equal(t, 1, gpuCount(t, pool, orgB, clusterB, "ml", "business_hours"))

	require.NoError(t, PruneOrgBusinessHoursDigests(ctx, pool, orgB))
	assert.Equal(t, 0, gpuCount(t, pool, orgB, clusterB, "ml", "business_hours"))
	assert.Equal(t, 1, gpuCount(t, pool, orgA, clusterA, "ml", "all_hours"))
}

func gpuCount(t *testing.T, pool *pgxpool.Pool, orgID, cluster, ns, sched string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM gpu_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3 AND schedule_type = $4`,
		orgID, cluster, ns, sched).Scan(&n)
	require.NoError(t, err)
	return n
}
