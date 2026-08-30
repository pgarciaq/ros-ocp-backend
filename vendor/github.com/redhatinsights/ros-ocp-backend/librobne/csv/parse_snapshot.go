package csv

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/snapshot"
)

// SnapshotRow is one interval from a VolumeSnapshot inventory CSV.
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

// MissingSnapshotColumnsError lists required snapshot-inventory headers that were absent.
type MissingSnapshotColumnsError struct {
	Columns []string
}

func (e *MissingSnapshotColumnsError) Error() string {
	return fmt.Sprintf("not a snapshot inventory CSV (missing columns: %s)", strings.Join(e.Columns, ", "))
}

type snapshotColumnIndex struct {
	intervalStart, intervalEnd int
	namespace, snapshotName    int
	sourcePVCName              int
	volumeSnapshotClass        int
	storageclass               int
	creationTimestamp          int
	readyToUse                 int
	restoreSizeBytes           int
	sourcePVCExists            int
	restoredPVCCount           int
	labels                     int
}

func newSnapshotColumnIndex() snapshotColumnIndex {
	return snapshotColumnIndex{
		intervalStart: -1, intervalEnd: -1,
		namespace: -1, snapshotName: -1,
		sourcePVCName: -1, volumeSnapshotClass: -1, storageclass: -1,
		creationTimestamp: -1, readyToUse: -1, restoreSizeBytes: -1,
		sourcePVCExists: -1, restoredPVCCount: -1, labels: -1,
	}
}

func buildSnapshotColumnIndex(header []string) (snapshotColumnIndex, error) {
	idx := newSnapshotColumnIndex()
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
	var missing []string
	if idx.namespace < 0 {
		missing = append(missing, "namespace")
	}
	if idx.snapshotName < 0 {
		missing = append(missing, "snapshot_name")
	}
	if idx.creationTimestamp < 0 {
		missing = append(missing, "creation_timestamp")
	}
	if len(missing) > 0 {
		return idx, &MissingSnapshotColumnsError{Columns: missing}
	}
	return idx, nil
}

// ForEachSnapshot parses a VolumeSnapshot inventory CSV one record at a time
// without retaining the full file. Empty snapshot names are dropped (not
// counted in skipped). Bad timestamps are skipped (counted in skipped).
// Structural CSV errors still fail. ctx is checked every 10_000 successfully
// parsed rows (same cadence as ForEachRow). A nil ctx is treated as Background.
//
// The operator must not import this package (ADR-0305).
func ForEachSnapshot(ctx context.Context, r io.Reader, fn func(SnapshotRow) error) (skipped int, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reader := csv.NewReader(r)
	reader.ReuseRecord = true
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading header: %w", err)
	}
	idx, err := buildSnapshotColumnIndex(header)
	if err != nil {
		return 0, err
	}
	accepted := 0
	lineNum := 1
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return skipped, nil
		}
		if err != nil {
			return skipped, fmt.Errorf("reading line %d: %w", lineNum+1, err)
		}
		lineNum++
		row, parseErr := parseSnapshotRecord(record, idx)
		if parseErr != nil {
			skipped++
			continue
		}
		if row.SnapshotName == "" {
			continue
		}
		if err := fn(row); err != nil {
			return skipped, err
		}
		accepted++
		if accepted%10000 == 0 {
			if err := ctx.Err(); err != nil {
				return skipped, err
			}
		}
	}
}

// ParseSnapshotRows reads a VolumeSnapshot inventory CSV. Empty snapshot names
// are dropped. Unparseable timestamps/numbers are skipped (counted in skipped).
// Structural CSV errors still fail. CLI batch load uses this; processor ingest
// uses ForEachSnapshot.
func ParseSnapshotRows(r io.Reader) (rows []SnapshotRow, skipped int, err error) {
	rows = make([]SnapshotRow, 0, 64)
	skipped, err = ForEachSnapshot(context.Background(), r, func(row SnapshotRow) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, skipped, nil
	}
	return rows, skipped, nil
}

