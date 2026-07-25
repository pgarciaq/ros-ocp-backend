package ingestion

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

var vmPVCCSVExpectedColumns = []string{
	"interval_start",
	"vm_name",
	"namespace",
	"pvc_name",
	"disk_capacity_bytes",
	"volume_mode",
}

type vmPVCHeaderIdx struct {
	intervalStart     int
	vmName            int
	namespace         int
	nodeName          int
	pvcName           int
	diskCapacityBytes int
	volumeMode        int
}

// VMPVCRow is one 15-minute sample for a single PVC attached to a VM.
type VMPVCRow struct {
	IntervalStart     time.Time
	VMName            string
	Namespace         string
	NodeName          string
	PVCName           string
	DiskCapacityBytes int64
	VolumeMode        string
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
		return idx, fmt.Errorf("VM PVC CSV missing required columns: %s", strings.Join(missing, ", "))
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

// ParseVMPVCCSVRows parses ros-openshift-vm-pvc CSV content.
func ParseVMPVCCSVRows(ctx context.Context, r io.Reader) ([]VMPVCRow, error) {
	reader := csv.NewReader(r)
	reader.ReuseRecord = true
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading VM PVC CSV header: %w", err)
	}
	idx, err := buildVMPVCColumnIndex(header)
	if err != nil {
		return nil, err
	}

	log := logging.GetLogger()
	var rows []VMPVCRow
	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading VM PVC CSV row: %w", err)
		}
		lineNum++
		row, parseErr := parseVMPVCRecord(record, idx)
		if parseErr != nil {
			log.Warnf("ParseVMPVCCSVRows: skipping line %d: %v", lineNum, parseErr)
			continue
		}
		if row.VMName == "" || row.Namespace == "" || row.PVCName == "" {
			log.Warnf("ParseVMPVCCSVRows: skipping line %d: empty vm_name, namespace, or pvc_name", lineNum)
			continue
		}
		rows = append(rows, row)
		if len(rows)%10000 == 0 {
			if err := ctx.Err(); err != nil {
				return rows, err
			}
		}
	}
	return rows, nil
}

func parseVMPVCRecord(record []string, idx vmPVCHeaderIdx) (VMPVCRow, error) {
	var row VMPVCRow
	var err error

	row.IntervalStart, err = parseFlexibleTimestamp(fieldAt(record, idx.intervalStart))
	if err != nil {
		return row, fmt.Errorf("parse interval_start: %w", err)
	}
	row.VMName = strings.TrimSpace(fieldAt(record, idx.vmName))
	row.Namespace = strings.TrimSpace(fieldAt(record, idx.namespace))
	if idx.nodeName >= 0 {
		row.NodeName = strings.TrimSpace(fieldAt(record, idx.nodeName))
	}
	row.PVCName = strings.TrimSpace(fieldAt(record, idx.pvcName))

	if v := strings.TrimSpace(fieldAt(record, idx.diskCapacityBytes)); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return row, fmt.Errorf("parse disk_capacity_bytes: %w", err)
		}
		row.DiskCapacityBytes = int64(f)
	}

	row.VolumeMode = strings.TrimSpace(fieldAt(record, idx.volumeMode))
	if row.VolumeMode == "" {
		row.VolumeMode = "Filesystem"
	}
	return row, nil
}

// MergeVMPVCRowsIntoDigests aggregates PVC CSV rows by (VM, namespace, bucket_date, pvc_name),
// keeping the max disk_capacity_bytes seen across samples.
func MergeVMPVCRowsIntoDigests(rows []VMPVCRow) map[VMDigestKey][]IngestPVCDigest {
	type pvcKey struct {
		digest  VMDigestKey
		pvcName string
	}
	acc := make(map[pvcKey]*IngestPVCDigest)

	for _, r := range rows {
		bucket := vmBucketDate(r.IntervalStart)
		dk := VMDigestKey{VMName: r.VMName, Namespace: r.Namespace, BucketDate: bucket}
		key := pvcKey{digest: dk, pvcName: r.PVCName}
		pvc, ok := acc[key]
		if !ok {
			pvc = &IngestPVCDigest{
				PVCName:           r.PVCName,
				DiskCapacityBytes: r.DiskCapacityBytes,
				VolumeMode:        r.VolumeMode,
			}
			acc[key] = pvc
		}
		if r.DiskCapacityBytes > pvc.DiskCapacityBytes {
			pvc.DiskCapacityBytes = r.DiskCapacityBytes
		}
		if r.VolumeMode != "" {
			pvc.VolumeMode = r.VolumeMode
		}
	}

	result := make(map[VMDigestKey][]IngestPVCDigest)
	for key, pvc := range acc {
		result[key.digest] = append(result[key.digest], *pvc)
	}
	return result
}

// IngestPVCDigest holds aggregated PVC data ready for upsert.
type IngestPVCDigest struct {
	PVCName           string
	DiskCapacityBytes int64
	VolumeMode        string
}
