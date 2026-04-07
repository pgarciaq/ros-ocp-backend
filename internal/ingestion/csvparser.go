package ingestion

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

const dateFormat = "2006-01-02 15:04:05 -0700 MST"

// CoreToMillicores converts a floating-point core count string (e.g., "0.250")
// to integer millicores (250). Returns an error for NaN, Inf, negative, or
// non-numeric inputs.
func CoreToMillicores(s string) (int64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("CoreToMillicores: %w", err)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("CoreToMillicores: invalid value %s", s)
	}
	if f < 0 {
		return 0, fmt.Errorf("CoreToMillicores: negative value %s", s)
	}
	return int64(math.Round(f * 1000)), nil
}

// BytesToKiB converts a floating-point byte count string (e.g., "1048576.0")
// to integer kibibytes (1024). Returns an error for NaN, Inf, negative, or
// non-numeric inputs.
func BytesToKiB(s string) (int64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("BytesToKiB: %w", err)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("BytesToKiB: invalid value %s", s)
	}
	if f < 0 {
		return 0, fmt.Errorf("BytesToKiB: negative value %s", s)
	}
	return int64(math.Round(f / 1024)), nil
}

// ValidateMetricRow checks that all numeric fields in a MetricRow are
// non-negative. Returns an error describing the first invalid field found.
func ValidateMetricRow(row MetricRow) error {
	checks := []struct {
		name string
		val  int64
	}{
		{"CPURequestMC", row.CPURequestMC},
		{"CPULimitMC", row.CPULimitMC},
		{"CPUUsageMC", row.CPUUsageMC},
		{"CPUThrottleMC", row.CPUThrottleMC},
		{"MemRequestKiB", row.MemRequestKiB},
		{"MemLimitKiB", row.MemLimitKiB},
		{"MemUsageKiB", row.MemUsageKiB},
		{"MemRSSKiB", row.MemRSSKiB},
		{"OOMCount", row.OOMCount},
	}
	for _, c := range checks {
		if c.val < 0 {
			return fmt.Errorf("ValidateMetricRow: %s is negative (%d)", c.name, c.val)
		}
	}
	return nil
}

// csvColumnIndex maps expected CSV header names to their column indices.
type csvColumnIndex struct {
	intervalStart int
	intervalEnd   int
	namespace     int
	workloadName  int
	workloadType  int
	containerName int
	cpuRequest    int
	cpuLimit      int
	cpuUsage      int
	cpuThrottle   int
	memRequest    int
	memLimit      int
	memUsage      int
	memRSS        int
	oomCount      int
}

func buildColumnIndex(header []string) (csvColumnIndex, error) {
	idx := csvColumnIndex{
		intervalStart: -1, intervalEnd: -1, namespace: -1, workloadName: -1,
		workloadType: -1, containerName: -1, cpuRequest: -1, cpuLimit: -1,
		cpuUsage: -1, cpuThrottle: -1, memRequest: -1, memLimit: -1,
		memUsage: -1, memRSS: -1, oomCount: -1,
	}
	for i, col := range header {
		switch col {
		case "interval_start":
			idx.intervalStart = i
		case "interval_end":
			idx.intervalEnd = i
		case "namespace":
			idx.namespace = i
		case "workload_name":
			idx.workloadName = i
		case "workload_type":
			idx.workloadType = i
		case "container_name":
			idx.containerName = i
		case "cpu_request":
			idx.cpuRequest = i
		case "cpu_limit":
			idx.cpuLimit = i
		case "cpu_usage":
			idx.cpuUsage = i
		case "cpu_throttle":
			idx.cpuThrottle = i
		case "mem_request":
			idx.memRequest = i
		case "mem_limit":
			idx.memLimit = i
		case "mem_usage":
			idx.memUsage = i
		case "mem_rss":
			idx.memRSS = i
		case "oom_count":
			idx.oomCount = i
		}
	}
	required := []struct {
		name string
		val  int
	}{
		{"interval_start", idx.intervalStart},
		{"interval_end", idx.intervalEnd},
		{"namespace", idx.namespace},
		{"workload_name", idx.workloadName},
		{"workload_type", idx.workloadType},
		{"container_name", idx.containerName},
		{"cpu_request", idx.cpuRequest},
		{"cpu_usage", idx.cpuUsage},
		{"mem_request", idx.memRequest},
		{"mem_usage", idx.memUsage},
	}
	for _, r := range required {
		if r.val < 0 {
			return idx, fmt.Errorf("ParseCSVRows: missing required column %q", r.name)
		}
	}
	return idx, nil
}

