// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

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

func hcpRead(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	var v []string
	err := pool.QueryRow(context.Background(), `
		SELECT c.hcp_namespaces FROM clusters c
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1 AND c.cluster_uuid = $2`, topoTestOrg, topoTestCluster).Scan(&v)
	require.NoError(t, err)
	return v
}

// TestHCPNamespacesMigration_BackfillsEmpty proves pre-#590 rows gain the
// empty default when 000198 applies (fails before the migration exists).
func TestHCPNamespacesMigration_BackfillsEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsTo(t, connStr, 197)
	pool := topoTestPool(t, connStr)
	topoSeedCluster(t, pool)
	runMigrationsUp(t, connStr)
	assert.Empty(t, hcpRead(t, pool), "pre-#590 rows must backfill to an empty namespace list")
}

// TestReadHCPNamespaces_MissingColumn survives databases migrated before
// 000198 (SaaS rollout ordering): empty, nil error — never fail the run.
func TestReadHCPNamespaces_MissingColumn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsTo(t, connStr, 197)
	pool := topoTestPool(t, connStr)

	got, err := pgrec.ReadHCPNamespaces(context.Background(), pool, topoTestOrg, topoTestCluster, hcpStaleCutoff())
	require.NoError(t, err, "missing column must degrade, not fail")
	assert.Empty(t, got, "pre-000198 databases yield no namespaces (guardrail routing off)")
}

// TestUpdateReadHCPNamespaces_RoundtripAndOverwrite proves the pgrec writer
// stores lists, overwrites per cycle (empty list clears), tolerates nil
// (normalizes to empty for the NOT NULL column), and survives missing rows.
func TestUpdateReadHCPNamespaces_RoundtripAndOverwrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	topoSeedCluster(t, pool)
	ctx := context.Background()

	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, []string{"hc01-infra-hc01", "app-2"}))
	got, err := pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster, hcpStaleCutoff())
	require.NoError(t, err)
	assert.Equal(t, []string{"hc01-infra-hc01", "app-2"}, got)

	// Per-cycle overwrite: a new list replaces the old one entirely.
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, []string{"only-ns"}))
	got, err = pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster, hcpStaleCutoff())
	require.NoError(t, err)
	assert.Equal(t, []string{"only-ns"}, got)

	// Empty list clears stored namespaces; nil must not violate NOT NULL.
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, []string{}))
	got, err = pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster, hcpStaleCutoff())
	require.NoError(t, err)
	assert.Empty(t, got, "empty list must clear stored namespaces")
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, nil))
	got, err = pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster, hcpStaleCutoff())
	require.NoError(t, err)
	assert.Empty(t, got, "nil must normalize to an empty list, not NULL")

	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, "00000000-0000-0000-0000-000000000000", []string{"x"}),
		"missing row must be a silent no-op, not an error")
}

// TestReadHCPNamespaces_NULLDegradesEmpty proves a NULL storage value (e.g. an
// operator DROP NOT NULL on the column) reads back as empty — the COALESCE in
// the read guards the guardrail against NULL inputs.
func TestReadHCPNamespaces_NULLDegradesEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()
	topoSeedCluster(t, pool)

	_, err := pool.Exec(ctx, `ALTER TABLE clusters ALTER COLUMN hcp_namespaces DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE clusters SET hcp_namespaces = NULL WHERE cluster_uuid = $1`, topoTestCluster)
	require.NoError(t, err)

	got, err := pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster, hcpStaleCutoff())
	require.NoError(t, err)
	assert.Empty(t, got, "NULL hcp_namespaces must read as empty, never an error")
}

// TestLoadHCPNamespacesForRun_DegradesNotFails covers the product-side loader:
// persisted passthrough, empty on missing row or pre-000198 database.
func TestLoadHCPNamespacesForRun_DegradesNotFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()
	topoSeedCluster(t, pool)

	assert.Empty(t, loadHCPNamespacesForRun(ctx, pool, topoTestOrg, topoTestCluster), "fresh rows yield no namespaces")

	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, []string{"hc01-infra-hc01"}))
	assert.Equal(t, []string{"hc01-infra-hc01"}, loadHCPNamespacesForRun(ctx, pool, topoTestOrg, topoTestCluster))

	assert.Empty(t, loadHCPNamespacesForRun(ctx, pool, topoTestOrg, "00000000-0000-0000-0000-000000000000"),
		"missing row must yield empty, never an error")
}

