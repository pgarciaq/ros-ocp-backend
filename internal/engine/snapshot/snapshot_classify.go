package snapshot

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	libsnap "github.com/redhatinsights/ros-ocp-backend/librobne/snapshot"
)

type SnapshotSettings = libsnap.SnapshotSettings
type SnapshotRec = libsnap.SnapshotRec
type InventoryRow = libsnap.InventoryRow
type snapshotInventoryRow = libsnap.InventoryRow

// ClassifySnapshotInventory classifies already-loaded inventory rows with no pool.
// Persist SQL stays in WriteSnapshotRecommendations.
func ClassifySnapshotInventory(inventory []snapshotInventoryRow, orgID, clusterUUID string, settings SnapshotSettings, now time.Time) []SnapshotRec {
	return libsnap.ClassifySnapshotInventory(inventory, orgID, clusterUUID, settings, now)
}

// ClassifySnapshots reads the latest inventory for a cluster and produces
// classified recommendations.
func ClassifySnapshots(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, settings SnapshotSettings) ([]SnapshotRec, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT ON (namespace, snapshot_name)
			namespace, snapshot_name, source_pvc_name,
			volume_snapshot_class, storageclass, creation_timestamp,
			restore_size_bytes, source_pvc_exists, restored_pvc_count, labels
		FROM snapshot_inventory
		WHERE org_id = $1 AND cluster_uuid = $2
			AND ingested_at >= NOW() - ($3 * INTERVAL '1 hour')
		ORDER BY namespace, snapshot_name, ingested_at DESC`,
		orgID, clusterUUID, snapshotInventoryFreshHours(settings),
	)
	if err != nil {
		return nil, fmt.Errorf("querying snapshot inventory: %w", err)
	}
	defer rows.Close()

	var inventory []snapshotInventoryRow
	for rows.Next() {
		var r snapshotInventoryRow
		if err := rows.Scan(
			&r.Namespace, &r.SnapshotName, &r.SourcePVCName,
			&r.VolumeSnapshotClass, &r.StorageClass, &r.CreationTimestamp,
			&r.RestoreSizeBytes, &r.SourcePVCExists, &r.RestoredPVCCount, &r.Labels,
		); err != nil {
			return nil, fmt.Errorf("scanning snapshot inventory row: %w", err)
		}
		if r.Labels == nil {
			r.Labels = make(map[string]string)
		}
		inventory = append(inventory, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating snapshot inventory: %w", err)
	}

	if len(inventory) == 0 {
		return nil, nil
	}

	return ClassifySnapshotInventory(inventory, orgID, clusterUUID, settings, time.Now().UTC()), nil
}

// WriteSnapshotRecommendations upserts classified snapshot recommendations.
func WriteSnapshotRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []SnapshotRec) error {
	for _, rec := range recs {
		_, err := pool.Exec(ctx, `
			INSERT INTO snapshot_recommendation_sets (
				org_id, cluster_uuid, namespace, snapshot_name,
				source_pvc_name, volume_snapshot_class, storageclass,
				creation_timestamp, restore_size_bytes, age_days,
				source_pvc_exists, restored_pvc_count, managed_by,
				recommendation_type, estimated_cost_cents,
				notification_codes, updated_at,`+engine.SnapshotExplSQLColumns+`
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, NOW(), $17, $18, $19)
			ON CONFLICT (org_id, cluster_uuid, namespace, snapshot_name)
			DO UPDATE SET
				source_pvc_name = EXCLUDED.source_pvc_name,
				volume_snapshot_class = EXCLUDED.volume_snapshot_class,
				storageclass = EXCLUDED.storageclass,
				creation_timestamp = EXCLUDED.creation_timestamp,
				restore_size_bytes = EXCLUDED.restore_size_bytes,
				age_days = EXCLUDED.age_days,
				source_pvc_exists = EXCLUDED.source_pvc_exists,
				restored_pvc_count = EXCLUDED.restored_pvc_count,
				managed_by = EXCLUDED.managed_by,
				recommendation_type = EXCLUDED.recommendation_type,
				estimated_cost_cents = EXCLUDED.estimated_cost_cents,
				notification_codes = EXCLUDED.notification_codes,
				updated_at = NOW(),`+engine.SnapshotExplUpdateSet,
			append([]any{
				rec.OrgID, rec.ClusterUUID, rec.Namespace, rec.SnapshotName,
				rec.SourcePVCName, rec.VolumeSnapshotClass, rec.StorageClass,
				rec.CreationTimestamp, rec.RestoreSizeBytes, rec.AgeDays,
				rec.SourcePVCExists, rec.RestoredPVCCount, rec.ManagedBy,
				rec.RecommendationType, rec.EstimatedCostCents,
				rec.NotificationCodes,
			}, engine.AppendSnapshotExplArgs(nil, rec.Expl)...)...,
		)
		if err != nil {
			return fmt.Errorf("upserting snapshot recommendation %s/%s: %w", rec.Namespace, rec.SnapshotName, err)
		}
	}
	return nil
}

func snapshotInventoryFreshHours(settings SnapshotSettings) int {
	if settings.InventoryFreshHours > 0 {
		return settings.InventoryFreshHours
	}
	return SnapshotSettingsDefaults.InventoryFreshHours
}

// ReconcileSnapshotRecommendations deletes rows from snapshot_recommendation_sets
// (ROS resource optimization data only; unrelated to Koku tables) when a snapshot
// no longer appears in snapshot_inventory within the fresh window.
//
// Inventory gating (staleGraceHours from ROS_SNAPSHOT_STALE_GRACE_HOURS, default 48):
//
//   - Normal path: rows exist in snapshot_inventory within freshHours → run
//     DELETE for recommendations whose snapshot is absent from that fresh inventory.
//
//   - Transient gap: rows exist within staleGraceHours but none within freshHours → skip
//     reconcile. Ingest may have paused briefly; deleting would risk wiping valid rows
//     because NOT EXISTS against an empty fresh inventory would match everything.
//
//   - Stale / abandoned cluster: no snapshot_inventory rows within staleGraceHours →
//     run DELETE anyway. The cluster has stopped reporting; clearing orphaned ROS rows
//     is preferable to leaving stale recommendations indefinitely.
func ReconcileSnapshotRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, freshHours, staleGraceHours int) (int64, error) {
	if freshHours <= 0 {
		freshHours = SnapshotSettingsDefaults.InventoryFreshHours
	}
	if staleGraceHours <= 0 {
		staleGraceHours = 48
	}

	var cntFresh, cntGrace int64
	err := pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM snapshot_inventory
			 WHERE org_id = $1 AND cluster_uuid = $2::uuid
			   AND ingested_at >= NOW() - ($3 * INTERVAL '1 hour')),
			(SELECT COUNT(*) FROM snapshot_inventory
			 WHERE org_id = $1 AND cluster_uuid = $2::uuid
			   AND ingested_at >= NOW() - ($4 * INTERVAL '1 hour'))`,
		orgID, clusterUUID, freshHours, staleGraceHours,
	).Scan(&cntFresh, &cntGrace)
	if err != nil {
		return 0, fmt.Errorf("count snapshot inventory: %w", err)
	}

	if cntGrace > 0 && cntFresh == 0 {
		return 0, nil
	}

	tag, err := pool.Exec(ctx, `
		DELETE FROM snapshot_recommendation_sets srs
		WHERE srs.org_id = $1 AND srs.cluster_uuid = $2::uuid
		  AND NOT EXISTS (
			SELECT 1 FROM snapshot_inventory si
			WHERE si.org_id = $1 AND si.cluster_uuid = $2::uuid
			  AND si.namespace = srs.namespace
			  AND si.snapshot_name = srs.snapshot_name
			  AND si.ingested_at >= NOW() - ($3 * INTERVAL '1 hour')
		)`, orgID, clusterUUID, freshHours)
	if err != nil {
		return 0, fmt.Errorf("reconciling snapshot recommendations: %w", err)
	}
	return tag.RowsAffected(), nil
}
