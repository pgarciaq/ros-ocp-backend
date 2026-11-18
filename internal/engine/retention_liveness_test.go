package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #874: both recommendation tables need their own staleness clock, because the
// purge must be keyed on when a row was MARKED, not on when it last reported.
func TestStaledAtColumnsExistAndAreNullable(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	for _, table := range []string{"recommendation_sets", "namespace_recommendation_sets"} {
		require.True(t, columnExists(t, pool, table, "staled_at"),
			"%s must carry a staled_at column", table)

		var dataType, isNullable string
		var colDefault *string
		err := pool.QueryRow(ctx, `
			SELECT data_type, is_nullable, column_default
			FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = 'staled_at'`,
			table).Scan(&dataType, &isNullable, &colDefault)
		require.NoError(t, err)

		assert.Equal(t, "timestamp with time zone", dataType, "%s.staled_at type", table)
		assert.Equal(t, "YES", isNullable, "%s.staled_at must be nullable: legacy rows have no mark", table)
		assert.Nil(t, colDefault, "%s.staled_at must not default to anything: NULL means never marked", table)
	}
}

// #874: staleness and report time are two different clocks. `updated_at` is the
// report time (projected as `last_reported`), so marking a row stale must not
// move it, and the mark itself needs its own column.
const (
	livenessCluster   = "8f0f6f5e-0000-4000-8000-000000087401"
	livenessNsCluster = "8f0f6f5e-0000-4000-8000-000000087402"
)

func seedLivenessOrg(t *testing.T, pool *pgxpool.Pool, org string, tenant int) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, tenant, org)
	require.NoError(t, err)
}

func seedLivenessCluster(t *testing.T, pool *pgxpool.Pool, cluster string, tenant int, reportedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2, $3, $4, $5)`,
		tenant, cluster, "liveness-"+cluster[len(cluster)-4:], "src-liveness", reportedAt)
	require.NoError(t, err)
}

func TestMarkUnreportedContainersStale_PreservesUpdatedAt(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-updated-at"
	const tenant = 9401
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, livenessCluster, tenant, time.Now().UTC())

	seeded := time.Now().UTC().AddDate(0, 0, -10).Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets
			(org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, updated_at)
		VALUES ($1, $2, 'ns-live', 'w', 'Deployment', 'c', 'short', 'cost', false, $3)`,
		org, livenessCluster, seeded)
	require.NoError(t, err)

	marked, err := pgrec.MarkUnreportedContainersStale(ctx, pool, org, livenessCluster, time.Now().UTC())
	require.NoError(t, err)
	require.EqualValues(t, 1, marked, "the unreported row must be marked")

	var stale bool
	var staledAt *time.Time
	var updatedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT stale, staled_at, updated_at FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'c'`,
		org, livenessCluster).Scan(&stale, &staledAt, &updatedAt))

	assert.True(t, stale)
	require.NotNil(t, staledAt, "marking stale must record when it happened")
	assert.WithinDuration(t, time.Now().UTC(), *staledAt, 5*time.Second)
	assert.Truef(t, seeded.Equal(updatedAt),
		"updated_at must stay the report time: seeded %s, got %s (last_reported would claim a report that never happened)",
		seeded, updatedAt)
}

func TestWriteRecommendations_ClearsStaledAt(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-clear"
	const tenant = 9402
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, livenessCluster, tenant, time.Now().UTC())

	old := time.Now().UTC().AddDate(0, 0, -20).Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets
			(org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, staled_at, updated_at)
		VALUES ($1, $2, 'ns-live', 'w', 'Deployment', 'c', 'short', 'cost', true, $3, $3)`,
		org, livenessCluster, old)
	require.NoError(t, err)

	require.NoError(t, pgrec.WriteRecommendations(ctx, pool, []types.ContainerRec{{
		OrgID:         org,
		ClusterUUID:   livenessCluster,
		Namespace:     "ns-live",
		Workload:      "w",
		WorkloadType:  "Deployment",
		ContainerName: "c",
		Term:          "short",
		Engine:        "cost",
		Stale:         false,
	}}))

	var stale bool
	var staledAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT stale, staled_at FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'c'`,
		org, livenessCluster).Scan(&stale, &staledAt))

	assert.False(t, stale, "a reporting container un-stales")
	assert.Nil(t, staledAt, "a reporting container clears its stale clock too")
}

func TestWriteRecommendations_StaleRecSetsStaledAt(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-stale-rec"
	const tenant = 9403
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, livenessCluster, tenant, time.Now().UTC())

	require.NoError(t, pgrec.WriteRecommendations(ctx, pool, []types.ContainerRec{{
		OrgID:         org,
		ClusterUUID:   livenessCluster,
		Namespace:     "ns-live",
		Workload:      "w",
		WorkloadType:  "Deployment",
		ContainerName: "c",
		Term:          "short",
		Engine:        "cost",
		Stale:         true,
	}}))

	var stale bool
	var staledAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT stale, staled_at FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = 'c'`,
		org, livenessCluster).Scan(&stale, &staledAt))

	assert.True(t, stale)
	require.NotNil(t, staledAt, "a row written already-stale must still record when")
}

