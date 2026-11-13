package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #846: populated downgrade scenarios. The empty-schema roundtrip
// (TestMigrationRoundtrip) proves the down scripts execute; these prove
// what they do to DATA. Every scenario seeds the collision shapes that
// are legal under the up-migration's contract, then downgrades past the
// migration under test:
//   - success scenarios assert the predecessor contract is restored;
//   - guard scenarios assert the fail-loud policy message (asserting
//     the MESSAGE text, never just "an error", so removing a guard
//     cannot pass: the raw PostgreSQL failure carries no remediation).
//
// Guard scenarios use golang-migrate's Force to land on the target
// version without walking the whole chain down (the chain below 25 is
// not exercised anywhere and is not this issue's scope); the guard runs
// FIRST in each down file, so the version the schema reached by is
// immaterial to what is under test.

// newDownScenarioDB starts a fresh Postgres and returns its connStr.
func newDownScenarioDB(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("down_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgContainer.Terminate(ctx) })
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	return connStr
}

// migrateUpTo runs all migrations up.
func migrateUpTo(t *testing.T, connStr string) {
	t.Helper()
	m, err := migrate.New("file://"+testutil.MigrationsPath(), connStr)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)
}

// migrateDownTo targets a version; wantErr (empty = must succeed)
// asserts the outcome, matching message text for failures.
func migrateDownTo(t *testing.T, connStr string, target uint, wantErr string) {
	t.Helper()
	m, err := migrate.New("file://"+testutil.MigrationsPath(), connStr)
	require.NoError(t, err)
	err = m.Migrate(target)
	srcErr, dbErr := m.Close()
	if wantErr == "" {
		require.NoError(t, err)
	} else {
		require.Error(t, err, "the downgrade must fail loud, not silently proceed")
		// #846 review P3: golang-migrate embeds the whole migration file
		// in the error text, so a Contains match would pass even when the
		// guard never fired and a LATER statement failed. The guard's
		// runtime message LEADS the error ("migration failed: <PG
		// error> in line 0: <file>") — match the prefix, never the
		// excerpt.
		assert.True(t, strings.HasPrefix(err.Error(), "migration failed: "+wantErr),
			"the guard's message must lead the error (got: %.200s)", err.Error())
	}
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)
}

// forceVersion pins the golang-migrate version without running
// migrations (test scaffolding for guard scenarios).
func forceVersion(t *testing.T, connStr string, version int) {
	t.Helper()
	m, err := migrate.New("file://"+testutil.MigrationsPath(), connStr)
	require.NoError(t, err)
	require.NoError(t, m.Force(version))
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)
}

// scenarioPool opens a pgx pool for seeding and assertions.
func scenarioPool(t *testing.T, connStr string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// S1 (#846 close criterion): 113-down restores the actual predecessor
// contract — the five-column term key 64-up established — and succeeds
// on populated multi-term data. Baseline: the current 113-down adds a
// four-column constraint over legal multi-term rows and fails.
func TestMigrationDown_PVC113_PopulatedMultiTermSucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := newDownScenarioDB(t)
	migrateUpTo(t, connStr)
	pool := scenarioPool(t, connStr)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO pvc_recommendation_sets (org_id, cluster_uuid, namespace, persistentvolumeclaim, term)
		VALUES ('org1', '11111111-1111-1111-1111-111111111111', 'ns', 'pvc-a', 'short'),
		       ('org1', '11111111-1111-1111-1111-111111111111', 'ns', 'pvc-a', 'medium')`)
	require.NoError(t, err, "multi-term PVCs are legal under the 113-up contract")

	migrateDownTo(t, connStr, 112, "")

	var def string
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = 'pvc_recommendation_sets_org_cluster_ns_pvc_term_key'
		  AND conrelid = 'pvc_recommendation_sets'::regclass
	`).Scan(&def))
	assert.Contains(t, def, "org_id, cluster_uuid, namespace, persistentvolumeclaim, term",
		"113-down must restore the five-column term key 64-up established (columns, not just the name)")
}

