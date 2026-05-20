package ingestion

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// SnapshotRow represents a single parsed row from the snapshot inventory CSV.
type SnapshotRow struct {
	IntervalStart       time.Time
	IntervalEnd         time.Time
	Namespace           string
	SnapshotName        string
	SourcePVCName       string
	VolumeSnapshotClass string
	StorageClass        string
	CreationTimestamp   time.Time
	ReadyToUse          bool
	RestoreSizeBytes    int64
	SourcePVCExists     bool
	RestoredPVCCount    int
	Labels              map[string]string
}

type snapshotHeaderIdx struct {
	intervalStart       int
	intervalEnd         int
	namespace           int
	snapshotName        int
	sourcePVCName       int
	volumeSnapshotClass int
	storageclass        int
	creationTimestamp   int
	readyToUse          int
	restoreSizeBytes    int
	sourcePVCExists     int
	restoredPVCCount    int
	labels              int
}

func newSnapshotHeaderIdx() snapshotHeaderIdx {
	return snapshotHeaderIdx{
		intervalStart:       -1,
		intervalEnd:         -1,
		namespace:           -1,
		snapshotName:        -1,
		sourcePVCName:       -1,
		volumeSnapshotClass: -1,
		storageclass:        -1,
		creationTimestamp:   -1,
		readyToUse:         -1,
		restoreSizeBytes:    -1,
		sourcePVCExists:     -1,
		restoredPVCCount:    -1,
		labels:              -1,
	}
}