func TestWriteNamespaceRecommendations_ClearsStaledAt(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-ns"
	const tenant = 9404
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, livenessNsCluster, tenant, time.Now().UTC())

	old := time.Now().UTC().AddDate(0, 0, -20).Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `
		INSERT INTO namespace_recommendation_sets
			(org_id, cluster_uuid, namespace_name, term, engine, stale, staled_at, updated_at,
			 monitoring_start_time, monitoring_end_time)
		VALUES ($1, $2, 'ns-live', 'short', 'cost', true, $3, $3, $4, $5)`,
		org, livenessNsCluster, old, old, old.AddDate(0, 0, 7))
	require.NoError(t, err)

	monitoringStart := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Microsecond)
	require.NoError(t, pgrec.WriteNamespaceRecommendations(ctx, pool, []namespace.NamespaceRec{{
		OrgID:               org,
		ClusterUUID:         livenessNsCluster,
		Namespace:           "ns-live",
		Term:                "short",
		Engine:              "cost",
		Stale:               false,
		MonitoringStartTime: monitoringStart,
		MonitoringEndTime:   monitoringStart.AddDate(0, 0, 7),
	}}))

	var stale bool
	var staledAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT stale, staled_at FROM namespace_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace_name = 'ns-live'`,
		org, livenessNsCluster).Scan(&stale, &staledAt))

	assert.False(t, stale)
	assert.Nil(t, staledAt)
}

func seedLivenessNamespaceRow(t *testing.T, pool *pgxpool.Pool, org, cluster, namespace string, stale bool) {
	t.Helper()
	ctx := context.Background()
	start := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `
		INSERT INTO namespace_recommendation_sets
			(org_id, cluster_uuid, namespace_name, term, engine, stale,
			 monitoring_start_time, monitoring_end_time, updated_at)
		VALUES ($1, $2, $3, 'short', 'cost', $4, $5, $6, now())`,
		org, cluster, namespace, stale, start, start.AddDate(0, 0, 7))
	require.NoError(t, err)
}

func seedLivenessContainerRow(t *testing.T, pool *pgxpool.Pool, org, cluster string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets
			(org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, updated_at)
		VALUES ($1, $2, 'ns-live', 'w', 'Deployment', 'c', 'short', 'cost', false, now())`,
		org, cluster)
	require.NoError(t, err)
}