// S2: 64-down drops the term column (creating the duplicates) and then
// adds the four-column contract — the guard must block on multi-term
// data instead of failing on a raw constraint error.
func TestMigrationDown_PVC64_GuardBlocksMultiTerm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := newDownScenarioDB(t)
	migrateUpTo(t, connStr)
	pool := scenarioPool(t, connStr)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO pvc_recommendation_sets (org_id, cluster_uuid, namespace, persistentvolumeclaim, term)
		VALUES ('org1', '11111111-1111-1111-1111-111111111111', 'ns', 'pvc-a', 'short'),
		       ('org1', '11111111-1111-1111-1111-111111111111', 'ns', 'pvc-a', 'medium')`)
	require.NoError(t, err)
	forceVersion(t, connStr, 64)
	migrateDownTo(t, connStr, 63, "downgrade 000064 blocked")
}

// S3: 125-down narrows quota identity back to one row per namespace —
// the guard must block on multi-quota-name data.
func TestMigrationDown_Quota125_GuardBlocksMultiQuota(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := newDownScenarioDB(t)
	migrateUpTo(t, connStr)
	pool := scenarioPool(t, connStr)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO quota_recommendation_sets (org_id, cluster_uuid, namespace, quota_name)
		VALUES ('org1', '11111111-1111-1111-1111-111111111111', 'ns', 'cpu'),
		       ('org1', '11111111-1111-1111-1111-111111111111', 'ns', 'memory')`)
	require.NoError(t, err, "multiple quota names per namespace are legal under 125-up")
	forceVersion(t, connStr, 125)
	migrateDownTo(t, connStr, 124, "downgrade 000125 blocked")
}

// S4 (owner decision 2026-11-13): 185-down must not silently delete the
// business-hours stream — the guard blocks with the exact deterministic
// cleanup statement. Baseline: the current 185-down deletes the BH rows
// and succeeds, silently.
func TestMigrationDown_VM185_GuardBlocksDualSchedule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := newDownScenarioDB(t)
	migrateUpTo(t, connStr)
	pool := scenarioPool(t, connStr)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO daily_vm_digests (org_id, cluster_uuid, vm_name, namespace, bucket_date, schedule_type)
		VALUES ('org1', '11111111-1111-1111-1111-111111111111', 'vm-a', 'ns', '2026-01-05', 'all_hours'),
		       ('org1', '11111111-1111-1111-1111-111111111111', 'vm-a', 'ns', '2026-01-05', 'business_hours')`)
	require.NoError(t, err, "dual-stream VM digests are legal under 185-up")
	forceVersion(t, connStr, 185)
	migrateDownTo(t, connStr, 184, "downgrade 000185 blocked")
}

// S5: 189-down narrows the GPU natural key back to cluster scope — the
// guard replaces the documented-but-raw constraint failure with the
// policy message. Baseline: raw "could not create unique index" with no
// remediation.
func TestMigrationDown_GPU189_GuardBlocksCrossOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := newDownScenarioDB(t)
	migrateUpTo(t, connStr)
	pool := scenarioPool(t, connStr)
	// gpu_container_digests is partitioned by interval_start month; the
	// ingestion layer ensures partitions at write time (same statement).
	_, err := pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS gpu_container_digests_202601 PARTITION OF gpu_container_digests
		FOR VALUES FROM ('2026-01-01') TO ('2026-02-01')`)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO gpu_container_digests (interval_start, org_id, cluster_uuid, namespace, workload, container_name, gpu_model_name)
		VALUES ('2026-01-05 00:00:00', 'org1', '11111111-1111-1111-1111-111111111111', 'ns', 'wl', 'ctr', 'model-a'),
		       ('2026-01-05 00:00:00', 'org2', '11111111-1111-1111-1111-111111111111', 'ns', 'wl', 'ctr', 'model-a')`)
	require.NoError(t, err, "cross-org GPU digests are legal under 189-up")
	forceVersion(t, connStr, 189)
	migrateDownTo(t, connStr, 188, "downgrade 000189 blocked")
}

// S6: 12-down re-adds UNIQUE(account) — the guard must block on
// duplicate accounts. NULL accounts do not collide (Postgres UNIQUE
// treats NULLs as distinct), so the guard counts non-null values only.
func TestMigrationDown_Accounts12_GuardBlocksDuplicateAccounts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := newDownScenarioDB(t)
	migrateUpTo(t, connStr)
	pool := scenarioPool(t, connStr)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO rh_accounts (account, org_id)
		VALUES ('acct-dup', 'org1'), ('acct-dup', 'org2')`)
	require.NoError(t, err, "duplicate accounts are legal under 12-up (constraint removed)")
	forceVersion(t, connStr, 12)
	migrateDownTo(t, connStr, 11, "downgrade 000012 blocked")
}

