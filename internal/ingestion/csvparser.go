package ingestion

import (
	"context"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// CoreToMillicores converts a floating-point core count string (e.g., "0.250")
// to integer millicores (250). Returns an error for NaN, Inf, negative, or
// non-numeric inputs. Kept for tests. Entity CSV parse lives in librobne/csv.
func CoreToMillicores(s string) (int64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errInvalidCoreValue
	}
	if f < 0 {
		return 0, errNegativeCoreValue
	}
	return int64(math.Round(f * 1000)), nil
}

// BytesToKiB converts a floating-point byte count string (e.g., "1048576.0")
// to integer kibibytes (1024). Returns an error for NaN, Inf, negative, or
// non-numeric inputs. Kept for tests. Entity CSV parse lives in librobne/csv.
func BytesToKiB(s string) (int64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errInvalidByteValue
	}
	if f < 0 {
		return 0, errNegativeByteValue
	}
	return int64(math.Round(f / 1024)), nil
}

// ValidateMetricRow checks that all numeric fields in a MetricRow are
// non-negative. Returns an error describing the first invalid field found.
// Container parse already rejects negatives; this remains belt-and-suspenders
// for tests and ingest wrappers.
func ValidateMetricRow(row MetricRow) error {
	if row.CPURequestMC < 0 {
		return fmt.Errorf("ValidateMetricRow: CPURequestMC is negative (%d)", row.CPURequestMC)
	}
	if row.CPULimitMC < 0 {
		return fmt.Errorf("ValidateMetricRow: CPULimitMC is negative (%d)", row.CPULimitMC)
	}
	if row.CPUUsageMC < 0 {
		return fmt.Errorf("ValidateMetricRow: CPUUsageMC is negative (%d)", row.CPUUsageMC)
	}
	if row.CPUThrottleMC < 0 {
		return fmt.Errorf("ValidateMetricRow: CPUThrottleMC is negative (%d)", row.CPUThrottleMC)
	}
	if row.MemRequestKiB < 0 {
		return fmt.Errorf("ValidateMetricRow: MemRequestKiB is negative (%d)", row.MemRequestKiB)
	}
	if row.MemLimitKiB < 0 {
		return fmt.Errorf("ValidateMetricRow: MemLimitKiB is negative (%d)", row.MemLimitKiB)
	}
	if row.MemUsageKiB < 0 {
		return fmt.Errorf("ValidateMetricRow: MemUsageKiB is negative (%d)", row.MemUsageKiB)
	}
	if row.MemRSSKiB < 0 {
		return fmt.Errorf("ValidateMetricRow: MemRSSKiB is negative (%d)", row.MemRSSKiB)
	}
	if row.OOMCount < 0 {
		return fmt.Errorf("ValidateMetricRow: OOMCount is negative (%d)", row.OOMCount)
	}
	return nil
}

// ParseCSVRows reads an OCP metrics CSV (with header row) and converts all
// numeric columns to integer types. Rows with NaN, Inf, negative, or
// malformed values are skipped. Returns the successfully parsed rows.
// Processor ingest uses forEachCSVRow (streaming); this batch API is for
// tests and plugins.
func ParseCSVRows(r io.Reader) ([]MetricRow, error) {
	var rows []MetricRow
	_, err := forEachCSVRow(context.Background(), r, func(row MetricRow) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ParseCSVRows: %w", err)
	}
	return rows, nil
}

// forEachCSVRow parses CSV rows one at a time without retaining a full-slice copy.
func forEachCSVRow(ctx context.Context, r io.Reader, fn func(MetricRow) error) (int, error) {
	count := 0
	validatorSkipped := 0
	skipped, err := libcsv.ForEachRow(ctx, r, func(row libcsv.Row) error {
		if valErr := ValidateMetricRow(row); valErr != nil {
			logging.GetLogger().Debugf("ParseCSVRows: skipping row: %v", valErr)
			validatorSkipped++
			return nil
		}
		if err := fn(row); err != nil {
			return err
		}
		count++
		return nil
	})
	totalSkipped := skipped + validatorSkipped
	if totalSkipped > 0 {
		metrics.IncCSVRowsSkipped("container", totalSkipped)
		logging.GetLogger().Warnf("ParseCSVRows: skipped %d malformed or invalid rows", totalSkipped)
	}
	return count, err
}