func parseSnapshotRecord(record []string, idx snapshotColumnIndex) (SnapshotRow, error) {
	var row SnapshotRow
	row.Namespace = strings.TrimSpace(cell(record, idx.namespace))
	row.SnapshotName = strings.TrimSpace(cell(record, idx.snapshotName))
	row.SourcePVCName = strings.TrimSpace(cell(record, idx.sourcePVCName))
	row.VolumeSnapshotClass = strings.TrimSpace(cell(record, idx.volumeSnapshotClass))
	row.StorageClass = strings.TrimSpace(cell(record, idx.storageclass))

	ts := strings.TrimSpace(cell(record, idx.creationTimestamp))
	created, err := parseFlexibleTimestamp(ts)
	if err != nil {
		return row, fmt.Errorf("parse creation_timestamp %q: %w", ts, err)
	}
	row.CreationTimestamp = created

	if idx.readyToUse >= 0 {
		row.ReadyToUse = parseSnapshotBool(cell(record, idx.readyToUse))
	}
	if idx.restoreSizeBytes >= 0 {
		row.RestoreSizeBytes = parseIntOrByteSeconds(cell(record, idx.restoreSizeBytes))
	}
	if idx.sourcePVCExists >= 0 && strings.TrimSpace(cell(record, idx.sourcePVCExists)) != "" {
		row.SourcePVCExists = parseSnapshotBool(cell(record, idx.sourcePVCExists))
	} else {
		row.SourcePVCExists = true
	}
	if idx.restoredPVCCount >= 0 {
		raw := strings.TrimSpace(cell(record, idx.restoredPVCCount))
		if raw != "" {
			if v, e := strconv.Atoi(raw); e == nil {
				row.RestoredPVCCount = v
			}
		}
	}
	if idx.labels >= 0 {
		raw := strings.TrimSpace(cell(record, idx.labels))
		if raw != "" {
			row.Labels = make(map[string]string)
			_ = json.Unmarshal([]byte(raw), &row.Labels)
		}
	}
	if row.Labels == nil {
		row.Labels = map[string]string{}
	}
	if idx.intervalStart >= 0 {
		raw := strings.TrimSpace(cell(record, idx.intervalStart))
		if raw != "" {
			t, err := parseFlexibleTimestamp(raw)
			if err != nil {
				return row, fmt.Errorf("parse interval_start %q: %w", raw, err)
			}
			row.IntervalStart = t
		}
	}
	if idx.intervalEnd >= 0 {
		raw := strings.TrimSpace(cell(record, idx.intervalEnd))
		if raw != "" {
			t, err := parseFlexibleTimestamp(raw)
			if err != nil {
				return row, fmt.Errorf("parse interval_end %q: %w", raw, err)
			}
			row.IntervalEnd = t
		}
	}
	return row, nil
}

func parseSnapshotBool(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "true" || s == "1" || s == "yes"
}

// LatestSnapshotInventory keeps the latest hourly inventory row per
// (namespace, snapshot_name) by interval_end, then interval_start, then
// creation_timestamp. ClassifySnapshotInventory must not see one snapshot per hour.
func LatestSnapshotInventory(rows []SnapshotRow) []snapshot.InventoryRow {
	type key struct{ ns, name string }
	best := make(map[key]SnapshotRow, len(rows))
	for _, r := range rows {
		k := key{r.Namespace, r.SnapshotName}
		prev, ok := best[k]
		if !ok || snapshotRowNewer(r, prev) {
			best[k] = r
		}
	}
	out := make([]snapshot.InventoryRow, 0, len(best))
	for _, r := range best {
		out = append(out, snapshot.InventoryRow{
			Namespace:           r.Namespace,
			SnapshotName:        r.SnapshotName,
			SourcePVCName:       r.SourcePVCName,
			VolumeSnapshotClass: r.VolumeSnapshotClass,
			StorageClass:        r.StorageClass,
			CreationTimestamp:   r.CreationTimestamp,
			RestoreSizeBytes:    r.RestoreSizeBytes,
			SourcePVCExists:     r.SourcePVCExists,
			RestoredPVCCount:    r.RestoredPVCCount,
			Labels:              r.Labels,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].SnapshotName < out[j].SnapshotName
	})
	return out
}

func snapshotRowNewer(a, b SnapshotRow) bool {
	if !a.IntervalEnd.Equal(b.IntervalEnd) {
		return a.IntervalEnd.After(b.IntervalEnd)
	}
	if !a.IntervalStart.Equal(b.IntervalStart) {
		return a.IntervalStart.After(b.IntervalStart)
	}
	return a.CreationTimestamp.After(b.CreationTimestamp)
}