// S7: 28-down re-adds UQ_Recommendation(workload_id, container_name) —
// the guard must block on duplicate workload/container pairs. NULL keys
// do not collide (as with 12).
func TestMigrationDown_RecommendationSets28_GuardBlocksDuplicateWorkloadContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := newDownScenarioDB(t)
	migrateUpTo(t, connStr)
	pool := scenarioPool(t, connStr)
	// The FK chain: recommendation_sets.workload_id → workloads →
	// clusters → rh_accounts.
	_, err := pool.Exec(context.Background(), `
		INSERT INTO rh_accounts (id, account, org_id) VALUES (1, 'acct-28', 'org1');
		INSERT INTO clusters (id, tenant_id, source_id, cluster_uuid, cluster_alias)
		VALUES (1, 1, 'src', '11111111-1111-1111-1111-111111111111', 'c');
		INSERT INTO workloads (id, org_id, cluster_id, experiment_name, namespace, workload_type, workload_name, containers, metrics_upload_at)
		VALUES (123, 'org1', 1, 'exp', 'ns', 'Deployment', 'wl', ARRAY['ctr'], NOW())`)
	require.NoError(t, err, "the FK parent chain for the duplicate pair")
	_, err = pool.Exec(context.Background(), `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, workload_id, updated_at)
		VALUES ('org1', '11111111-1111-1111-1111-111111111111', 'ns', 'wl', 'Deployment', 'ctr', 'short', 'cost', 123, NOW()),
		       ('org1', '11111111-1111-1111-1111-111111111111', 'ns', 'wl', 'Deployment', 'ctr', 'medium', 'cost', 123, NOW())`)
	require.NoError(t, err, "duplicate workload/container pairs are legal under 28-up (constraint dropped)")
	forceVersion(t, connStr, 28)
	migrateDownTo(t, connStr, 27, "downgrade 000028 blocked")
}

// S8 (#846 review P2): the guard's recovery instructions are followable.
// A guard failure leaves the database dirty at the TARGET version having
// executed nothing (golang-migrate marks dirty before running; the guard
// is the first statement and the file runs in one transaction). The
// documented recovery — clean up deliberately, force the SOURCE version,
// re-run — must succeed end to end.
func TestMigrationDown_PVC64_GuardRecoverySequenceWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := newDownScenarioDB(t)
	migrateUpTo(t, connStr)
	pool := scenarioPool(t, connStr)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO pvc_recommendation_sets (org_id, cluster_uuid, namespace, persistentvolumeclaim, term)
		VALUES ('org1', '11111111-1111-1111-1111-111111111111', 'ns', 'pvc-a', 'short'),
		       ('org1', '11111111-1111-1111-1111-111111111111', 'ns', 'pvc-a', 'medium')`)
	require.NoError(t, err)
	forceVersion(t, connStr, 64)

	// The guard refuses.
	migrateDownTo(t, connStr, 63, "downgrade 000064 blocked")

	// Operator cleanup: keep one term per identity.
	_, err = pool.Exec(context.Background(), `
		DELETE FROM pvc_recommendation_sets
		WHERE ctid NOT IN (SELECT MIN(ctid) FROM pvc_recommendation_sets
			GROUP BY org_id, cluster_uuid, namespace, persistentvolumeclaim)`)
	require.NoError(t, err)

	// Recovery per the guard message: force the SOURCE version, re-run.
	forceVersion(t, connStr, 64)
	migrateDownTo(t, connStr, 63, "")

	var dropped bool
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT NOT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'pvc_recommendation_sets' AND column_name = 'term'
		)`).Scan(&dropped))
	assert.True(t, dropped, "after recovery the 64-down must complete — the term column is gone")
}

