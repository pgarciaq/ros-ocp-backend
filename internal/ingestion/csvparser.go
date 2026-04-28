package ingestion

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
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
	intervalStart    int
	intervalEnd      int
	namespace        int
	workloadName     int
	workloadType     int
	containerName    int
	pod              int
	cpuRequest       int
	cpuLimit         int
	cpuUsage         int
	cpuThrottle      int
	memRequest       int
	memLimit         int
	memUsage         int
	memRSS           int
	oomCount         int
	workloadPodCount int
	// GPU columns (optional; -1 when header absent).
	acceleratorModelName           int
	acceleratorProfileName         int
	acceleratorFrameBufferUsageMin int
	acceleratorFrameBufferUsageMax int
	acceleratorFrameBufferUsageAvg int
	tensorPipeActiveMin            int
	tensorPipeActiveMax            int
	tensorPipeActiveAvg            int
	dramActiveMin                  int
	dramActiveMax                  int
	dramActiveAvg                  int
	smActiveMin                    int
	smActiveMax                    int
	smActiveAvg                    int
}

func buildColumnIndex(header []string) (csvColumnIndex, error) {
	idx := csvColumnIndex{
		intervalStart: -1, intervalEnd: -1, namespace: -1, workloadName: -1,
		workloadType: -1, containerName: -1, pod: -1, cpuRequest: -1, cpuLimit: -1,
		cpuUsage: -1, cpuThrottle: -1, memRequest: -1, memLimit: -1,
		memUsage: -1, memRSS: -1, oomCount: -1, workloadPodCount: -1,
		acceleratorModelName:           -1,
		acceleratorProfileName:         -1,
		acceleratorFrameBufferUsageMin: -1,
		acceleratorFrameBufferUsageMax: -1,
		acceleratorFrameBufferUsageAvg: -1,
		tensorPipeActiveMin:            -1,
		tensorPipeActiveMax:            -1,
		tensorPipeActiveAvg:            -1,
		dramActiveMin:                  -1,
		dramActiveMax:                  -1,
		dramActiveAvg:                  -1,
		smActiveMin:                    -1,
		smActiveMax:                    -1,
		smActiveAvg:                    -1,
	}
	for i, col := range header {
		switch col {
		case "interval_start":
			idx.intervalStart = i
		case "interval_end":
			idx.intervalEnd = i
		case "namespace":
			idx.namespace = i
		case "workload":
			idx.workloadName = i
		case "workload_type":
			idx.workloadType = i
		case "container_name":
			idx.containerName = i
		case "pod":
			idx.pod = i
		case "cpu_request_container_avg":
			idx.cpuRequest = i
		case "cpu_limit_container_avg":
			idx.cpuLimit = i
		case "cpu_usage_container_avg":
			idx.cpuUsage = i
		case "cpu_throttle_container_avg":
			idx.cpuThrottle = i
		case "memory_request_container_avg":
			idx.memRequest = i
		case "memory_limit_container_avg":
			idx.memLimit = i
		case "memory_usage_container_avg":
			idx.memUsage = i
		case "memory_rss_usage_container_avg":
			idx.memRSS = i
		case "oom_count":
			idx.oomCount = i
		case "workload_pod_count":
			idx.workloadPodCount = i
		case "accelerator_model_name":
			idx.acceleratorModelName = i
		case "accelerator_profile_name":
			idx.acceleratorProfileName = i
		case "accelerator_frame_buffer_usage_min":
			idx.acceleratorFrameBufferUsageMin = i
		case "accelerator_frame_buffer_usage_max":
			idx.acceleratorFrameBufferUsageMax = i
		case "accelerator_frame_buffer_usage_avg":
			idx.acceleratorFrameBufferUsageAvg = i
		case "tensor_pipe_active_min":
			idx.tensorPipeActiveMin = i
		case "tensor_pipe_active_max":
			idx.tensorPipeActiveMax = i
		case "tensor_pipe_active_avg":
			idx.tensorPipeActiveAvg = i
		case "dram_active_min":
			idx.dramActiveMin = i
		case "dram_active_max":
			idx.dramActiveMax = i
		case "dram_active_avg":
			idx.dramActiveAvg = i
		case "sm_active_min":
			idx.smActiveMin = i
		case "sm_active_max":
			idx.smActiveMax = i
		case "sm_active_avg":
			idx.smActiveAvg = i
		}
	}
	required := []struct {
		name string
		val  int
	}{
		{"interval_start", idx.intervalStart},
		{"interval_end", idx.intervalEnd},
		{"namespace", idx.namespace},
		{"workload", idx.workloadName},
		{"workload_type", idx.workloadType},
		{"container_name", idx.containerName},
		{"pod", idx.pod},
		{"cpu_request_container_avg", idx.cpuRequest},
		{"cpu_usage_container_avg", idx.cpuUsage},
		{"memory_request_container_avg", idx.memRequest},
		{"memory_usage_container_avg", idx.memUsage},
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

// optionalParseFloat parses an optional GPU metric cell. Missing column index,
// short records, or empty/whitespace cells yield 0 with no error.
func optionalParseFloat(record []string, col int, field string) (float64, error) {
	if col < 0 || col >= len(record) {
		return 0, nil
	}
	s := strings.TrimSpace(record[col])
	if s == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", field, err)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%s: invalid value %s", field, s)
	}
	return f, nil
}

