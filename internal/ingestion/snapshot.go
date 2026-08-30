package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// SnapshotRow is a parsed VolumeSnapshot inventory CSV row (librobne/csv.SnapshotRow).
type SnapshotRow = libcsv.SnapshotRow

// forEachSnapshotCSVRow parses snapshot inventory CSV rows one at a time
// without retaining a full-slice copy. Processor ingest uses this; ParseSnapshotRows
// collects from it for tests.
func forEachSnapshotCSVRow(ctx context.Context, r io.Reader, fn func(SnapshotRow) error) (int, error) {
	count := 0
	skipped, err := libcsv.ForEachSnapshot(ctx, r, func(row libcsv.SnapshotRow) error {
		if err := fn(row); err != nil {
			return err
		}
		count++
		return nil
	})
	if skipped > 0 {
		metrics.IncCSVRowsSkipped("snapshot", skipped)
		logging.GetLogger().Warnf("ParseSnapshotRows: skipped %d malformed or invalid rows", skipped)
	}
	return count, err
}

// ParseSnapshotRows parses the snapshot inventory CSV into SnapshotRow structs.
// Processor ingest uses forEachSnapshotCSVRow; this collector is for tests and
// callers that still want a slice. Empty snapshot names are dropped. Bad
// timestamps are skipped.
func ParseSnapshotRows(r io.Reader) ([]SnapshotRow, error) {
	var rows []SnapshotRow
	_, err := forEachSnapshotCSVRow(context.Background(), r, func(row SnapshotRow) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

func insertSnapshotInventoryRow(ctx context.Context, pool *pgxpool.Pool, r SnapshotRow, orgID, clusterUUID string) error {
	labelsJSON, _ := json.Marshal(r.Labels)
	_, err := pool.Exec(ctx, `
			INSERT INTO snapshot_inventory (
				org_id, cluster_uuid, namespace, snapshot_name,
				source_pvc_name, volume_snapshot_class, storageclass,
				creation_timestamp, ready_to_use, restore_size_bytes,
				source_pvc_exists, restored_pvc_count, labels
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		orgID, clusterUUID, r.Namespace, r.SnapshotName,
		r.SourcePVCName, r.VolumeSnapshotClass, r.StorageClass,
		r.CreationTimestamp, r.ReadyToUse, r.RestoreSizeBytes,
		r.SourcePVCExists, r.RestoredPVCCount, labelsJSON,
	)
	if err != nil {
		return fmt.Errorf("inserting snapshot inventory %s/%s: %w", r.Namespace, r.SnapshotName, err)
	}
	return nil
}

// UpsertSnapshotInventory inserts snapshot rows into the staging table.
// Processor ingest uses ProcessSnapshotCSV (one insert per callback); this
// slice loop remains for tests.
func UpsertSnapshotInventory(ctx context.Context, pool *pgxpool.Pool, rows []SnapshotRow, orgID, clusterUUID string) error {
	if len(rows) == 0 {
		return nil
	}

	for _, r := range rows {
		if err := insertSnapshotInventoryRow(ctx, pool, r, orgID, clusterUUID); err != nil {
			return err
		}
	}
	return nil
}

// ProcessSnapshotCSV is the top-level entry point for snapshot CSV ingestion.
// Rows are inserted one at a time (ADR-0230 append-only). This does not collapse
// to LatestSnapshotInventory — that is CLI classify, not processor ingest.
func ProcessSnapshotCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	inserted := 0
	_, err := forEachSnapshotCSVRow(ctx, r, func(row SnapshotRow) error {
		if err := insertSnapshotInventoryRow(ctx, pool, row, orgID, clusterUUID); err != nil {
			return err
		}
		inserted++
		return nil
	})
	if err != nil {
		return err
	}
	if inserted == 0 {
		logging.GetLogger().WithField("cluster_uuid", clusterUUID).Info("ProcessSnapshotCSV: no snapshot rows found")
		return nil
	}

	logging.GetLogger().WithField("cluster_uuid", clusterUUID).Infof("ProcessSnapshotCSV: inserted %d snapshot inventory rows", inserted)
	return nil
}

// PurgeSnapshotInventory removes snapshot inventory rows older than the retention period.
func PurgeSnapshotInventory(ctx context.Context, pool *pgxpool.Pool, retentionHours int) (int64, error) {
	tag, err := pool.Exec(ctx, `DELETE FROM snapshot_inventory WHERE ingested_at < NOW() - ($1 || ' hours')::INTERVAL`, strconv.Itoa(retentionHours))
	if err != nil {
		return 0, fmt.Errorf("purging snapshot inventory: %w", err)
	}
	return tag.RowsAffected(), nil
}
