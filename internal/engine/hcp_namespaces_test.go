// Copyright 2026 Red Hat Inc.
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
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

	got, err := pgrec.ReadHCPNamespaces(context.Background(), pool, topoTestOrg, topoTestCluster)
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
	got, err := pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster)
	require.NoError(t, err)
	assert.Equal(t, []string{"hc01-infra-hc01", "app-2"}, got)

	// Per-cycle overwrite: a new list replaces the old one entirely.
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, []string{"only-ns"}))
	got, err = pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster)
	require.NoError(t, err)
	assert.Equal(t, []string{"only-ns"}, got)

	// Empty list clears stored namespaces; nil must not violate NOT NULL.
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, []string{}))
	got, err = pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster)
	require.NoError(t, err)
	assert.Empty(t, got, "empty list must clear stored namespaces")
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, nil))
	got, err = pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster)
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

	got, err := pgrec.ReadHCPNamespaces(ctx, pool, topoTestOrg, topoTestCluster)
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