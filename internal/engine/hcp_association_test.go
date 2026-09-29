package engine

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

// hcpSnap builds a complete snapshot row; empty strings stay empty.
func hcpSnap(manifest, ns, hcID, hcUID string, observed time.Time, complete bool) pgrec.SnapshotRow {
	return pgrec.SnapshotRow{
		ManifestID:      manifest,
		HCPNamespace:    ns,
		HostedClusterID: hcID,
		HcUID:           hcUID,
		HcpUID:          "hcp-" + hcUID,
		NamespaceUID:    "ns-" + manifest,
		ObservedAt:      observed,
		Complete:        complete,
	}
}

// TestResolveHCAssociation_Matrix pins the temporal rule with adversarial
// values: distinct IDs/UIDs/namespaces per case so agreement-by-coincidence
// (same values, single row, ordering) cannot green it.
func TestResolveHCAssociation_Matrix(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()

	tests := []struct {
		name  string
		snaps []pgrec.SnapshotRow
		want  map[string]string
		why   string
	}{
		{
			name:  "single complete proof associates",
			snaps: []pgrec.SnapshotRow{hcpSnap("m1", "clusters-hc1", "aaa-111", "uid-1", now, true)},
			want:  map[string]string{"clusters-hc1": "aaa-111"},
			why:   "unanimous complete evidence associates",
		},
		{
			name: "two hosted clusters map independently",
			snaps: []pgrec.SnapshotRow{
				hcpSnap("m1", "clusters-hc1", "aaa-111", "uid-1", now, true),
				hcpSnap("m1", "clusters-hc2", "bbb-222", "uid-2", now, true),
			},
			want: map[string]string{"clusters-hc1": "aaa-111", "clusters-hc2": "bbb-222"},
			why:  "per-namespace agreement is independent",
		},
		{
			name: "conflicting IDs fail closed",
			snaps: []pgrec.SnapshotRow{
				hcpSnap("m1", "clusters-hc1", "aaa-111", "uid-1", now, true),
				hcpSnap("m2", "clusters-hc1", "ccc-333", "uid-9", now.Add(time.Hour), true),
			},
			want: map[string]string{},
			why:  "any complete conflict voids the namespace",
		},
		{
			name: "same ID new UID is recreation not agreement",
			snaps: []pgrec.SnapshotRow{
				hcpSnap("m1", "clusters-hc", "aaa-111", "uid-3", now, true),
				hcpSnap("m2", "clusters-hc", "aaa-111", "uid-5", now.Add(time.Hour), true),
			},
			want: map[string]string{},
			why:  "HC3 recreated as HC5 under a reused ID must not associate",
		},
		{
			name: "same ID missing UIDs is lenient",
			snaps: []pgrec.SnapshotRow{
				hcpSnap("m1", "clusters-hc", "aaa-111", "", now, true),
				hcpSnap("m2", "clusters-hc", "aaa-111", "", now.Add(time.Hour), true),
			},
			want: map[string]string{"clusters-hc": "aaa-111"},
			why:  "absent UIDs cannot disprove incarnation; present conflicts can",
		},
		{
			name: "incomplete-only proves nothing",
			snaps: []pgrec.SnapshotRow{
				hcpSnap("m1", "clusters-hc1", "aaa-111", "uid-1", now, false),
			},
			want: map[string]string{},
			why:  "incomplete entries never associate, even with an ID present",
		},
		{
			name: "complete without ID proves nothing",
			snaps: []pgrec.SnapshotRow{
				hcpSnap("m1", "clusters-hc1", "", "uid-1", now, true),
			},
			want: map[string]string{},
			why:  "empty hosted ID is unproven identity",
		},
		{
			name: "incomplete conflicting rows do not void agreement",
			snaps: []pgrec.SnapshotRow{
				hcpSnap("m1", "clusters-hc1", "aaa-111", "uid-1", now, true),
				hcpSnap("m2", "clusters-hc1", "zzz-999", "uid-7", now.Add(time.Hour), false),
			},
			want: map[string]string{"clusters-hc1": "aaa-111"},
			why:  "only complete rows vote; incomplete noise is ignored",
		},
		{
			name:  "blank namespace skipped",
			snaps: []pgrec.SnapshotRow{hcpSnap("m1", "", "aaa-111", "uid-1", now, true)},
			want:  map[string]string{},
			why:   "blank namespaces never enter the map",
		},
		{
			name:  "empty input empty map",
			snaps: nil,
			want:  map[string]string{},
			why:   "no evidence means no association",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ResolveHCAssociation(tc.snaps), tc.why)
			// Order independence: reversed input must agree (map iteration
			// must not leak into the verdict).
			rev := append([]pgrec.SnapshotRow(nil), tc.snaps...)
			for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
				rev[i], rev[j] = rev[j], rev[i]
			}
			assert.Equal(t, tc.want, ResolveHCAssociation(rev), "order must not affect %s", tc.why)
		})
	}
}