func livenessFlags(t *testing.T, pool *pgxpool.Pool, table, org, cluster string) (stale bool, staledAt *time.Time) {
	t.Helper()
	ctx := context.Background()
	clusterCol := "cluster_uuid"
	require.NoError(t, pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT stale, staled_at FROM %s WHERE org_id = $1 AND %s = $2`,
		pgx.Identifier{table}.Sanitize(), clusterCol), org, cluster).Scan(&stale, &staledAt))
	return stale, staledAt
}

func TestMarkAbandonedClustersStale_MarksBothTables(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-mark-both"
	const tenant = 9410
	const dark = "8f0f6f5e-0000-4000-8000-000000008751"
	const live = "8f0f6f5e-0000-4000-8000-000000008752"
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, dark, tenant, time.Now().UTC().AddDate(0, 0, -40))
	seedLivenessCluster(t, pool, live, tenant, time.Now().UTC())

	seedLivenessContainerRow(t, pool, org, dark)
	seedLivenessContainerRow(t, pool, org, live)
	seedLivenessNamespaceRow(t, pool, org, dark, "dark-ns", false)
	seedLivenessNamespaceRow(t, pool, org, live, "live-ns", false)

	// updated_at is the report time (projected as last_reported). Marking must
	// leave it alone: a container that merely missed a report cycle must not
	// start claiming it was reported when the sweep ran.
	reportedBefore := map[string]time.Time{}
	for _, table := range recommendationLivenessTables {
		var reportedAt time.Time
		require.NoError(t, pool.QueryRow(ctx, fmt.Sprintf(
			`SELECT updated_at FROM %s WHERE org_id = $1 AND cluster_uuid = $2`,
			pgx.Identifier{table}.Sanitize()), org, dark).Scan(&reportedAt))
		reportedBefore[table] = reportedAt
	}

	marked, err := markAbandonedClustersStale(ctx, pool, time.Now().UTC().AddDate(0, 0, -14))
	require.NoError(t, err)
	assert.EqualValues(t, 2, marked, "one row per recommendation table must be marked")

	for _, table := range []string{"recommendation_sets", "namespace_recommendation_sets"} {
		stale, staledAt := livenessFlags(t, pool, table, org, dark)
		assert.Truef(t, stale, "%s: the 40-day-silent cluster must be marked", table)
		assert.NotNilf(t, staledAt, "%s: the mark must carry its own clock", table)

		var reportedAfter time.Time
		require.NoError(t, pool.QueryRow(ctx, fmt.Sprintf(
			`SELECT updated_at FROM %s WHERE org_id = $1 AND cluster_uuid = $2`,
			pgx.Identifier{table}.Sanitize()), org, dark).Scan(&reportedAfter))
		assert.Truef(t, reportedBefore[table].Equal(reportedAfter),
			"%s: marking must not move updated_at (report time): was %s, now %s",
			table, reportedBefore[table], reportedAfter)

		stale, staledAt = livenessFlags(t, pool, table, org, live)
		assert.Falsef(t, stale, "%s: a reporting cluster must stay live", table)
		assert.Nilf(t, staledAt, "%s: a live cluster carries no stale clock", table)
	}
}

func TestMarkAbandonedClustersStale_MultiRowClusterStaysLive(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-multi-row"
	const tenant = 9411
	const cluster = "8f0f6f5e-0000-4000-8000-000000008753"
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, cluster, tenant, time.Now().UTC().AddDate(0, 0, -40))
	// Second alias row for the same UUID, reporting now.
	_, err := pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2, 'alias-two', 'src-liveness-two', now())`, tenant, cluster)
	require.NoError(t, err)
	seedLivenessContainerRow(t, pool, org, cluster)

	marked, err := markAbandonedClustersStale(ctx, pool, time.Now().UTC().AddDate(0, 0, -14))
	require.NoError(t, err)
	assert.Zero(t, marked, "a cluster with any recent alias row is alive")

	stale, staledAt := livenessFlags(t, pool, "recommendation_sets", org, cluster)
	assert.False(t, stale)
	assert.Nil(t, staledAt)
}

