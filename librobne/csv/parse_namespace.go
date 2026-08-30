package csv

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

type nsColumnIndex struct {
	intervalStart, intervalEnd       int
	namespace, clusterID             int
	cpuRequest, cpuLimit             int
	cpuUsage, cpuUsageMax            int
	cpuUsageMin                      int
	cpuThrottleAvg, cpuThrottleMax   int
	memRequest, memLimit             int
	memUsage, memUsageMax            int
	memUsageMin                      int
	memRSS, memRSSMax                int
	quotaName                        int
	cpuRequestUsed, cpuLimitUsed     int
	memRequestUsed, memLimitUsed     int
	storageHard, storageUsed         int
	podsHard, podsUsed               int
	objectCountHard, objectCountUsed int
}

func newNSColumnIndex() nsColumnIndex {
	return nsColumnIndex{
		intervalStart: -1, intervalEnd: -1, namespace: -1, clusterID: -1,
		cpuRequest: -1, cpuLimit: -1, cpuUsage: -1, cpuUsageMax: -1,
		cpuUsageMin: -1, cpuThrottleAvg: -1, cpuThrottleMax: -1,
		memRequest: -1, memLimit: -1, memUsage: -1, memUsageMax: -1,
		memUsageMin: -1, memRSS: -1, memRSSMax: -1, quotaName: -1,
		cpuRequestUsed: -1, cpuLimitUsed: -1,
		memRequestUsed: -1, memLimitUsed: -1,
		storageHard: -1, storageUsed: -1,
		podsHard: -1, podsUsed: -1,
		objectCountHard: -1, objectCountUsed: -1,
	}
}