// S9 (#846 review P3): 67-down must not silently delete the
// business-hours container/namespace digest streams.
func TestMigrationDown_Digests67_GuardBlocksBusinessHours(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := newDownScenarioDB(t)
	migrateUpTo(t, connStr)
	pool := scenarioPool(t, connStr)
	_, err := pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS daily_container_digests_202601 PARTITION OF daily_container_digests
		FOR VALUES FROM ('2026-01-01') TO ('2026-02-01')`)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO daily_container_digests (bucket_date, org_id, cluster_uuid, namespace, workload, workload_type, container_name, schedule_type)
		VALUES ('2026-01-05', 'org1', '11111111-1111-1111-1111-111111111111', 'ns', 'wl', 'Deployment', 'ctr', 'all_hours'),
		       ('2026-01-05', 'org1', '11111111-1111-1111-1111-111111111111', 'ns', 'wl', 'Deployment', 'ctr', 'business_hours')`)
	require.NoError(t, err, "dual-stream container digests are legal under 67-up")
	forceVersion(t, connStr, 67)
	migrateDownTo(t, connStr, 66, "downgrade 000067 blocked")
}

// S10 (#846 review P3): 182-down must not silently delete the
// business-hours node digest stream.
func TestMigrationDown_Node182_GuardBlocksBusinessHours(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := newDownScenarioDB(t)
	migrateUpTo(t, connStr)
	pool := scenarioPool(t, connStr)
	_, err := pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS daily_node_digests_202601 PARTITION OF daily_node_digests
		FOR VALUES FROM ('2026-01-01') TO ('2026-02-01')`)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO daily_node_digests (bucket_date, org_id, cluster_uuid, node, schedule_type)
		VALUES ('2026-01-05', 'org1', '11111111-1111-1111-1111-111111111111', 'node-a', 'all_hours'),
		       ('2026-01-05', 'org1', '11111111-1111-1111-1111-111111111111', 'node-a', 'business_hours')`)
	require.NoError(t, err, "dual-stream node digests are legal under 182-up")
	forceVersion(t, connStr, 182)
	migrateDownTo(t, connStr, 181, "downgrade 000182 blocked")
}

// S11 (#846 review P3): 183-down must not silently delete the
// business-hours GPU digest stream.
func TestMigrationDown_GPU183_GuardBlocksBusinessHours(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := newDownScenarioDB(t)
	migrateUpTo(t, connStr)
	pool := scenarioPool(t, connStr)
	_, err := pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS gpu_container_digests_202601 PARTITION OF gpu_container_digests
		FOR VALUES FROM ('2026-01-01') TO ('2026-02-01')`)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO gpu_container_digests (interval_start, org_id, cluster_uuid, namespace, workload, container_name, gpu_model_name, schedule_type)
		VALUES ('2026-01-05 00:00:00', 'org1', '11111111-1111-1111-1111-111111111111', 'ns', 'wl', 'ctr', 'model-a', 'all_hours'),
		       ('2026-01-05 00:00:00', 'org1', '11111111-1111-1111-1111-111111111111', 'ns', 'wl', 'ctr', 'model-a', 'business_hours')`)
	require.NoError(t, err, "dual-stream GPU digests are legal under 183-up")
	forceVersion(t, connStr, 183)
	migrateDownTo(t, connStr, 182, "downgrade 000183 blocked")
}