func TestMarkAbandonedClustersStale_SkipsNullLastReported(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-null-report"
	const tenant = 9412
	const cluster = "8f0f6f5e-0000-4000-8000-000000008754"
	seedLivenessOrg(t, pool, org, tenant)
	// Registered, never reported: unknown, not abandoned.
	_, err := pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2, 'never-reported', 'src-liveness-null', NULL)`, tenant, cluster)
	require.NoError(t, err)
	seedLivenessContainerRow(t, pool, org, cluster)

	marked, err := markAbandonedClustersStale(ctx, pool, time.Now().UTC().AddDate(0, 0, -14))
	require.NoError(t, err)
	assert.Zero(t, marked, "a cluster that never reported must not be declared dead")

	stale, _ := livenessFlags(t, pool, "recommendation_sets", org, cluster)
	assert.False(t, stale)
}

func TestMarkAbandonedClustersStale_SkipsClusterWithoutClustersRow(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-no-cluster-row"
	const tenant = 9413
	const cluster = "8f0f6f5e-0000-4000-8000-000000008755"
	seedLivenessOrg(t, pool, org, tenant)
	// Rows left behind after the clusters row was removed (Sources destroy path).
	seedLivenessContainerRow(t, pool, org, cluster)

	marked, err := markAbandonedClustersStale(ctx, pool, time.Now().UTC().AddDate(0, 0, -14))
	require.NoError(t, err)
	assert.Zero(t, marked, "no clusters row means no liveness evidence at all")

	stale, _ := livenessFlags(t, pool, "recommendation_sets", org, cluster)
	assert.False(t, stale)
}

func TestMarkAbandonedClustersStale_IgnoresLegacyNamespaceRows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-legacy-ns"
	const tenant = 9414
	seedLivenessOrg(t, pool, org, tenant)

	start := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `
		INSERT INTO namespace_recommendation_sets
			(org_id, cluster_uuid, namespace_name, term, engine, stale,
			 monitoring_start_time, monitoring_end_time, updated_at)
		VALUES ($1, NULL, 'legacy-ns', NULL, 'cost', false, $2, $3, now())`,
		org, start, start.AddDate(0, 0, 7))
	require.NoError(t, err)

	marked, err := markAbandonedClustersStale(ctx, pool, time.Now().UTC().AddDate(0, 0, -14))
	require.NoError(t, err)
	assert.Zero(t, marked, "a legacy row has no cluster to judge")

	var stale bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT stale FROM namespace_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid IS NULL AND namespace_name = 'legacy-ns'`, org).Scan(&stale))
	assert.False(t, stale)
}

