package engine

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SnapshotSettings holds resolved snapshot classification thresholds.
type SnapshotSettings struct {
	OrphanAgeDays       int
	NeverRestoredDays   int
	StaleDays           int
	RedundantThreshold  int
	CostPerGiBMonth     float64
	InventoryFreshHours int
}

// SnapshotRec is a classified snapshot recommendation.
type SnapshotRec struct {
	OrgID                string
	ClusterUUID          string
	Namespace            string
	SnapshotName         string
	SourcePVCName        string
	VolumeSnapshotClass  string
	StorageClass         string
	CreationTimestamp    time.Time
	RestoreSizeBytes     int64
	AgeDays              int
	SourcePVCExists      bool
	RestoredPVCCount     int
	ManagedBy            string
	RecommendationType   string
	EstimatedMonthlyCost *float32
	NotificationCodes    []int16
}

// pvcGroup holds the indices of snapshots sharing the same source PVC.
type pvcGroup struct {
	snapshots []int
}

// managedToolPrefixes maps label key prefixes to backup tool display names.
var managedToolPrefixes = map[string]string{
	"velero.io/":             "Velero",
	"k10.kasten.io/":         "Kasten K10",
	"backup.openshift.io/":   "OpenShift Backup",
	"triliovault.trilio.io/": "Trilio",
	"stash.appscode.com/":    "Stash/KubeStash",
}

// snapshotInventoryRow is the DB row shape from snapshot_inventory.
type snapshotInventoryRow struct {
	Namespace           string
	SnapshotName        string
	SourcePVCName       string
	VolumeSnapshotClass string
	StorageClass        string
	CreationTimestamp   time.Time
	RestoreSizeBytes    int64
	SourcePVCExists     bool
	RestoredPVCCount    int
	Labels              map[string]string
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

	// Group snapshots by source PVC for redundant detection
	pvcGroups := make(map[string]*pvcGroup) // key: namespace/source_pvc_name
	for i, snap := range inventory {
		if snap.SourcePVCName == "" {
			continue
		}
		key := snap.Namespace + "/" + snap.SourcePVCName
		g, ok := pvcGroups[key]
		if !ok {
			g = &pvcGroup{}
			pvcGroups[key] = g
		}
		g.snapshots = append(g.snapshots, i)
	}

	now := time.Now().UTC()
	recs := make([]SnapshotRec, 0, len(inventory))

	for i, snap := range inventory {
		ageDays := int(math.Floor(now.Sub(snap.CreationTimestamp).Hours() / 24))
		if ageDays < 0 {
			ageDays = 0
		}

		rec := SnapshotRec{
			OrgID:               orgID,
			ClusterUUID:         clusterUUID,
			Namespace:           snap.Namespace,
			SnapshotName:        snap.SnapshotName,
			SourcePVCName:       snap.SourcePVCName,
			VolumeSnapshotClass: snap.VolumeSnapshotClass,
			StorageClass:        snap.StorageClass,
			CreationTimestamp:   snap.CreationTimestamp,
			RestoreSizeBytes:    snap.RestoreSizeBytes,
			AgeDays:             ageDays,
			SourcePVCExists:     snap.SourcePVCExists,
			RestoredPVCCount:    snap.RestoredPVCCount,
		}

		// Compute cost estimate
		gib := float64(snap.RestoreSizeBytes) / (1024 * 1024 * 1024)
		cost := float32(gib * settings.CostPerGiBMonth)
		rec.EstimatedMonthlyCost = &cost

		// Detect managed backup tool
		managedBy := detectManagedTool(snap.Labels)
		rec.ManagedBy = managedBy

		// Classification with priority: Orphaned > Managed > Redundant > Stale > Never-restored > Active
		classification, codes := classifySnapshot(snap, i, ageDays, managedBy, settings, pvcGroups, inventory)
		rec.RecommendationType = classification
		rec.NotificationCodes = codes

		recs = append(recs, rec)
	}

	return recs, nil
}

func classifySnapshot(
	snap snapshotInventoryRow,
	idx, ageDays int,
	managedBy string,
	settings SnapshotSettings,
	pvcGroups map[string]*pvcGroup,
	inventory []snapshotInventoryRow,
) (string, []int16) {
	// 1. Orphaned: source PVC deleted AND age > orphan threshold
	if snap.SourcePVCName != "" && !snap.SourcePVCExists && ageDays > settings.OrphanAgeDays {
		return "orphaned", []int16{NotifSnapshotOrphaned}
	}

	// 2. Managed: backup tool detected
	if managedBy != "" {
		return "managed", []int16{NotifSnapshotManaged}
	}

	// 3. Redundant: only check if source PVC is known
	if snap.SourcePVCName != "" {
		key := snap.Namespace + "/" + snap.SourcePVCName
		if g, ok := pvcGroups[key]; ok && len(g.snapshots) > settings.RedundantThreshold {
			if ageDays > settings.StaleDays && !isAmongNewest(idx, g.snapshots, settings.RedundantThreshold, inventory) {
				return "redundant", []int16{NotifSnapshotRedundant}
			}
		}
	}

	// 4. Stale: age > stale threshold AND never restored
	if ageDays > settings.StaleDays && snap.RestoredPVCCount == 0 {
		return "stale", []int16{NotifSnapshotStale}
	}

	// 5. Never restored: age > never-restored threshold AND no restores
	if ageDays > settings.NeverRestoredDays && snap.RestoredPVCCount == 0 {
		return "never_restored", []int16{NotifSnapshotNeverUsed}
	}

	// 6. Active: recent OR has restores
	return "active", nil
}

// isAmongNewest checks whether the snapshot at `idx` is among the N most recent
// snapshots in its PVC group (sorted by creation_timestamp descending).
func isAmongNewest(idx int, groupIdxs []int, n int, inventory []snapshotInventoryRow) bool {
	if n >= len(groupIdxs) {
		return true
	}

	target := inventory[idx].CreationTimestamp
	newerCount := 0
	for _, gi := range groupIdxs {
		if gi == idx {
			continue
		}
		if inventory[gi].CreationTimestamp.After(target) {
			newerCount++
		}
	}
	// If fewer than N snapshots are newer, this one is among the N most recent
	return newerCount < n
}

func detectManagedTool(labels map[string]string) string {
	for key := range labels {
		for prefix, tool := range managedToolPrefixes {
			if strings.HasPrefix(key, prefix) {
				return tool
			}
		}
	}
	return ""
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
				recommendation_type, estimated_monthly_cost_usd,
				notification_codes, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, NOW())
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
				estimated_monthly_cost_usd = EXCLUDED.estimated_monthly_cost_usd,
				notification_codes = EXCLUDED.notification_codes,
				updated_at = NOW()`,
			rec.OrgID, rec.ClusterUUID, rec.Namespace, rec.SnapshotName,
			rec.SourcePVCName, rec.VolumeSnapshotClass, rec.StorageClass,
			rec.CreationTimestamp, rec.RestoreSizeBytes, rec.AgeDays,
			rec.SourcePVCExists, rec.RestoredPVCCount, rec.ManagedBy,
			rec.RecommendationType, rec.EstimatedMonthlyCost,
			rec.NotificationCodes,
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
