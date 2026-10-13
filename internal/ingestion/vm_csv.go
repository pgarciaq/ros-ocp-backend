package ingestion

import (
	"context"
	"io"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// CanonicalVMUsageCSVHeader returns the comma-separated base column header for ros-openshift-vm-usage CSV.
// Optional columns (restart_count, GPU metrics, network metrics) may be appended by newer operators.
func CanonicalVMUsageCSVHeader() string {
	return libcsv.CanonicalVMUsageCSVHeader()
}

// forEachVMCSVRow parses VM usage CSV rows one at a time without retaining a full-slice copy.
func forEachVMCSVRow(ctx context.Context, r io.Reader, fn func(VMRow) error) (int, error) {
	count := 0
	skipped, err := libcsv.ForEachVM(ctx, r, func(row libcsv.VMRow) error {
		if err := fn(row); err != nil {
			return err
		}
		count++
		return nil
	})
	if skipped > 0 {
		metrics.IncCSVRowsSkipped("vm", skipped)
		logging.GetLogger().Warnf("ParseVMCSVRows: skipped %d malformed or invalid rows", skipped)
	}
	return count, err
}

// ParseVMCSVRows parses ros-openshift-vm-usage CSV content into VMRow values.
// Processor ingest uses forEachVMCSVRow; this collector is for tests and callers
// that still want a slice.
func ParseVMCSVRows(r io.Reader) ([]VMRow, error) {
	var rows []VMRow
	_, err := forEachVMCSVRow(context.Background(), r, func(row VMRow) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}





