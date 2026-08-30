package ingestion

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

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

func parseOptionalInt32(s string) (*int32, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return nil, err
	}
	if v < 0 {
		return nil, fmt.Errorf("negative value %d", v)
	}
	out := int32(v)
	return &out, nil
}

func fieldAt(record []string, col int) string {
	if col < 0 || col >= len(record) {
		return ""
	}
	return record[col]
}

func parseRequiredFloat(s, field string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("%s is empty", field)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", field, err)
	}
	if f < 0 {
		return 0, fmt.Errorf("%s is negative", field)
	}
	return f, nil
}

func optionalFloatValue(s string) (float64, error) {
	v, err := parseOptionalFloat(s)
	if err != nil {
		return 0, err
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

func parseOptionalFloat(s string) (*float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, err
	}
	if f < 0 {
		return nil, fmt.Errorf("negative value %v", f)
	}
	return &f, nil
}