func buildNSColumnIndex(header []string) (nsColumnIndex, error) {
	idx := newNSColumnIndex()
	for i, col := range header {
		switch strings.TrimSpace(col) {
		case "interval_start":
			idx.intervalStart = i
		case "interval_end":
			idx.intervalEnd = i
		case "namespace":
			idx.namespace = i
		case "cluster_id", "cluster_uuid":
			idx.clusterID = i
		case "cpu_request_namespace_sum":
			idx.cpuRequest = i
		case "cpu_limit_namespace_sum":
			idx.cpuLimit = i
		case "cpu_usage_namespace_avg":
			idx.cpuUsage = i
		case "cpu_usage_namespace_max":
			idx.cpuUsageMax = i
		case "cpu_usage_namespace_min":
			idx.cpuUsageMin = i
		case "cpu_throttle_namespace_avg":
			idx.cpuThrottleAvg = i
		case "cpu_throttle_namespace_max":
			idx.cpuThrottleMax = i
		case "memory_request_namespace_sum":
			idx.memRequest = i
		case "memory_limit_namespace_sum":
			idx.memLimit = i
		case "memory_usage_namespace_avg":
			idx.memUsage = i
		case "memory_usage_namespace_max":
			idx.memUsageMax = i
		case "memory_usage_namespace_min":
			idx.memUsageMin = i
		case "memory_rss_usage_namespace_avg":
			idx.memRSS = i
		case "memory_rss_usage_namespace_max":
			idx.memRSSMax = i
		case "quota_name", "resource_quota_name":
			idx.quotaName = i
		case "cpu_request_namespace_used":
			idx.cpuRequestUsed = i
		case "cpu_limit_namespace_used":
			idx.cpuLimitUsed = i
		case "memory_request_namespace_used":
			idx.memRequestUsed = i
		case "memory_limit_namespace_used":
			idx.memLimitUsed = i
		case "storage_request_namespace_hard":
			idx.storageHard = i
		case "storage_request_namespace_used":
			idx.storageUsed = i
		case "pods_namespace_hard":
			idx.podsHard = i
		case "pods_namespace_used":
			idx.podsUsed = i
		case "object_count_namespace_hard":
			idx.objectCountHard = i
		case "object_count_namespace_used":
			idx.objectCountUsed = i
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
		{"cpu_request_namespace_sum", idx.cpuRequest},
		{"cpu_usage_namespace_avg", idx.cpuUsage},
		{"memory_request_namespace_sum", idx.memRequest},
		{"memory_usage_namespace_avg", idx.memUsage},
	}
	for _, r := range required {
		if r.val < 0 {
			missing = append(missing, r.name)
		}
	}
	if len(missing) > 0 {
		return idx, &MissingNamespaceColumnsError{Columns: missing}
	}
	return idx, nil
}

// ForEachNamespace parses a ROS namespace CSV one record at a time without
// retaining the full file. The callback receives each successfully parsed row.
// Bad numeric/timestamp rows are skipped (counted in skipped). Structural CSV
// errors still fail. ctx is checked every 10_000 successfully parsed rows
// (same cadence as ForEachRow). A nil ctx is treated as Background.
//
// The operator must not import this package (ADR-0305).
func ForEachNamespace(ctx context.Context, r io.Reader, fn func(NamespaceRow) error) (skipped int, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reader := csv.NewReader(r)
	reader.ReuseRecord = true
	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading header: %w", err)
	}
	idx, err := buildNSColumnIndex(header)
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
		row, parseErr := parseNSRecord(record, idx)
		if parseErr != nil {
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

// ParseNamespaceRows reads a ROS namespace CSV. Bad numeric/timestamp rows are
// skipped (counted in skipped), not a parse error. Structural CSV errors still fail.
// CLI batch load uses this; processor ingest uses ForEachNamespace.
func ParseNamespaceRows(r io.Reader) (rows []NamespaceRow, skipped int, err error) {
	rows = make([]NamespaceRow, 0, 256)
	skipped, err = ForEachNamespace(context.Background(), r, func(row NamespaceRow) error {
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

func parseNSRecord(record []string, idx nsColumnIndex) (NamespaceRow, error) {
	var row NamespaceRow
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
	row.ClusterID = cell(record, idx.clusterID)

	row.CPURequestMC, err = coreToMillicores(cell(record, idx.cpuRequest))
	if err != nil {
		return row, err
	}
	row.CPURequestHardMC = row.CPURequestMC
	row.CPUUsageMC, err = coreToMillicores(cell(record, idx.cpuUsage))
	if err != nil {
		return row, err
	}
	if s := cell(record, idx.cpuLimit); s != "" {
		row.CPULimitMC, err = coreToMillicores(s)
		if err != nil {
			return row, err
		}
		row.CPULimitHardMC = row.CPULimitMC
	}
	if s := cell(record, idx.cpuUsageMax); s != "" {
		row.CPUUsageMaxMC, err = coreToMillicores(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.cpuUsageMin); s != "" {
		row.CPUUsageMinMC, err = coreToMillicores(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.cpuThrottleAvg); s != "" {
		row.CPUThrottleAvgMC, err = coreToMillicores(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.cpuThrottleMax); s != "" {
		row.CPUThrottleMaxMC, err = coreToMillicores(s)
		if err != nil {
			return row, err
		}
	}
	row.MemRequestKiB, err = bytesToKiB(cell(record, idx.memRequest))
	if err != nil {
		return row, err
	}
	row.MemoryRequestHardBytes = row.MemRequestKiB * 1024
	row.MemUsageKiB, err = bytesToKiB(cell(record, idx.memUsage))
	if err != nil {
		return row, err
	}
	if s := cell(record, idx.memLimit); s != "" {
		row.MemLimitKiB, err = bytesToKiB(s)
		if err != nil {
			return row, err
		}
		row.MemoryLimitHardBytes = row.MemLimitKiB * 1024
	}
	if s := cell(record, idx.memUsageMax); s != "" {
		row.MemUsageMaxKiB, err = bytesToKiB(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.memUsageMin); s != "" {
		row.MemUsageMinKiB, err = bytesToKiB(s)
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
	if s := cell(record, idx.memRSSMax); s != "" {
		row.MemRSSMaxKiB, err = bytesToKiB(s)
		if err != nil {
			return row, err
		}
	}
	row.QuotaName = cell(record, idx.quotaName)
	if s := cell(record, idx.cpuRequestUsed); s != "" {
		row.CPURequestUsedMC, err = coreToMillicores(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.cpuLimitUsed); s != "" {
		row.CPULimitUsedMC, err = coreToMillicores(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.memRequestUsed); s != "" {
		usedKiB, err := bytesToKiB(s)
		if err != nil {
			return row, err
		}
		row.MemoryRequestUsedBytes = usedKiB * 1024
	}
	if s := cell(record, idx.memLimitUsed); s != "" {
		usedKiB, err := bytesToKiB(s)
		if err != nil {
			return row, err
		}
		row.MemoryLimitUsedBytes = usedKiB * 1024
	}
	if s := cell(record, idx.storageHard); s != "" {
		row.StorageRequestHardBytes, err = parseInt64Field(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.storageUsed); s != "" {
		row.StorageRequestUsedBytes, err = parseInt64Field(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.podsHard); s != "" {
		row.PodsHard, err = parseInt64Field(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.podsUsed); s != "" {
		row.PodsUsed, err = parseInt64Field(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.objectCountHard); s != "" {
		row.ObjectCountHard, err = parseInt64Field(s)
		if err != nil {
			return row, err
		}
	}
	if s := cell(record, idx.objectCountUsed); s != "" {
		row.ObjectCountUsed, err = parseInt64Field(s)
		if err != nil {
			return row, err
		}
	}
	return row, nil
}
