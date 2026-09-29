package engine

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

const hcpHistNS = "clusters-hc1"

// hcpHistRec builds a minimal history batch row.
func hcpHistRec(namespace, workload string) ContainerRec {
	return ContainerRec{
		OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
		Namespace: namespace, Workload: workload,
		WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer,
		Term: "short", Engine: "cost",
		RecCPURequestMC: 100, RecMemRequestKiB: 1024,
	}
}

// hcpHistHCID reads the frozen association for one history row.
func hcpHistHCID(t *testing.T, pool *pgxpool.Pool, namespace, workload string) string {
	t.Helper()
	var hc string
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT hosted_cluster_id FROM recommendation_history
		WHERE org_id = $1 AND namespace = $2 AND workload = $3 AND term = 'short' AND engine = 'cost'`,
		testutil.TestOrgID, namespace, workload).Scan(&hc))
	return hc
}

// TestHistoryHCID_FrozenOnWrite proves the writer freezes the window
// association per row: HCP rows carry the proven ID, app rows stay ''.
func TestHistoryHCID_FrozenOnWrite(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	EnsureHistoryPartitions(ctx, pool)

	now := time.Now().UTC()
	require.NoError(t, pgrec.UpsertHCPSnapshots(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, "m-h1",
		[]topology.HCPSnapshotEntry{{
			HCPNamespace: hcpHistNS, HostedClusterID: "aaa-111", HcUID: "uid-1",
			ObservedAt: now.Format(time.RFC3339), Complete: true,
		}}))

	require.NoError(t, WriteRecommendationHistory(ctx, pool,
		[]ContainerRec{hcpHistRec(hcpHistNS, "etcd"), hcpHistRec("ros-demo", "app")}, "hc-bin"))

	assert.Equal(t, "aaa-111", hcpHistHCID(t, pool, hcpHistNS, "etcd"),
		"proven HCP rows freeze the hosted ID")
	assert.Equal(t, "", hcpHistHCID(t, pool, "ros-demo", "app"),
		"app rows stay unassociated")
}

// TestHistoryHCID_HC3HC5Coexist proves recreation never overwrites: after a
// conflicting snapshot voids the namespace, rewriting the same day/workload
// inserts a separate '' row beside the frozen HC3 row.
func TestHistoryHCID_HC3HC5Coexist(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	EnsureHistoryPartitions(ctx, pool)

	now := time.Now().UTC()
	require.NoError(t, pgrec.UpsertHCPSnapshots(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, "m-hc3",
		[]topology.HCPSnapshotEntry{{
			HCPNamespace: hcpHistNS, HostedClusterID: "aaa-111", HcUID: "uid-3",
			ObservedAt: now.Format(time.RFC3339), Complete: true,
		}}))
	require.NoError(t, WriteRecommendationHistory(ctx, pool,
		[]ContainerRec{hcpHistRec(hcpHistNS, "etcd")}, "hc-bin"))

	require.NoError(t, pgrec.UpsertHCPSnapshots(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, "m-hc5",
		[]topology.HCPSnapshotEntry{{
			HCPNamespace: hcpHistNS, HostedClusterID: "bbb-222", HcUID: "uid-5",
			ObservedAt: now.Add(time.Hour).Format(time.RFC3339), Complete: true,
		}}))
	require.NoError(t, WriteRecommendationHistory(ctx, pool,
		[]ContainerRec{hcpHistRec(hcpHistNS, "etcd")}, "hc-bin"))

	var ids []string
	rows, err := pool.Query(ctx, `
		SELECT hosted_cluster_id FROM recommendation_history
		WHERE org_id = $1 AND namespace = $2 AND workload = 'etcd' AND term = 'short' AND engine = 'cost'
		ORDER BY hosted_cluster_id`,
		testutil.TestOrgID, hcpHistNS)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var hc string
		require.NoError(t, rows.Scan(&hc))
		ids = append(ids, hc)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"", "aaa-111"}, ids,
		"HC3 frozen row and voided rewrite must coexist; nothing overwritten, nothing backfilled")
}

// TestHistoryHCID_PreMigrationDegrades proves code ahead of migration 000200
// degrades to unwritten history instead of failing the batch.
func TestHistoryHCID_PreMigrationDegrades(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsTo(t, connStr, 199)
	pool := topoTestPool(t, connStr)

	require.NoError(t, WriteRecommendationHistory(context.Background(), pool,
		[]ContainerRec{hcpHistRec("clusters-hc1", "etcd")}, "hc-bin"),
		"pre-000200 history write must degrade, not fail")
}