func hcpRecRow(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID, namespace, workload string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine, stale, updated_at
		) VALUES ($1, $2, $3, $4, 'deployment', $4, 'short', 'cost', false, now())
		ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine) DO NOTHING`,
		orgID, clusterUUID, namespace, workload)
	require.NoError(t, err)
}

func hcpRecAssoc(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID, namespace, workload string) *string {
	t.Helper()
	var v *string
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT hosted_cluster_id FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND workload = $4
		  AND container_name = $4 AND term = 'short' AND engine = 'cost'`,
		orgID, clusterUUID, namespace, workload).Scan(&v))
	return v
}

// TestHCPMigration199_PreMigrationDegrades proves code ahead of migration 000199
// never fails: upsert, resolve, and marking degrade to silent no-ops.
func TestHCPMigration199_PreMigrationDegrades(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsTo(t, connStr, 198)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()

	now := time.Now().UTC()
	require.NoError(t, pgrec.UpsertHCPSnapshots(ctx, pool, topoTestOrg, topoTestCluster, "m-pre",
		[]topology.HCPSnapshotEntry{{HCPNamespace: "clusters-hc1", HostedClusterID: "aaa-111", Complete: true, ObservedAt: now.Format(time.RFC3339)}}),
		"pre-000199 upsert must degrade, not fail")
	got, err := LoadHCAssociationForRun(ctx, pool, topoTestOrg, topoTestCluster)
	require.NoError(t, err)
	assert.Empty(t, got, "pre-000199 resolve must yield empty, never an error")
	assoc, cleared, err := MarkHCPAssociations(ctx, pool, topoTestOrg, topoTestCluster, map[string]string{"clusters-hc1": "aaa-111"})
	require.NoError(t, err, "pre-000199 marking must degrade, not fail")
	assert.Zero(t, assoc)
	assert.Zero(t, cleared)
}

// TestHCPAssociation_RoundTripMarkClear proves persist -> resolve -> mark ->
// refresh-clear end to end, with NULL defaults on never-associated rows.
func TestHCPAssociation_RoundTripMarkClear(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()
	now := time.Now().UTC()

	hcpRecRow(t, pool, topoTestOrg, topoTestCluster, "clusters-hc1", "etcd")
	hcpRecRow(t, pool, topoTestOrg, topoTestCluster, "ros-demo", "app")
	assert.Nil(t, hcpRecAssoc(t, pool, topoTestOrg, topoTestCluster, "clusters-hc1", "etcd"),
		"fresh rows default to NULL association")

	require.NoError(t, pgrec.UpsertHCPSnapshots(ctx, pool, topoTestOrg, topoTestCluster, "m1",
		[]topology.HCPSnapshotEntry{{
			HCPNamespace: "clusters-hc1", HostedClusterID: "aaa-111",
			HcUID: "uid-1", ObservedAt: now.Format(time.RFC3339), Complete: true,
		}}))
	got, err := LoadHCAssociationForRun(ctx, pool, topoTestOrg, topoTestCluster)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"clusters-hc1": "aaa-111"}, got)

	assoc, cleared, err := MarkHCPAssociations(ctx, pool, topoTestOrg, topoTestCluster, got)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, assoc, int64(1), "HCP row must associate")
	assert.Zero(t, cleared)
	require.NotNil(t, hcpRecAssoc(t, pool, topoTestOrg, topoTestCluster, "clusters-hc1", "etcd"))
	assert.Equal(t, "aaa-111", *hcpRecAssoc(t, pool, topoTestOrg, topoTestCluster, "clusters-hc1", "etcd"))
	assert.Nil(t, hcpRecAssoc(t, pool, topoTestOrg, topoTestCluster, "ros-demo", "app"),
		"app-namespace rows never associate")

	// Conflicting later evidence voids and clears (refresh-clear, not relabel).
	require.NoError(t, pgrec.UpsertHCPSnapshots(ctx, pool, topoTestOrg, topoTestCluster, "m2",
		[]topology.HCPSnapshotEntry{{
			HCPNamespace: "clusters-hc1", HostedClusterID: "ccc-333",
			HcUID: "uid-9", ObservedAt: now.Add(time.Hour).Format(time.RFC3339), Complete: true,
		}}))
	got, err = LoadHCAssociationForRun(ctx, pool, topoTestOrg, topoTestCluster)
	require.NoError(t, err)
	assert.Empty(t, got, "conflict must void the namespace, not relabel it")
	assoc, cleared, err = MarkHCPAssociations(ctx, pool, topoTestOrg, topoTestCluster, got)
	require.NoError(t, err)
	assert.Zero(t, assoc)
	assert.GreaterOrEqual(t, cleared, int64(1), "lapsed association must clear")
	assert.Nil(t, hcpRecAssoc(t, pool, topoTestOrg, topoTestCluster, "clusters-hc1", "etcd"),
		"cleared rows return to NULL, never to the conflicting ID")
}
