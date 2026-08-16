package csv

import (
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

// ParseVMPVCRows reads a ROS VM PVC companion CSV. Empty identity rows and bad
// timestamps are skipped. Structural CSV errors still fail.
func ParseVMPVCRows(r io.Reader) (rows []VMPVCRow, skipped int, err error) {
	reader := csv.NewReader(r)
	reader.ReuseRecord = true
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("reading header: %w", err)
	}
	idx, err := buildVMPVCColumnIndex(header)
	if err != nil {
		return nil, 0, err
	}
	rows = make([]VMPVCRow, 0, 64)
	lineNum := 1
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("reading line %d: %w", lineNum+1, err)
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
		rows = append(rows, row)
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
