package ingestion

import (
	"context"
	"io"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// VMPVCRow is a parsed ROS VM PVC companion CSV row (librobne/csv.VMPVCRow).
type VMPVCRow = libcsv.VMPVCRow

// CanonicalVMPVCCSVHeader returns the comma-separated required column header
// for ros-openshift-vm-pvc CSVs. node_name is optional.
func CanonicalVMPVCCSVHeader() string {
	return libcsv.CanonicalVMPVCCSVHeader()
}

// forEachVMPVCCSVRow parses VM PVC companion CSV rows one at a time without
// retaining a full-slice copy.
func forEachVMPVCCSVRow(ctx context.Context, r io.Reader, fn func(VMPVCRow) error) (int, error) {
	count := 0
	skipped, err := libcsv.ForEachVMPVC(ctx, r, func(row libcsv.VMPVCRow) error {
		if err := fn(row); err != nil {
			return err
		}
		count++
		return nil
	})
	if skipped > 0 {
		metrics.IncCSVRowsSkipped("vm-pvc", skipped)
		logging.GetLogger().Warnf("ParseVMPVCCSVRows: skipped %d malformed or invalid rows", skipped)
	}
	return count, err
}

// ParseVMPVCCSVRows parses ros-openshift-vm-pvc CSV content into VMPVCRow values.
// Processor ingest uses forEachVMPVCCSVRow; this collector is for tests and
// callers that still want a slice.
func ParseVMPVCCSVRows(ctx context.Context, r io.Reader) ([]VMPVCRow, error) {
	var rows []VMPVCRow
	_, err := forEachVMPVCCSVRow(ctx, r, func(row VMPVCRow) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

type vmPVCAccKey struct {
	digest  VMDigestKey
	pvcName string
}

func addVMPVCRowToGroups(acc map[vmPVCAccKey]*IngestPVCDigest, r VMPVCRow) {
	bucket := vmBucketDate(r.IntervalStart)
	dk := VMDigestKey{VMName: r.VMName, Namespace: r.Namespace, BucketDate: bucket}
	key := vmPVCAccKey{digest: dk, pvcName: r.PVCName}
	pvc, ok := acc[key]
	if !ok {
		pvc = &IngestPVCDigest{
			PVCName:           r.PVCName,
			DiskCapacityBytes: r.DiskCapacityBytes,
			VolumeMode:        r.VolumeMode,
		}
		acc[key] = pvc
		return
	}
	if r.DiskCapacityBytes > pvc.DiskCapacityBytes {
		pvc.DiskCapacityBytes = r.DiskCapacityBytes
	}
	if r.VolumeMode != "" {
		pvc.VolumeMode = r.VolumeMode
	}
}

func finalizeVMPVCGroups(acc map[vmPVCAccKey]*IngestPVCDigest) map[VMDigestKey][]IngestPVCDigest {
	result := make(map[VMDigestKey][]IngestPVCDigest)
	for key, pvc := range acc {
		result[key.digest] = append(result[key.digest], *pvc)
	}
	return result
}

// MergeVMPVCRowsIntoDigests aggregates PVC CSV rows by (VM, namespace, bucket_date, pvc_name),
// keeping the max disk_capacity_bytes seen across samples.
func MergeVMPVCRowsIntoDigests(rows []VMPVCRow) map[VMDigestKey][]IngestPVCDigest {
	acc := make(map[vmPVCAccKey]*IngestPVCDigest)
	for _, r := range rows {
		addVMPVCRowToGroups(acc, r)
	}
	return finalizeVMPVCGroups(acc)
}

// IngestPVCDigest holds aggregated PVC data ready for upsert.
type IngestPVCDigest struct {
	PVCName           string
	DiskCapacityBytes int64
	VolumeMode        string
}