// TestEnsureIngestClusterRow_CreatesRowWithSaneDefaults proves the #612
// bootstrap: no rows beforehand, one row afterwards carrying the message
// identity (org scoping intact), with topology unknown and HCP list empty
// until the per-cycle persists land.
func TestEnsureIngestClusterRow_CreatesRowWithSaneDefaults(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()

	before := time.Now().UTC()
	require.NoError(t, pgrec.EnsureIngestClusterRow(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, topoTestCluster, time.Time{}))

	var orgID, sourceID, alias string
	var lastReported time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT c.org_id, c.source_id, c.cluster_alias, c.last_reported_at FROM clusters c
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1 AND c.cluster_uuid = $2`, topoTestOrg, topoTestCluster).
		Scan(&orgID, &sourceID, &alias, &lastReported))
	assert.Equal(t, topoTestOrg, orgID, "denormalized org_id must match (loadClusterLastReportedAt reads it directly)")
	assert.Equal(t, topoTestSource, sourceID)
	assert.Equal(t, topoTestCluster, alias)
	assert.False(t, lastReported.Before(before), "zero lastReported must default to now")

	topo, err := pgrec.ReadClusterTopology(ctx, pool, topoTestOrg, topoTestCluster)
	require.NoError(t, err)
	assert.Equal(t, topology.TopologyUnknown, topo, "fresh row classifies unknown until persist")
	got, err := pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster, hcpStaleCutoff())
	require.NoError(t, err)
	assert.Empty(t, got, "fresh row carries no namespaces until persist")
}

// TestEnsureIngestClusterRow_IdempotentAndNonDestructive proves repeat
// calls (every enriched message) neither error nor duplicate the row, and
// never clobber state a previous cycle persisted.
func TestEnsureIngestClusterRow_IdempotentAndNonDestructive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()
	topoSeedCluster(t, pool)
	require.NoError(t, pgrec.UpdateClusterTopology(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, topology.TopologyHosted))
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, []string{"hc01-infra-hc01"}))

	before := time.Now().UTC()
	require.NoError(t, pgrec.EnsureIngestClusterRow(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, topoTestCluster, before))
	require.NoError(t, pgrec.EnsureIngestClusterRow(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, topoTestCluster, before))

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM clusters c
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1 AND c.cluster_uuid = $2`, topoTestOrg, topoTestCluster).Scan(&n))
	assert.Equal(t, 1, n, "ensure must never duplicate the row")

	topo, err := pgrec.ReadClusterTopology(ctx, pool, topoTestOrg, topoTestCluster)
	require.NoError(t, err)
	assert.Equal(t, topology.TopologyHosted, topo, "ensure must not reset persisted classification")
	got, err := pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster, hcpStaleCutoff())
	require.NoError(t, err)
	assert.Equal(t, []string{"hc01-infra-hc01"}, got, "ensure must not clear persisted namespaces")
}

// TestEnsureIngestClusterRow_RejectsBlankIdentity proves missing message
// identity fails fast (caller warn-and-continues) instead of writing a
// junk row no source-sync would ever claim.
func TestEnsureIngestClusterRow_RejectsBlankIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()

	for name, args := range map[string][4]string{
		"empty org":    {"", topoTestSource, topoTestCluster, topoTestCluster},
		"empty source": {topoTestOrg, "", topoTestCluster, topoTestCluster},
		"empty uuid":   {topoTestOrg, topoTestSource, "", topoTestCluster},
		"empty alias":  {topoTestOrg, topoTestSource, topoTestCluster, ""},
		"blank org":    {"  ", topoTestSource, topoTestCluster, topoTestCluster},
	} {
		a := args
		require.Error(t, pgrec.EnsureIngestClusterRow(ctx, pool, a[0], a[1], a[2], a[3], time.Now().UTC()), "%s must fail", name)
	}

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM clusters`).Scan(&n))
	assert.Equal(t, 0, n, "rejected ensures must write nothing")
}

// hcpStaleCutoff mirrors the loader's bound (MaxLookbackDays default) with
// an explicit timestamp so staleness arbitration tests stay deterministic
// at 30d-vs-14d and now-vs-14d margins.
func hcpStaleCutoff() time.Time {
	return time.Now().UTC().AddDate(0, 0, -14)
}

func ptrTime(v time.Time) *time.Time { return &v }

// seedHCPSecondRow inserts an additional clusters row sharing org+uuid
// under a different source (#613 multi-row arbitration fixture).
func seedHCPSecondRow(t *testing.T, pool *pgxpool.Pool, source, alias string, namespaces []string, lastReported *time.Time) {
	t.Helper()
	ctx := context.Background()
	var tenantID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM rh_accounts WHERE org_id = $1`, topoTestOrg).Scan(&tenantID))
	_, err := pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, org_id, source_id, cluster_uuid, cluster_alias, last_reported_at, hcp_namespaces)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		tenantID, topoTestOrg, source, topoTestCluster, alias, lastReported, namespaces)
	require.NoError(t, err)
}

// TestReadHCPNamespaces_StaleListDefersToFreshRow is the #613 regression
// test: a stale non-empty row (old source truth) must not shadow a fresher
// row, even an empty one; NULL-timestamp rows count as stale. Pre-fix the
// prefer-non-empty tiebreak ignores age and returns the stale list.
func TestReadHCPNamespaces_StaleListDefersToFreshRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()

	topoSeedCluster(t, pool)
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, []string{"hc01-infra-hc01"}))
	stale := time.Now().UTC().AddDate(0, 0, -30)
	_, err := pool.Exec(ctx, `UPDATE clusters SET last_reported_at = $1 WHERE cluster_uuid = $2 AND source_id = $3`,
		stale, topoTestCluster, topoTestSource)
	require.NoError(t, err)

	seedHCPSecondRow(t, pool, "other-source", "other-alias", []string{}, ptrTime(time.Now().UTC()))
	seedHCPSecondRow(t, pool, "null-source", "null-alias", []string{"ghost"}, nil)

	got, err := pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster, hcpStaleCutoff())
	require.NoError(t, err)
	assert.Empty(t, got, "stale (and NULL-timestamp) non-empty rows must defer to the fresh row")
}

// TestReadHCPNamespaces_FreshListStillWins guards the anti-flap property
// #613 must preserve: a fresh non-empty row keeps winning over a stale
// empty one, so facts-less re-registrations can't wipe known state.
func TestReadHCPNamespaces_FreshListStillWins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()

	topoSeedCluster(t, pool)
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, []string{"hc01-infra-hc01"}))
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `UPDATE clusters SET last_reported_at = $1 WHERE cluster_uuid = $2 AND source_id = $3`,
		now, topoTestCluster, topoTestSource)
	require.NoError(t, err)

	seedHCPSecondRow(t, pool, "other-source", "other-alias", []string{}, ptrTime(now.AddDate(0, 0, -30)))

	got, err := pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster, hcpStaleCutoff())
	require.NoError(t, err)
	assert.Equal(t, []string{"hc01-infra-hc01"}, got, "fresh lists must keep winning (no flap)")
}
