package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestCreateCluster_RejectsEmptyOrgID(t *testing.T) {
	c := Cluster{
		TenantID:     1,
		SourceId:     "src",
		ClusterUUID:  "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		ClusterAlias: "alias",
	}
	err := c.CreateCluster()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "org_id is required")
}

func TestCreateCluster_RejectsOrgMismatch(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-cluster-mismatch-a"
	clusterUUID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	t0 := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	t1 := t0.Add(48 * time.Hour)

	acct := RHAccount{OrgId: orgID}
	require.NoError(t, acct.CreateRHAccount())
	require.NotZero(t, acct.ID)

	c := Cluster{
		TenantID:       acct.ID,
		OrgID:          orgID,
		SourceId:       "src-cluster-mismatch-a",
		ClusterUUID:    clusterUUID,
		ClusterAlias:   "cluster-mismatch-a",
		LastReportedAt: t0,
	}
	require.NoError(t, c.CreateCluster())

	// Same 4-tuple with a caller org that contradicts rh_accounts truth.
	// Pre-fix this overwrote the row (the #508 pattern); post-fix it is rejected.
	liar := Cluster{
		TenantID:       acct.ID,
		OrgID:          "org-cluster-mismatch-b",
		SourceId:       c.SourceId,
		ClusterUUID:    clusterUUID,
		ClusterAlias:   c.ClusterAlias,
		LastReportedAt: t1,
	}
	err := liar.CreateCluster()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mismatch")

	// Rejection writes nothing: stored org and timestamp are untouched.
	var gotOrg string
	var gotReported time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT org_id, last_reported_at FROM clusters
		WHERE cluster_uuid = $1::uuid AND source_id = $2`, clusterUUID, c.SourceId).Scan(&gotOrg, &gotReported))
	assert.Equal(t, orgID, gotOrg)
	assert.True(t, gotReported.Equal(t0), "rejected write must not touch last_reported_at (got %v, want %v)", gotReported, t0)
}

func TestCreateCluster_HealsDivergedOrgID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-cluster-heal-a"
	clusterUUID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	t0 := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	t1 := t0.Add(48 * time.Hour)

	acct := RHAccount{OrgId: orgID}
	require.NoError(t, acct.CreateRHAccount())
	require.NotZero(t, acct.ID)

	c := Cluster{
		TenantID:       acct.ID,
		OrgID:          orgID,
		SourceId:       "src-cluster-heal-a",
		ClusterUUID:    clusterUUID,
		ClusterAlias:   "cluster-heal-a",
		LastReportedAt: t0,
	}
	require.NoError(t, c.CreateCluster())

	// Simulate a pre-fix wrong row, bypassing validation with raw SQL.
	// A blind drop-org_id fix would cement this lie forever (trigger fills
	// only NULL/empty); validate-on-match must converge it back to truth.
	_, err := pool.Exec(ctx, `UPDATE clusters SET org_id = $1 WHERE tenant_id = $2 AND cluster_uuid = $3::uuid`,
		"org-cluster-heal-diverged", acct.ID, clusterUUID)
	require.NoError(t, err)

	honest := Cluster{
		TenantID:       acct.ID,
		OrgID:          orgID,
		SourceId:       c.SourceId,
		ClusterUUID:    clusterUUID,
		ClusterAlias:   c.ClusterAlias,
		LastReportedAt: t1,
	}
	require.NoError(t, honest.CreateCluster())

	var gotOrg string
	var gotReported time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT org_id, last_reported_at FROM clusters
		WHERE cluster_uuid = $1::uuid AND source_id = $2`, clusterUUID, c.SourceId).Scan(&gotOrg, &gotReported))
	assert.Equal(t, orgID, gotOrg)
	assert.True(t, gotReported.Equal(t1), "honest rewrite must refresh last_reported_at (got %v, want %v)", gotReported, t1)
}

func TestCreateCluster_CollidingUUIDAcrossTenants(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgA := "org-cluster-collide-a"
	orgB := "org-cluster-collide-b"
	// Same UUID AND same alias AND same source under two tenants: tenant_id
	// is the only discriminator (composite unique in 000002). Same values
	// defeat lexicographic-luck assertions per adversarial-fixtures.md.
	clusterUUID := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	sourceID := "src-cluster-collide"
	alias := "shared-alias"
	t0 := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	acctA := RHAccount{OrgId: orgA}
	require.NoError(t, acctA.CreateRHAccount())
	acctB := RHAccount{OrgId: orgB}
	require.NoError(t, acctB.CreateRHAccount())

	require.NoError(t, (&Cluster{
		TenantID: acctA.ID, OrgID: orgA, SourceId: sourceID,
		ClusterUUID: clusterUUID, ClusterAlias: alias, LastReportedAt: t0,
	}).CreateCluster())
	require.NoError(t, (&Cluster{
		TenantID: acctB.ID, OrgID: orgB, SourceId: sourceID,
		ClusterUUID: clusterUUID, ClusterAlias: alias, LastReportedAt: t0,
	}).CreateCluster())

	// A cross-tenant repoint attempt (A's row, B's org) is rejected, not merged.
	err := (&Cluster{
		TenantID: acctA.ID, OrgID: orgB, SourceId: sourceID,
		ClusterUUID: clusterUUID, ClusterAlias: alias, LastReportedAt: t0.Add(time.Hour),
	}).CreateCluster()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mismatch")

	// Both tenants keep their own rows and orgs.
	var gotA, gotB string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT org_id FROM clusters WHERE tenant_id = $1 AND cluster_uuid = $2::uuid`, acctA.ID, clusterUUID).Scan(&gotA))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT org_id FROM clusters WHERE tenant_id = $1 AND cluster_uuid = $2::uuid`, acctB.ID, clusterUUID).Scan(&gotB))
	assert.Equal(t, orgA, gotA)
	assert.Equal(t, orgB, gotB)
}

func TestCreateCluster_CollidingUUIDReversedOrder(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgA := "org-cluster-collide-rev-a"
	orgB := "org-cluster-collide-rev-b"
	clusterUUID := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	sourceID := "src-cluster-collide-rev"
	alias := "shared-alias"
	t0 := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	acctA := RHAccount{OrgId: orgA}
	require.NoError(t, acctA.CreateRHAccount())
	acctB := RHAccount{OrgId: orgB}
	require.NoError(t, acctB.CreateRHAccount())

	// Reversed insertion order: validation is per-call against rh_accounts,
	// so order must not matter. A fix keyed on "lowest id wins" or MAX(alias)
	// would be order-sensitive; this pins order-independence.
	require.NoError(t, (&Cluster{
		TenantID: acctB.ID, OrgID: orgB, SourceId: sourceID,
		ClusterUUID: clusterUUID, ClusterAlias: alias, LastReportedAt: t0,
	}).CreateCluster())
	require.NoError(t, (&Cluster{
		TenantID: acctA.ID, OrgID: orgA, SourceId: sourceID,
		ClusterUUID: clusterUUID, ClusterAlias: alias, LastReportedAt: t0,
	}).CreateCluster())

	var n int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM clusters WHERE cluster_uuid = $1::uuid AND source_id = $2`, clusterUUID, sourceID).Scan(&n))
	assert.Equal(t, 2, n, "colliding UUID must yield two tenant rows regardless of insertion order")
}
