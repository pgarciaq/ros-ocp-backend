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

// VMPVCRow is one interval from a ROS VM PVC companion CSV.
type VMPVCRow struct {
	IntervalStart     time.Time
	VMName            string
	Namespace         string
	NodeName          string
	PVCName           string
	DiskCapacityBytes int64
	VolumeMode        string
}

// MissingVMPVCColumnsError lists required VM PVC companion headers that were absent.
type MissingVMPVCColumnsError struct {
	Columns []string
}

func (e *MissingVMPVCColumnsError) Error() string {
	return fmt.Sprintf("not a VM PVC CSV (missing columns: %s)", strings.Join(e.Columns, ", "))
}

// CanonicalVMPVCCSVHeader is the comma-separated required column header for
// ros-openshift-vm-pvc companion CSVs. node_name is optional.
func CanonicalVMPVCCSVHeader() string {
	return strings.Join(vmPVCCSVExpectedColumns, ",")
}

var vmPVCCSVExpectedColumns = []string{
	"interval_start",
	"vm_name",
	"namespace",
	"pvc_name",
	"disk_capacity_bytes",
	"volume_mode",
}

type vmPVCHeaderIdx struct {
	intervalStart, vmName, namespace int
	nodeName, pvcName                int
	diskCapacityBytes, volumeMode    int
}

func buildVMPVCColumnIndex(header []string) (vmPVCHeaderIdx, error) {
	idx := vmPVCHeaderIdx{
		intervalStart: -1, vmName: -1, namespace: -1, nodeName: -1,
		pvcName: -1, diskCapacityBytes: -1, volumeMode: -1,
	}
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case "interval_start":
			idx.intervalStart = i
		case "vm_name":
			idx.vmName = i
		case "namespace":
			idx.namespace = i
		case "node_name":
			idx.nodeName = i
		case "pvc_name":
			idx.pvcName = i
		case "disk_capacity_bytes":
			idx.diskCapacityBytes = i
		case "volume_mode":
			idx.volumeMode = i
		}
	}
	var missing []string
	for _, col := range vmPVCCSVExpectedColumns {
		if !vmPVCColumnPresent(idx, col) {
			missing = append(missing, col)
		}
	}
	if len(missing) > 0 {
		return idx, &MissingVMPVCColumnsError{Columns: missing}
	}
	return idx, nil
}

func vmPVCColumnPresent(idx vmPVCHeaderIdx, col string) bool {
	switch col {
	case "interval_start":
		return idx.intervalStart >= 0
	case "vm_name":
		return idx.vmName >= 0
	case "namespace":
		return idx.namespace >= 0
	case "pvc_name":
		return idx.pvcName >= 0
	case "disk_capacity_bytes":
		return idx.diskCapacityBytes >= 0
	case "volume_mode":
		return idx.volumeMode >= 0
	default:
		return false
	}
}

// ForEachVMPVC parses a ROS VM PVC companion CSV one record at a time without
// retaining the full file. Empty vm_name, namespace, or pvc_name rows and bad
// timestamps/numbers are skipped (counted in skipped). Structural CSV errors
// still fail. ctx is checked every 10_000 successfully parsed rows (same
// cadence as ForEachRow). A nil ctx is treated as Background.
//
// The operator must not import this package (ADR-0305).
func ForEachVMPVC(ctx context.Context, r io.Reader, fn func(VMPVCRow) error) (skipped int, err error) {
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
	idx, err := buildVMPVCColumnIndex(header)
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
		row, parseErr := parseVMPVCRecord(record, idx)
		if parseErr != nil {
			skipped++
			continue
		}
		if row.VMName == "" || row.Namespace == "" || row.PVCName == "" {
			skipped++
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

// ParseVMPVCRows reads a ROS VM PVC companion CSV. Empty identity rows and bad
// timestamps are skipped (counted in skipped). Structural CSV errors still fail.
// CLI batch load uses this; processor ingest uses ForEachVMPVC.
func ParseVMPVCRows(r io.Reader) (rows []VMPVCRow, skipped int, err error) {
	rows = make([]VMPVCRow, 0, 64)
	skipped, err = ForEachVMPVC(context.Background(), r, func(row VMPVCRow) error {
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

func parseVMPVCRecord(record []string, idx vmPVCHeaderIdx) (VMPVCRow, error) {
	var row VMPVCRow
	var err error
	row.IntervalStart, err = parseFlexibleTimestamp(cell(record, idx.intervalStart))
	if err != nil {
		return row, err
	}
	row.VMName = cell(record, idx.vmName)
	row.Namespace = cell(record, idx.namespace)
	row.NodeName = cell(record, idx.nodeName)
	row.PVCName = cell(record, idx.pvcName)
	if v := cell(record, idx.diskCapacityBytes); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return row, err
		}
		row.DiskCapacityBytes = int64(f)
	}
	row.VolumeMode = cell(record, idx.volumeMode)
	if row.VolumeMode == "" {
		row.VolumeMode = "Filesystem"
	}
	return row, nil
}