func seedStaleContainerRow(t *testing.T, pool *pgxpool.Pool, org, cluster string, staledAt *time.Time, updatedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets
			(org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			 term, engine, stale, staled_at, updated_at)
		VALUES ($1, $2, 'ns-live', 'w', 'Deployment', 'c', 'short', 'cost', true, $3, $4)`,
		org, cluster, staledAt, updatedAt)
	require.NoError(t, err)
}

func seedStaleNamespaceRow(t *testing.T, pool *pgxpool.Pool, org, cluster, namespace string, staledAt *time.Time, updatedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	start := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `
		INSERT INTO namespace_recommendation_sets
			(org_id, cluster_uuid, namespace_name, term, engine, stale,
			 staled_at, monitoring_start_time, monitoring_end_time, updated_at)
		VALUES ($1, $2, $3, 'short', 'cost', true, $4, $5, $6, $7)`,
		org, cluster, namespace, staledAt, start, start.AddDate(0, 0, 7), updatedAt)
	require.NoError(t, err)
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

func TestPurge_DeletesDueSilentClusterInBothTables(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-purge-both"
	const tenant = 9420
	const cluster = "8f0f6f5e-0000-4000-8000-000000008760"
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, cluster, tenant, time.Now().UTC().AddDate(0, 0, -60))

	markedAt := time.Now().UTC().AddDate(0, 0, -31)
	seedStaleContainerRow(t, pool, org, cluster, &markedAt, markedAt)
	seedStaleNamespaceRow(t, pool, org, cluster, "ns-live", &markedAt, markedAt)

	purged, err := purgeStaleRecommendations(ctx, pool, time.Now().UTC().AddDate(0, 0, -30), time.Now().UTC().AddDate(0, 0, -14))
	require.NoError(t, err)
	assert.EqualValues(t, 2, purged, "both recommendation tables must be reclaimed")

	assert.Zero(t, countRows(t, pool,
		`SELECT COUNT(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, org, cluster))
	assert.Zero(t, countRows(t, pool,
		`SELECT COUNT(*) FROM namespace_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, org, cluster))
}

func TestPurge_KeepsMistakenlyMarkedCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-purge-mistaken"
	const tenant = 9421
	const cluster = "8f0f6f5e-0000-4000-8000-000000008761"
	seedLivenessOrg(t, pool, org, tenant)
	// Reporting now: whatever marked its rows was wrong.
	seedLivenessCluster(t, pool, cluster, tenant, time.Now().UTC())

	markedAt := time.Now().UTC().AddDate(0, 0, -31)
	seedStaleContainerRow(t, pool, org, cluster, &markedAt, markedAt)
	seedStaleNamespaceRow(t, pool, org, cluster, "mistaken-ns", &markedAt, markedAt)

	purged, err := purgeStaleRecommendations(ctx, pool, time.Now().UTC().AddDate(0, 0, -30), time.Now().UTC().AddDate(0, 0, -14))
	require.NoError(t, err)
	assert.Zero(t, purged)
	assert.Equalf(t, 1, countRows(t, pool,
		`SELECT COUNT(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, org, cluster),
		"cluster %s is reporting; deleting it would destroy live data", cluster)
	assert.Equalf(t, 1, countRows(t, pool,
		`SELECT COUNT(*) FROM namespace_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, org, cluster),
		"the namespace branch carries its own liveness guard and must hold too (cluster %s)", cluster)
}

// A cluster whose clusters row is gone (the Sources destroy path) leaves
// recommendation rows behind with no liveness evidence at all. The guard is
// NOT EXISTS, so an empty probe means "nothing says this cluster is alive" and
// the rows must still be reclaimed — otherwise they would never age out.
func TestPurge_ReclaimsRowsOfDestroyedCluster(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-purge-destroyed"
	const tenant = 9426
	const cluster = "8f0f6f5e-0000-4000-8000-000000008766"
	seedLivenessOrg(t, pool, org, tenant)
	// No clusters row on purpose.

	markedAt := time.Now().UTC().AddDate(0, 0, -31)
	seedStaleContainerRow(t, pool, org, cluster, &markedAt, markedAt)
	seedStaleNamespaceRow(t, pool, org, cluster, "destroyed-ns", &markedAt, markedAt)

	purged, err := purgeStaleRecommendations(ctx, pool, time.Now().UTC().AddDate(0, 0, -30), time.Now().UTC().AddDate(0, 0, -14))
	require.NoError(t, err)
	assert.EqualValues(t, 2, purged, "a destroyed cluster's due rows must still be reclaimed")
	assert.Zero(t, countRows(t, pool,
		`SELECT COUNT(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, org, cluster))
	assert.Zero(t, countRows(t, pool,
		`SELECT COUNT(*) FROM namespace_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, org, cluster))
}

func TestPurge_KeepsNotYetDue(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-purge-not-due"
	const tenant = 9422
	const cluster = "8f0f6f5e-0000-4000-8000-000000008762"
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, cluster, tenant, time.Now().UTC().AddDate(0, 0, -60))

	markedAt := time.Now().UTC().AddDate(0, 0, -10)
	seedStaleContainerRow(t, pool, org, cluster, &markedAt, markedAt)

	purged, err := purgeStaleRecommendations(ctx, pool, time.Now().UTC().AddDate(0, 0, -30), time.Now().UTC().AddDate(0, 0, -14))
	require.NoError(t, err)
	assert.Zero(t, purged, "a mark is visible for ROS_STALE_CLEANUP_DAYS before deletion")
	assert.Equal(t, 1, countRows(t, pool,
		`SELECT COUNT(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, org, cluster))
}

func TestPurge_LegacyStaleRowUsesUpdatedAt(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-purge-legacy"
	const tenant = 9423
	const cluster = "8f0f6f5e-0000-4000-8000-000000008763"
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, cluster, tenant, time.Now().UTC().AddDate(0, 0, -60))

	// Marked before 000223 existed: no staled_at, deadline still runs from updated_at.
	reportedAt := time.Now().UTC().AddDate(0, 0, -45)
	seedStaleContainerRow(t, pool, org, cluster, nil, reportedAt)

	purged, err := purgeStaleRecommendations(ctx, pool, time.Now().UTC().AddDate(0, 0, -30), time.Now().UTC().AddDate(0, 0, -14))
	require.NoError(t, err)
	assert.EqualValues(t, 1, purged, "a legacy stale row must keep the deadline it had before the migration")
}

func TestRunRetentionSweep_MarksButDoesNotPurgeFreshMark(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-sweep-visible"
	const tenant = 9424
	const cluster = "8f0f6f5e-0000-4000-8000-000000008764"
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, cluster, tenant, time.Now().UTC().AddDate(0, 0, -30))

	reportedAt := time.Now().UTC().AddDate(0, 0, -30)
	seedLivenessContainerRowAt(t, pool, org, cluster, "c", reportedAt)

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	require.Equalf(t, 1, countRows(t, pool,
		`SELECT COUNT(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, org, cluster),
		"cluster %s was marked in this sweep, so its rows must stay visible for %d days",
		cluster, 30)

	stale, staledAt := livenessFlags(t, pool, "recommendation_sets", org, cluster)
	assert.True(t, stale, "the sweep must mark the cluster")
	require.NotNil(t, staledAt)
}

func TestRunRetentionSweep_PurgesWhenMarkAged(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-sweep-aged"
	const tenant = 9425
	const cluster = "8f0f6f5e-0000-4000-8000-000000008765"
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, cluster, tenant, time.Now().UTC().AddDate(0, 0, -60))

	markedAt := time.Now().UTC().AddDate(0, 0, -31)
	seedStaleContainerRow(t, pool, org, cluster, &markedAt, markedAt)
	seedStaleNamespaceRow(t, pool, org, cluster, "ns-live", &markedAt, markedAt)

	require.NoError(t, RunRetentionSweep(ctx, pool, 6))

	assert.Zero(t, countRows(t, pool,
		`SELECT COUNT(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, org, cluster))
	assert.Zero(t, countRows(t, pool,
		`SELECT COUNT(*) FROM namespace_recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`, org, cluster))
}

func seedLivenessContainerRowAt(t *testing.T, pool *pgxpool.Pool, org, cluster, container string, updatedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets
			(org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			 term, engine, stale, updated_at)
		VALUES ($1, $2, 'ns-live', 'w', 'Deployment', $3, 'short', 'cost', false, $4)`,
		org, cluster, container, updatedAt)
	require.NoError(t, err)
}

// The purge deletes recommendation rows; the list endpoints page and count
// through org_container_keys, whose is_stale is a copy. Without a refresh after
// the delete, the catalog keeps an entry for a cluster that no longer has rows,
// and an org whose only cluster died never runs another ingest to repair it —
// the orphan (and the count it feeds) is permanent.
//
// Mutation: dropping the refreshOrgListCatalogs call from
// purgeStaleRecommendations leaves container_keys = 1 here.
func TestPurge_PrunesOrgListCatalogs(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const org = "org-liveness-purge-catalogs"
	const tenant = 9431
	const cluster = "8f0f6f5e-0000-4000-8000-000000008771"
	seedLivenessOrg(t, pool, org, tenant)
	seedLivenessCluster(t, pool, cluster, tenant, time.Now().UTC().AddDate(0, 0, -40))

	markedAt := time.Now().UTC().AddDate(0, 0, -31)
	seedStaleContainerRow(t, pool, org, cluster, &markedAt, markedAt)

	// The catalog as the mark leaves it: the row is stale, but the container key
	// exists and carries is_stale = true.
	require.NoError(t, RefreshOrgMetadata(ctx, pool, org))
	assert.Equalf(t, 1, countRows(t, pool,
		`SELECT COUNT(*) FROM org_container_keys WHERE org_id = $1`, org),
		"fixture precondition: the mark publishes the key so filter[stale]=only can reach it")

	purged, err := purgeStaleRecommendations(ctx, pool, time.Now().UTC().AddDate(0, 0, -30), time.Now().UTC().AddDate(0, 0, -14))
	require.NoError(t, err)
	require.EqualValues(t, 1, purged)

	assert.Zero(t, countRows(t, pool,
		`SELECT COUNT(*) FROM org_container_keys WHERE org_id = $1`, org),
		"a deleted cluster must not keep a catalog entry: nothing would ever prune it")
	assert.Zero(t, countRows(t, pool,
		`SELECT COUNT(*) FROM recommendation_sets WHERE org_id = $1`, org))
}
