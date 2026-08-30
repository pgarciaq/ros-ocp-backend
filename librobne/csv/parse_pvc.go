package csv

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// PVCRow is one interval from a storage / PVC CSV (NISE ocp_storage_usage or operator ros-openshift-storage).
type PVCRow struct {
	IntervalStart         time.Time
	IntervalEnd           time.Time
	Namespace             string
	Pod                   string
	VMName                string
	PersistentVolumeClaim string
	PersistentVolume      string
	StorageClass          string
	CapacityBytes         int64
	RequestByteSeconds    int64
	UsageByteSeconds      int64
}

// MissingStorageColumnsError lists required storage headers that were absent.
type MissingStorageColumnsError struct {
	Columns []string
}

func (e *MissingStorageColumnsError) Error() string {
	return fmt.Sprintf("not a storage CSV (missing columns: %s)", strings.Join(e.Columns, ", "))
}

type pvcColumnIndex struct {
	intervalStart, intervalEnd int
	namespace, pod, vmName     int
	persistentvolumeclaim      int
	persistentvolume           int
	storageclass               int
	capacityBytes              int
	capacityByteSeconds        int
	requestByteSeconds         int
	usageByteSeconds           int
}

func newPVCColumnIndex() pvcColumnIndex {
	return pvcColumnIndex{
		intervalStart: -1, intervalEnd: -1, namespace: -1, pod: -1, vmName: -1,
		persistentvolumeclaim: -1, persistentvolume: -1, storageclass: -1,
		capacityBytes: -1, capacityByteSeconds: -1, requestByteSeconds: -1, usageByteSeconds: -1,
	}
}

func buildPVCColumnIndex(header []string) (pvcColumnIndex, error) {
	idx := newPVCColumnIndex()
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case "interval_start":
			idx.intervalStart = i
		case "interval_end":
			idx.intervalEnd = i
		case "namespace":
			idx.namespace = i
		case "pod":
			idx.pod = i
		case "vm_name":
			idx.vmName = i
		case "persistentvolumeclaim":
			idx.persistentvolumeclaim = i
		case "persistentvolume":
			idx.persistentvolume = i
		case "storageclass":
			idx.storageclass = i
		case "persistentvolumeclaim_capacity_bytes":
			idx.capacityBytes = i
		case "persistentvolumeclaim_capacity_byte_seconds":
			idx.capacityByteSeconds = i
		case "volume_request_storage_byte_seconds":
			idx.requestByteSeconds = i
		case "persistentvolumeclaim_usage_byte_seconds":
			idx.usageByteSeconds = i
		}
	}
	var missing []string
	if idx.intervalStart < 0 {
		missing = append(missing, "interval_start")
	}
	if idx.namespace < 0 {
		missing = append(missing, "namespace")
	}
	if idx.persistentvolumeclaim < 0 {
		missing = append(missing, "persistentvolumeclaim")
	}
	if len(missing) > 0 {
		return idx, &MissingStorageColumnsError{Columns: missing}
	}
	return idx, nil
}

// ForEachPVC parses a storage CSV one record at a time without retaining the
// full file. Empty PVC names are dropped (not counted in skipped). Bad
// timestamps are skipped (counted in skipped). Structural CSV errors still
// fail. ctx is checked every 10_000 successfully parsed rows (same cadence as
// ForEachRow). A nil ctx is treated as Background.
//
// The operator must not import this package (ADR-0305).
func ForEachPVC(ctx context.Context, r io.Reader, fn func(PVCRow) error) (skipped int, err error) {
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
	idx, err := buildPVCColumnIndex(header)
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
		row, parseErr := parsePVCRecord(record, idx)
		if parseErr != nil {
			skipped++
			continue
		}
		if row.PersistentVolumeClaim == "" {
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

// ParsePVCRows reads a storage CSV. Empty PVC names are dropped. Bad timestamps
// are skipped (counted in skipped). Structural CSV errors still fail.
// CLI batch load uses this; processor ingest uses ForEachPVC.
func ParsePVCRows(r io.Reader) (rows []PVCRow, skipped int, err error) {
	rows = make([]PVCRow, 0, 256)
	skipped, err = ForEachPVC(context.Background(), r, func(row PVCRow) error {
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

func parsePVCRecord(record []string, idx pvcColumnIndex) (PVCRow, error) {
	var row PVCRow
	var err error
	row.IntervalStart, err = parseFlexibleTimestamp(cell(record, idx.intervalStart))
	if err != nil {
		return row, err
	}
	if s := cell(record, idx.intervalEnd); s != "" {
		row.IntervalEnd, err = parseFlexibleTimestamp(s)
		if err != nil {
			return row, err
		}
	}
	row.Namespace = cell(record, idx.namespace)
	row.Pod = cell(record, idx.pod)
	row.VMName = cell(record, idx.vmName)
	row.PersistentVolumeClaim = cell(record, idx.persistentvolumeclaim)
	row.PersistentVolume = cell(record, idx.persistentvolume)
	row.StorageClass = cell(record, idx.storageclass)
	row.CapacityBytes = parseIntOrByteSeconds(cell(record, idx.capacityBytes))
	if row.CapacityBytes == 0 {
		row.CapacityBytes = parseIntOrByteSeconds(cell(record, idx.capacityByteSeconds))
	}
	row.RequestByteSeconds = parseIntOrByteSeconds(cell(record, idx.requestByteSeconds))
	row.UsageByteSeconds = parseIntOrByteSeconds(cell(record, idx.usageByteSeconds))
	return row, nil
}

func parseIntOrByteSeconds(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(f)
}
