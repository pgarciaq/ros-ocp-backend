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
	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

const (
	topoTestOrg     = "1234567"
	topoTestSource  = "topo-test-source"
	topoTestCluster = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

func topoTestPool(t *testing.T, connStr string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func topoSeedCluster(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, topoTestOrg)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, org_id, source_id, cluster_uuid, cluster_alias)
		SELECT ra.id, $1, $2, $3, $4 FROM rh_accounts ra WHERE ra.org_id = $1
		ON CONFLICT (tenant_id, source_id, cluster_uuid, cluster_alias) DO NOTHING`,
		topoTestOrg, topoTestSource, topoTestCluster, topoTestCluster)
	require.NoError(t, err)
}

func topoRead(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var v string
	err := pool.QueryRow(context.Background(), `
		SELECT c.cluster_topology FROM clusters c
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1 AND c.cluster_uuid = $2`, topoTestOrg, topoTestCluster).Scan(&v)
	require.NoError(t, err)
	return v
}

// TestClusterTopologyMigration_BackfillsUnknown proves pre-#407 rows gain
// the unknown default when 000196 applies (fails before the migration exists).
func TestClusterTopologyMigration_BackfillsUnknown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsTo(t, connStr, 195)
	pool := topoTestPool(t, connStr)
	topoSeedCluster(t, pool)
	runMigrationsUp(t, connStr)
	assert.Equal(t, "unknown", topoRead(t, pool))
}

// TestClusterTopologyCheckRejectsGarbage proves the CHECK vocabulary is enforced.
func TestClusterTopologyCheckRejectsGarbage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	topoSeedCluster(t, pool)
	_, err := pool.Exec(context.Background(), `
		UPDATE clusters SET cluster_topology = 'bogus'
		WHERE cluster_uuid = $1`, topoTestCluster)
	require.Error(t, err, "CHECK must reject values outside the ADR-0328 vocabulary")
}

// TestUpdateClusterTopology_PersistAndNormalize proves the pgrec writer stores
// classifications, normalizes bogus input to unknown, and tolerates missing rows.
func TestUpdateClusterTopology_PersistAndNormalize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()
	topoSeedCluster(t, pool)

	require.NoError(t, pgrec.UpdateClusterTopology(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, topology.TopologyManagement))
	assert.Equal(t, "management", topoRead(t, pool))

	require.NoError(t, pgrec.UpdateClusterTopology(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, topology.ClusterTopology("bogus")))
	assert.Equal(t, "unknown", topoRead(t, pool), "bogus input must normalize, never persist raw")

	require.NoError(t, pgrec.UpdateClusterTopology(ctx, pool, topoTestOrg, topoTestSource, "00000000-0000-0000-0000-000000000000", topology.TopologyHosted),
		"missing row must be a silent no-op, not an error")
}

// TestReadClusterTopology_ReturnsStored prefers the stored value, degrades to
// unknown on missing rows, and survives pre-migration databases.
func TestReadClusterTopology_ReturnsStored(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()
	topoSeedCluster(t, pool)

	got, err := pgrec.ReadClusterTopology(ctx, pool, topoTestOrg, topoTestCluster)
	require.NoError(t, err)
	assert.Equal(t, topology.TopologyUnknown, got, "fresh rows default unknown")

	require.NoError(t, pgrec.UpdateClusterTopology(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, topology.TopologyHosted))
	got, err = pgrec.ReadClusterTopology(ctx, pool, topoTestOrg, topoTestCluster)
	require.NoError(t, err)
	assert.Equal(t, topology.TopologyHosted, got)
}

// TestReadClusterTopology_MissingColumn survives databases migrated before
// 000196 (SaaS rollout ordering): unknown, nil error — never fail the run.
func TestReadClusterTopology_MissingColumn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsTo(t, connStr, 195)
	pool := topoTestPool(t, connStr)

	got, err := pgrec.ReadClusterTopology(context.Background(), pool, topoTestOrg, topoTestCluster)
	require.NoError(t, err, "missing column must degrade, not fail")
	assert.Equal(t, topology.TopologyUnknown, got)
}

// TestClusterTopologyForRun_WarnsAndDefaults covers the product-side wrapper
// used by node recommendation paths: stored value passthrough, unknown default.
func TestClusterTopologyForRun_WarnsAndDefaults(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	pool := topoTestPool(t, connStr)
	ctx := context.Background()
	topoSeedCluster(t, pool)

	assert.Equal(t, topology.TopologyUnknown, ClusterTopologyForRun(ctx, pool, topoTestOrg, topoTestCluster))
	require.NoError(t, pgrec.UpdateClusterTopology(ctx, pool, topoTestOrg, topoTestSource, topoTestCluster, topology.TopologyManagement))
	assert.Equal(t, topology.TopologyManagement, ClusterTopologyForRun(ctx, pool, topoTestOrg, topoTestCluster))
}