// ParseSnapshotRows parses the snapshot inventory CSV into SnapshotRow structs.
func ParseSnapshotRows(r io.Reader) ([]SnapshotRow, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading snapshot CSV header: %w", err)
	}

	idx := newSnapshotHeaderIdx()
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case "interval_start":
			idx.intervalStart = i
		case "interval_end":
			idx.intervalEnd = i
		case "namespace":
			idx.namespace = i
		case "snapshot_name":
			idx.snapshotName = i
		case "source_pvc_name":
			idx.sourcePVCName = i
		case "volume_snapshot_class":
			idx.volumeSnapshotClass = i
		case "storageclass":
			idx.storageclass = i
		case "creation_timestamp":
			idx.creationTimestamp = i
		case "ready_to_use":
			idx.readyToUse = i
		case "restore_size_bytes":
			idx.restoreSizeBytes = i
		case "source_pvc_exists":
			idx.sourcePVCExists = i
		case "restored_pvc_count":
			idx.restoredPVCCount = i
		case "labels":
			idx.labels = i
		}
	}

	if idx.namespace < 0 || idx.snapshotName < 0 || idx.creationTimestamp < 0 {
		return nil, fmt.Errorf("snapshot CSV missing required columns (namespace, snapshot_name, creation_timestamp)")
	}

	var rows []SnapshotRow
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading snapshot CSV row: %w", err)
		}

		row, parseErr := parseSnapshotRecord(record, idx)
		if parseErr != nil {
			logging.GetLogger().Debugf("skipping snapshot row: %v", parseErr)
			continue
		}
		if row.SnapshotName == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseSnapshotRecord(record []string, idx snapshotHeaderIdx) (SnapshotRow, error) {
	var row SnapshotRow
	var err error

	if idx.namespace >= 0 && idx.namespace < len(record) {
		row.Namespace = strings.TrimSpace(record[idx.namespace])
	}
	if idx.snapshotName >= 0 && idx.snapshotName < len(record) {
		row.SnapshotName = strings.TrimSpace(record[idx.snapshotName])
	}
	if idx.sourcePVCName >= 0 && idx.sourcePVCName < len(record) {
		row.SourcePVCName = strings.TrimSpace(record[idx.sourcePVCName])
	}
	if idx.volumeSnapshotClass >= 0 && idx.volumeSnapshotClass < len(record) {
		row.VolumeSnapshotClass = strings.TrimSpace(record[idx.volumeSnapshotClass])
	}
	if idx.storageclass >= 0 && idx.storageclass < len(record) {
		row.StorageClass = strings.TrimSpace(record[idx.storageclass])
	}
	if idx.creationTimestamp >= 0 && idx.creationTimestamp < len(record) {
		ts := strings.TrimSpace(record[idx.creationTimestamp])
		row.CreationTimestamp, err = time.Parse(time.RFC3339, ts)
		if err != nil {
			row.CreationTimestamp, err = time.Parse("2006-01-02 15:04:05+00:00", ts)
			if err != nil {
				return row, fmt.Errorf("parse creation_timestamp %q: %w", ts, err)
			}
		}
	}
	if idx.readyToUse >= 0 && idx.readyToUse < len(record) {
		row.ReadyToUse = parseBoolField(record[idx.readyToUse])
	}
	if idx.restoreSizeBytes >= 0 && idx.restoreSizeBytes < len(record) {
		row.RestoreSizeBytes = parseIntOrByteSeconds(record[idx.restoreSizeBytes])
	}
	if idx.sourcePVCExists >= 0 && idx.sourcePVCExists < len(record) {
		row.SourcePVCExists = parseBoolField(record[idx.sourcePVCExists])
	} else {
		row.SourcePVCExists = true
	}
	if idx.restoredPVCCount >= 0 && idx.restoredPVCCount < len(record) {
		if v, e := strconv.Atoi(strings.TrimSpace(record[idx.restoredPVCCount])); e == nil {
			row.RestoredPVCCount = v
		}
	}
	if idx.labels >= 0 && idx.labels < len(record) {
		raw := strings.TrimSpace(record[idx.labels])
		if raw != "" {
			row.Labels = make(map[string]string)
			_ = json.Unmarshal([]byte(raw), &row.Labels)
		}
	}
	if row.Labels == nil {
		row.Labels = make(map[string]string)
	}

	// Populate interval fields if available (not strictly required)
	if idx.intervalStart >= 0 && idx.intervalStart < len(record) {
		raw := strings.TrimSpace(record[idx.intervalStart])
		if raw != "" {
			ts, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				ts, err = time.Parse("2006-01-02 15:04:05+00:00", raw)
				if err != nil {
					return row, fmt.Errorf("parse interval_start %q: %w", raw, err)
				}
			}
			row.IntervalStart = ts
		}
	}
	if idx.intervalEnd >= 0 && idx.intervalEnd < len(record) {
		raw := strings.TrimSpace(record[idx.intervalEnd])
		if raw != "" {
			ts, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				ts, err = time.Parse("2006-01-02 15:04:05+00:00", raw)
				if err != nil {
					return row, fmt.Errorf("parse interval_end %q: %w", raw, err)
				}
			}
			row.IntervalEnd = ts
		}
	}

	return row, nil
}

func parseBoolField(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "true" || s == "1" || s == "yes"
}

// UpsertSnapshotInventory inserts snapshot rows into the staging table.
func UpsertSnapshotInventory(ctx context.Context, pool *pgxpool.Pool, rows []SnapshotRow, orgID, clusterUUID string) error {
	if len(rows) == 0 {
		return nil
	}

	for _, r := range rows {
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
	}
	return nil
}

// ProcessSnapshotCSV is the top-level entry point for snapshot CSV ingestion.
func ProcessSnapshotCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseSnapshotRows(r)
	if err != nil {
		return fmt.Errorf("parsing snapshot CSV: %w", err)
	}
	if len(rows) == 0 {
		logging.GetLogger().WithField("cluster_uuid", clusterUUID).Info("ProcessSnapshotCSV: no snapshot rows found")
		return nil
	}

	if err := UpsertSnapshotInventory(ctx, pool, rows, orgID, clusterUUID); err != nil {
		return fmt.Errorf("inserting snapshot inventory: %w", err)
	}

	logging.GetLogger().WithField("cluster_uuid", clusterUUID).Infof("ProcessSnapshotCSV: inserted %d snapshot inventory rows", len(rows))
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
