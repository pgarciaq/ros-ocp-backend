package csv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

var (
	errInvalidCoreValue  = errors.New("invalid CPU core value")
	errNegativeCoreValue = errors.New("negative CPU core value")
	errInvalidByteValue  = errors.New("invalid byte value")
	errNegativeByteValue = errors.New("negative byte value")
)

// MissingROSColumnsError lists required ROS headers that were absent.
type MissingROSColumnsError struct {
	Columns []string
}

func (e *MissingROSColumnsError) Error() string {
	return fmt.Sprintf("not a ROS container CSV (missing columns: %s)", strings.Join(e.Columns, ", "))
}

type columnIndex struct {
	intervalStart, intervalEnd                  int
	namespace, workloadName, workloadType       int
	containerName, pod, node                    int
	clusterID, instanceType, arch               int
	cpuRequest, cpuLimit, cpuUsage, cpuThrottle int
	memRequest, memLimit, memUsage, memRSS      int
	oomCount, workloadPodCount                  int
	desiredReplicas, availableReplicas          int
	acceleratorModelName                        int
}

func newColumnIndex() columnIndex {
	return columnIndex{
		intervalStart: -1, intervalEnd: -1, namespace: -1, workloadName: -1,
		workloadType: -1, containerName: -1, pod: -1, node: -1,
		clusterID: -1, instanceType: -1, arch: -1,
		cpuRequest: -1, cpuLimit: -1, cpuUsage: -1, cpuThrottle: -1,
		memRequest: -1, memLimit: -1, memUsage: -1, memRSS: -1,
		oomCount: -1, workloadPodCount: -1, desiredReplicas: -1, availableReplicas: -1,
		acceleratorModelName: -1,
	}
}

func buildColumnIndex(header []string) (columnIndex, error) {
	idx := newColumnIndex()
	for i, col := range header {
		switch strings.TrimSpace(col) {
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
		case "node":
			idx.node = i
		case "cluster_id", "cluster_uuid":
			idx.clusterID = i
		case "instance_type":
			idx.instanceType = i
		case "node_architecture", "architecture", "arch":
			idx.arch = i
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
		case "desired_replicas":
			idx.desiredReplicas = i
		case "available_replicas":
			idx.availableReplicas = i
		case "accelerator_model_name":
			idx.acceleratorModelName = i
		}
	}
	var missing []string
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
			missing = append(missing, r.name)
		}
	}
	if len(missing) > 0 {
		return idx, &MissingROSColumnsError{Columns: missing}
	}
	return idx, nil
}

// ParseRows reads a ROS container CSV. Bad numeric/timestamp rows are skipped
// (counted in skipped), not a parse error. Structural CSV errors still fail.
func ParseRows(r io.Reader) (rows []Row, skipped int, err error) {
	reader := csv.NewReader(r)
	reader.ReuseRecord = true
	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("reading header: %w", err)
	}
	idx, err := buildColumnIndex(header)
	if err != nil {
		return nil, 0, err
	}
	rows = make([]Row, 0, 256)
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
		row, parseErr := parseRecord(record, idx)
		if parseErr != nil {
			skipped++
			continue
		}
		rows = append(rows, row)
	}
	return rows, skipped, nil
}

func parseRecord(record []string, idx columnIndex) (Row, error) {
	var row Row
	var err error
	row.IntervalStart, err = parseFlexibleTimestamp(cell(record, idx.intervalStart))
	if err != nil {
		return row, err
	}
	row.IntervalEnd, err = parseFlexibleTimestamp(cell(record, idx.intervalEnd))
	if err != nil {
		return row, err
	}
	row.Namespace = cell(record, idx.namespace)
	row.WorkloadName = cell(record, idx.workloadName)
	row.WorkloadType = strings.ToLower(cell(record, idx.workloadType))
	row.ContainerName = cell(record, idx.containerName)
	row.Pod = cell(record, idx.pod)
	row.Node = cell(record, idx.node)
	row.ClusterID = cell(record, idx.clusterID)
	row.InstanceType = cell(record, idx.instanceType)
	row.Arch = cell(record, idx.arch)
	row.GPUModel = cell(record, idx.acceleratorModelName)

	row.CPURequestMC, err = coreToMillicores(cell(record, idx.cpuRequest))
	if err != nil {
		return row, err
	}
	row.CPUUsageMC, err = coreToMillicores(cell(record, idx.cpuUsage))
	if err != nil {
		return row, err
	}
	if s := cell(record, idx.cpuLimit); s != "" {
		row.CPULimitMC, err = coreToMillicores(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.cpuThrottle); s != "" {
		row.CPUThrottleMC, err = coreToMillicores(s)
		if err != nil {
			return row, err
		}
	}
	row.MemRequestKiB, err = bytesToKiB(cell(record, idx.memRequest))
	if err != nil {
		return row, err
	}
	row.MemUsageKiB, err = bytesToKiB(cell(record, idx.memUsage))
	if err != nil {
		return row, err
	}
	if s := cell(record, idx.memLimit); s != "" {
		row.MemLimitKiB, err = bytesToKiB(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.memRSS); s != "" {
		row.MemRSSKiB, err = bytesToKiB(s)
		if err != nil {
			return row, err
		}
	}
	row.OOMCount = optionalRoundedInt(record, idx.oomCount)
	row.WorkloadPodCount = optionalRoundedInt(record, idx.workloadPodCount)
	row.DesiredReplicas = optionalRoundedInt(record, idx.desiredReplicas)
	row.AvailableReplicas = optionalRoundedInt(record, idx.availableReplicas)
	return row, nil
}

func cell(record []string, i int) string {
	if i < 0 || i >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[i])
}

func optionalRoundedInt(record []string, i int) int64 {
	s := cell(record, i)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(math.Round(f))
}

func coreToMillicores(s string) (int64, error) {
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

func bytesToKiB(s string) (int64, error) {
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

func parseFlexibleTimestamp(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02 15:04:05 +0000 UTC",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05+00:00",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %q", s)
}
