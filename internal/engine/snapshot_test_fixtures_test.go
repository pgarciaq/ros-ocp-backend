package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// SnapshotFixtureNames identifies the canonical test snapshots seeded by SeedSnapshotTestInventory.
type SnapshotFixtureNames struct {
	Orphaned      string
	Stale         string
	NeverRestored string
	Redundant     string
	Healthy       string
	NamespaceA    string
	NamespaceB    string
}

// DefaultSnapshotFixtureNames returns stable snapshot names used across snapshot recommendation tests.
func DefaultSnapshotFixtureNames() SnapshotFixtureNames {
	return SnapshotFixtureNames{
		Orphaned:      "snap-orphaned",
		Stale:         "snap-stale",
		NeverRestored: "snap-never-restored",
		Redundant:     "snap-redundant-old",
		Healthy:       "snap-healthy-managed",
		NamespaceA:    "snapshot-ns-a",
		NamespaceB:    "snapshot-ns-b",
	}
}

type snapshotInventorySeed struct {
	namespace         string
	snapshotName      string
	sourcePVCName     string
	creationTimestamp time.Time
	sourcePVCExists   bool
	restoredPVCCount  int
	labels            map[string]string
}

// SeedSnapshotTestInventory inserts synthetic VolumeSnapshot inventory rows for integration tests.
//
// Five primary snapshots across two namespaces:
//   - orphaned: source PVC deleted, age > orphan threshold
//   - stale: age > stale threshold, never restored
//   - never restored: age between never-restored and stale thresholds
//   - redundant: oldest of four snapshots sharing one PVC (requires siblings below)
//   - healthy/managed: Velero-managed backup (negative case for deletion)
//
// Three additional sibling snapshots support redundant classification (same PVC as redundant).
func SeedSnapshotTestInventory(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) SnapshotFixtureNames {
	t.Helper()
	names := DefaultSnapshotFixtureNames()
	now := time.Now().UTC()

	rows := []snapshotInventorySeed{
		{
			namespace:         names.NamespaceA,
			snapshotName:      names.Orphaned,
			sourcePVCName:     "pvc-deleted",
			creationTimestamp: now.Add(-30 * 24 * time.Hour),
			sourcePVCExists:   false,
			restoredPVCCount:  0,
			labels:            map[string]string{},
		},
		{
			namespace:         names.NamespaceA,
			snapshotName:      names.Stale,
			sourcePVCName:     "pvc-stale",
			creationTimestamp: now.Add(-120 * 24 * time.Hour),
			sourcePVCExists:   true,
			restoredPVCCount:  0,
			labels:            map[string]string{},
		},
		{
			namespace:         names.NamespaceB,
			snapshotName:      names.NeverRestored,
			sourcePVCName:     "pvc-forgotten",
			creationTimestamp: now.Add(-45 * 24 * time.Hour),
			sourcePVCExists:   true,
			restoredPVCCount:  0,
			labels:            map[string]string{},
		},
		{
			namespace:         names.NamespaceB,
			snapshotName:      names.Healthy,
			sourcePVCName:     "pvc-managed",
			creationTimestamp: now.Add(-100 * 24 * time.Hour),
			sourcePVCExists:   true,
			restoredPVCCount:  0,
			labels:            map[string]string{"velero.io/backup-name": "daily-backup"},
		},
		// Redundant group: four snapshots on the same PVC; oldest is classified redundant.
		{
			namespace:         names.NamespaceB,
			snapshotName:      "snap-redundant-newest",
			sourcePVCName:     "pvc-redundant",
			creationTimestamp: now.Add(-2 * 24 * time.Hour),
			sourcePVCExists:   true,
			restoredPVCCount:  0,
			labels:            map[string]string{},
		},
		{
			namespace:         names.NamespaceB,
			snapshotName:      "snap-redundant-recent",
			sourcePVCName:     "pvc-redundant",
			creationTimestamp: now.Add(-30 * 24 * time.Hour),
			sourcePVCExists:   true,
			restoredPVCCount:  0,
			labels:            map[string]string{},
		},
		{
			namespace:         names.NamespaceB,
			snapshotName:      "snap-redundant-mid",
			sourcePVCName:     "pvc-redundant",
			creationTimestamp: now.Add(-95 * 24 * time.Hour),
			sourcePVCExists:   true,
			restoredPVCCount:  0,
			labels:            map[string]string{},
		},
		{
			namespace:         names.NamespaceB,
			snapshotName:      names.Redundant,
			sourcePVCName:     "pvc-redundant",
			creationTimestamp: now.Add(-150 * 24 * time.Hour),
			sourcePVCExists:   true,
			restoredPVCCount:  0,
			labels:            map[string]string{},
		},
	}

	ctx := context.Background()
	for _, row := range rows {
		insertSnapshotInventoryRow(t, ctx, pool, orgID, clusterUUID, row)
	}
	return names
}

func insertSnapshotInventoryRow(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	row snapshotInventorySeed,
) {
	t.Helper()
	labels := row.labels
	if labels == nil {
		labels = map[string]string{}
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_inventory (
			org_id, cluster_uuid, namespace, snapshot_name,
			source_pvc_name, creation_timestamp, ingested_at,
			source_pvc_exists, restored_pvc_count, labels,
			restore_size_bytes, ready_to_use
		) VALUES (
			$1, $2::uuid, $3, $4,
			$5, $6, NOW(),
			$7, $8, $9::jsonb,
			1073741824, true
		)`,
		orgID, clusterUUID, row.namespace, row.snapshotName,
		row.sourcePVCName, row.creationTimestamp,
		row.sourcePVCExists, row.restoredPVCCount, labelsJSON(t, labels),
	)
	require.NoError(t, err)
}

func labelsJSON(t *testing.T, labels map[string]string) string {
	t.Helper()
	if labels == nil {
		labels = map[string]string{}
	}
	b, err := json.Marshal(labels)
	require.NoError(t, err)
	return string(b)
}

func snapshotRecByName(recs []SnapshotRec, namespace, name string) (SnapshotRec, bool) {
	for _, rec := range recs {
		if rec.Namespace == namespace && rec.SnapshotName == name {
			return rec, true
		}
	}
	return SnapshotRec{}, false
}

func classifySnapshotTestInventory(t *testing.T, pool *pgxpool.Pool, settings SnapshotSettings) []SnapshotRec {
	t.Helper()
	recs, err := ClassifySnapshots(context.Background(), pool, testutil.TestOrgID, testutil.TestClusterUUID, settings)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	return recs
}