// ParseCSVRows reads an OCP metrics CSV (with header row) and converts all
// numeric columns to integer types. Rows with NaN, Inf, negative, or
// malformed values are skipped with a warning log. Returns the successfully
// parsed rows.
func ParseCSVRows(r io.Reader) ([]MetricRow, error) {
	reader := csv.NewReader(r)
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("ParseCSVRows: reading header: %w", err)
	}

	idx, err := buildColumnIndex(header)
	if err != nil {
		return nil, err
	}

	var rows []MetricRow
	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("ParseCSVRows: reading line %d: %w", lineNum+1, err)
		}
		lineNum++

		row, parseErr := parseRecord(record, idx)
		if parseErr != nil {
			log.Debugf("ParseCSVRows: skipping line %d: %v", lineNum, parseErr)
			continue
		}

		if valErr := ValidateMetricRow(row); valErr != nil {
			log.Debugf("ParseCSVRows: skipping line %d: %v", lineNum, valErr)
			continue
		}

		rows = append(rows, row)
	}

	return rows, nil
}

func parseRecord(record []string, idx csvColumnIndex) (MetricRow, error) {
	var row MetricRow
	var err error

	row.IntervalStart, err = time.Parse(dateFormat, record[idx.intervalStart])
	if err != nil {
		return row, fmt.Errorf("interval_start: %w", err)
	}
	row.IntervalEnd, err = time.Parse(dateFormat, record[idx.intervalEnd])
	if err != nil {
		return row, fmt.Errorf("interval_end: %w", err)
	}

	row.Namespace = record[idx.namespace]
	row.WorkloadName = record[idx.workloadName]
	row.WorkloadType = record[idx.workloadType]
	row.ContainerName = record[idx.containerName]

	row.CPURequestMC, err = CoreToMillicores(record[idx.cpuRequest])
	if err != nil {
		return row, err
	}
	row.CPUUsageMC, err = CoreToMillicores(record[idx.cpuUsage])
	if err != nil {
		return row, err
	}

	if idx.cpuLimit >= 0 && idx.cpuLimit < len(record) && record[idx.cpuLimit] != "" {
		row.CPULimitMC, err = CoreToMillicores(record[idx.cpuLimit])
		if err != nil {
			return row, err
		}
	}
	if idx.cpuThrottle >= 0 && idx.cpuThrottle < len(record) && record[idx.cpuThrottle] != "" {
		row.CPUThrottleMC, err = CoreToMillicores(record[idx.cpuThrottle])
		if err != nil {
			return row, err
		}
	}

	row.MemRequestKiB, err = BytesToKiB(record[idx.memRequest])
	if err != nil {
		return row, err
	}
	row.MemUsageKiB, err = BytesToKiB(record[idx.memUsage])
	if err != nil {
		return row, err
	}

	if idx.memLimit >= 0 && idx.memLimit < len(record) && record[idx.memLimit] != "" {
		row.MemLimitKiB, err = BytesToKiB(record[idx.memLimit])
		if err != nil {
			return row, err
		}
	}
	if idx.memRSS >= 0 && idx.memRSS < len(record) && record[idx.memRSS] != "" {
		row.MemRSSKiB, err = BytesToKiB(record[idx.memRSS])
		if err != nil {
			return row, err
		}
	}

	if idx.oomCount >= 0 && idx.oomCount < len(record) && record[idx.oomCount] != "" {
		v, err := strconv.ParseFloat(record[idx.oomCount], 64)
		if err != nil {
			return row, fmt.Errorf("oom_count: %w", err)
		}
		row.OOMCount = int64(math.Round(v))
	}

	return row, nil
}