func optionalStringField(record []string, col int) string {
	if col < 0 || col >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[col])
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
	row.Pod = record[idx.pod]

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

	if idx.workloadPodCount >= 0 && idx.workloadPodCount < len(record) && record[idx.workloadPodCount] != "" {
		v, err := strconv.ParseFloat(record[idx.workloadPodCount], 64)
		if err != nil {
			return row, fmt.Errorf("workload_pod_count: %w", err)
		}
		row.WorkloadPodCount = int64(math.Round(v))
	}

	row.AcceleratorModelName = optionalStringField(record, idx.acceleratorModelName)
	row.AcceleratorProfileName = optionalStringField(record, idx.acceleratorProfileName)

	if row.AcceleratorFBUsageMin, err = optionalParseFloat(record, idx.acceleratorFrameBufferUsageMin, "accelerator_frame_buffer_usage_min"); err != nil {
		return row, err
	}
	if row.AcceleratorFBUsageMax, err = optionalParseFloat(record, idx.acceleratorFrameBufferUsageMax, "accelerator_frame_buffer_usage_max"); err != nil {
		return row, err
	}
	if row.AcceleratorFBUsageAvg, err = optionalParseFloat(record, idx.acceleratorFrameBufferUsageAvg, "accelerator_frame_buffer_usage_avg"); err != nil {
		return row, err
	}
	if row.TensorPipeActiveMin, err = optionalParseFloat(record, idx.tensorPipeActiveMin, "tensor_pipe_active_min"); err != nil {
		return row, err
	}
	if row.TensorPipeActiveMax, err = optionalParseFloat(record, idx.tensorPipeActiveMax, "tensor_pipe_active_max"); err != nil {
		return row, err
	}
	if row.TensorPipeActiveAvg, err = optionalParseFloat(record, idx.tensorPipeActiveAvg, "tensor_pipe_active_avg"); err != nil {
		return row, err
	}
	if row.DRAMActiveMin, err = optionalParseFloat(record, idx.dramActiveMin, "dram_active_min"); err != nil {
		return row, err
	}
	if row.DRAMActiveMax, err = optionalParseFloat(record, idx.dramActiveMax, "dram_active_max"); err != nil {
		return row, err
	}
	if row.DRAMActiveAvg, err = optionalParseFloat(record, idx.dramActiveAvg, "dram_active_avg"); err != nil {
		return row, err
	}
	if row.SMActiveMin, err = optionalParseFloat(record, idx.smActiveMin, "sm_active_min"); err != nil {
		return row, err
	}
	if row.SMActiveMax, err = optionalParseFloat(record, idx.smActiveMax, "sm_active_max"); err != nil {
		return row, err
	}
	if row.SMActiveAvg, err = optionalParseFloat(record, idx.smActiveAvg, "sm_active_avg"); err != nil {
		return row, err
	}

	return row, nil
}
