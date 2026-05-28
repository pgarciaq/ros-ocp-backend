package ingestion

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// NamespaceMetricRow represents a single parsed row from a namespace CSV file.
// Namespace CSVs aggregate at the namespace level: requests/limits use SUM
// (additive across pods), usage metrics use AVG/MAX/MIN per interval.
type NamespaceMetricRow struct {
	IntervalStart time.Time
	IntervalEnd   time.Time
	Namespace     string

	CPURequestSumMC  int64
	CPULimitSumMC    int64
	CPUUsageAvgMC    int64
	CPUUsageMaxMC    int64
	CPUUsageMinMC    int64
	CPUThrottleAvgMC int64
	CPUThrottleMaxMC int64
	MemRequestSumKiB int64
	MemLimitSumKiB   int64
	MemUsageAvgKiB   int64
	MemUsageMaxKiB   int64
	MemUsageMinKiB   int64
	MemRSSAvgKiB     int64
	MemRSSMaxKiB     int64

	// ResourceQuota hard limits (operator maps *_namespace_sum to type=hard).
	CPURequestHardMC       int64
	CPULimitHardMC         int64
	MemoryRequestHardBytes int64
	MemoryLimitHardBytes   int64
	// ResourceQuota used consumption (optional CSV columns).
	CPURequestUsedMC       int64
	CPULimitUsedMC         int64
	MemoryRequestUsedBytes int64
	MemoryLimitUsedBytes   int64
}

// NamespaceDigestKey uniquely identifies a namespace-day and schedule stream.
type NamespaceDigestKey struct {
	OrgID        string
	ClusterUUID  string
	Namespace    string
	BucketDate   time.Time
	ScheduleType ScheduleType
}

// NamespaceDigestResult holds computed digest columns for a single
// namespace-day, matching the daily_namespace_digests table schema.
type NamespaceDigestResult struct {
	Key              NamespaceDigestKey
	CPURequestP50MC  int64
	CPURequestP60MC  int64
	CPURequestP95MC  int64
	CPURequestP98MC  int64
	CPURequestP99MC  int64
	CPUUsageP50MC    int64
	CPUUsageP60MC    int64
	CPUUsageP95MC    int64
	CPUUsageP98MC    int64
	CPUUsageP99MC    int64
	CPUUsageMaxMC    int64
	MemRequestP50KiB int64
	MemRequestP60KiB int64
	MemRequestP95KiB int64
	MemRequestP98KiB int64
	MemRequestP99KiB int64
	MemUsageP50KiB   int64
	MemUsageP60KiB   int64
	MemUsageP95KiB   int64
	MemUsageP98KiB   int64
	MemUsageP99KiB   int64
	MemUsageMaxKiB   int64
	CPUUsageMeanMC   int64
	MemUsageMeanKiB  int64
	SampleCount      int64

	CPURequestHardMC       int64
	CPULimitHardMC         int64
	MemoryRequestHardBytes int64
	MemoryLimitHardBytes   int64
	CPURequestUsedMC       int64
	CPULimitUsedMC         int64
	MemoryRequestUsedBytes int64
	MemoryLimitUsedBytes   int64
}

type nsColumnIndex struct {
	intervalStart  int
	intervalEnd    int
	namespace      int
	cpuRequestSum  int
	cpuLimitSum    int
	cpuUsageAvg    int
	cpuUsageMax    int
	cpuUsageMin    int
	cpuThrottleAvg int
	cpuThrottleMax int
	memRequestSum  int
	memLimitSum    int
	memUsageAvg    int
	memUsageMax    int
	memUsageMin    int
	memRSSUsageAvg int
	memRSSUsageMax int
	cpuRequestUsed int
	cpuLimitUsed   int
	memRequestUsed int
	memLimitUsed   int
}

// buildNSColumnIndex maps CSV headers to column indices. Parsing is header-based, not
// positional: older operator CSVs without *_namespace_used columns leave those indices
// at -1 and parseNSRecord leaves the corresponding used fields at zero (stored as NULL).
func buildNSColumnIndex(header []string) (nsColumnIndex, error) {
	idx := nsColumnIndex{
		intervalStart: -1, intervalEnd: -1, namespace: -1,
		cpuRequestSum: -1, cpuLimitSum: -1,
		cpuUsageAvg: -1, cpuUsageMax: -1, cpuUsageMin: -1,
		cpuThrottleAvg: -1, cpuThrottleMax: -1,
		memRequestSum: -1, memLimitSum: -1,
		memUsageAvg: -1, memUsageMax: -1, memUsageMin: -1,
		memRSSUsageAvg: -1, memRSSUsageMax: -1,
		cpuRequestUsed: -1, cpuLimitUsed: -1,
		memRequestUsed: -1, memLimitUsed: -1,
	}
	for i, col := range header {
		switch col {
		case "interval_start":
			idx.intervalStart = i
		case "interval_end":
			idx.intervalEnd = i
		case "namespace":
			idx.namespace = i
		case "cpu_request_namespace_sum":
			idx.cpuRequestSum = i
		case "cpu_limit_namespace_sum":
			idx.cpuLimitSum = i
		case "cpu_usage_namespace_avg":
			idx.cpuUsageAvg = i
		case "cpu_usage_namespace_max":
			idx.cpuUsageMax = i
		case "cpu_usage_namespace_min":
			idx.cpuUsageMin = i
		case "cpu_throttle_namespace_avg":
			idx.cpuThrottleAvg = i
		case "cpu_throttle_namespace_max":
			idx.cpuThrottleMax = i
		case "memory_request_namespace_sum":
			idx.memRequestSum = i
		case "memory_limit_namespace_sum":
			idx.memLimitSum = i
		case "memory_usage_namespace_avg":
			idx.memUsageAvg = i
		case "memory_usage_namespace_max":
			idx.memUsageMax = i
		case "memory_usage_namespace_min":
			idx.memUsageMin = i
		case "memory_rss_usage_namespace_avg":
			idx.memRSSUsageAvg = i
		case "memory_rss_usage_namespace_max":
			idx.memRSSUsageMax = i
		case "cpu_request_namespace_used":
			idx.cpuRequestUsed = i
		case "cpu_limit_namespace_used":
			idx.cpuLimitUsed = i
		case "memory_request_namespace_used":
			idx.memRequestUsed = i
		case "memory_limit_namespace_used":
			idx.memLimitUsed = i
		}
	}
	required := []struct {
		name string
		val  int
	}{
		{"interval_start", idx.intervalStart},
		{"interval_end", idx.intervalEnd},
		{"namespace", idx.namespace},
		{"cpu_request_namespace_sum", idx.cpuRequestSum},
		{"cpu_usage_namespace_avg", idx.cpuUsageAvg},
		{"memory_request_namespace_sum", idx.memRequestSum},
		{"memory_usage_namespace_avg", idx.memUsageAvg},
	}
	for _, r := range required {
		if r.val < 0 {
			return idx, fmt.Errorf("ParseNamespaceCSVRows: missing required column %q", r.name)
		}
	}
	return idx, nil
}

// ParseNamespaceCSVRows reads a namespace metrics CSV and converts numeric
// columns to integer types (millicores, KiB). Malformed rows are skipped.
func ParseNamespaceCSVRows(r io.Reader) ([]NamespaceMetricRow, error) {
	reader := csv.NewReader(r)
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("ParseNamespaceCSVRows: reading header: %w", err)
	}

	idx, err := buildNSColumnIndex(header)
	if err != nil {
		return nil, err
	}

	var rows []NamespaceMetricRow
	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("ParseNamespaceCSVRows: reading line %d: %w", lineNum+1, err)
		}
		lineNum++

		row, parseErr := parseNSRecord(record, idx)
		if parseErr != nil {
			logging.GetLogger().Debugf("ParseNamespaceCSVRows: skipping line %d: %v", lineNum, parseErr)
			continue
		}
		if valErr := ValidateNamespaceMetricRow(row); valErr != nil {
			logging.GetLogger().Debugf("ParseNamespaceCSVRows: skipping line %d: %v", lineNum, valErr)
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseNSRecord(record []string, idx nsColumnIndex) (NamespaceMetricRow, error) {
	var row NamespaceMetricRow
	var err error

	row.IntervalStart, err = parseFlexibleTimestamp(strings.TrimSpace(record[idx.intervalStart]))
	if err != nil {
		return row, fmt.Errorf("interval_start: %w", err)
	}
	row.IntervalEnd, err = parseFlexibleTimestamp(strings.TrimSpace(record[idx.intervalEnd]))
	if err != nil {
		return row, fmt.Errorf("interval_end: %w", err)
	}

	row.Namespace = record[idx.namespace]

	row.CPURequestSumMC, err = CoreToMillicores(record[idx.cpuRequestSum])
	if err != nil {
		return row, err
	}
	row.CPURequestHardMC = row.CPURequestSumMC
	row.CPUUsageAvgMC, err = CoreToMillicores(record[idx.cpuUsageAvg])
	if err != nil {
		return row, err
	}
	row.MemRequestSumKiB, err = BytesToKiB(record[idx.memRequestSum])
	if err != nil {
		return row, err
	}
	row.MemoryRequestHardBytes = row.MemRequestSumKiB * 1024
	row.MemUsageAvgKiB, err = BytesToKiB(record[idx.memUsageAvg])
	if err != nil {
		return row, err
	}

	if idx.cpuLimitSum >= 0 && idx.cpuLimitSum < len(record) && record[idx.cpuLimitSum] != "" {
		row.CPULimitSumMC, err = CoreToMillicores(record[idx.cpuLimitSum])
		if err != nil {
			return row, err
		}
		row.CPULimitHardMC = row.CPULimitSumMC
	}
	if idx.cpuUsageMax >= 0 && idx.cpuUsageMax < len(record) && record[idx.cpuUsageMax] != "" {
		row.CPUUsageMaxMC, err = CoreToMillicores(record[idx.cpuUsageMax])
		if err != nil {
			return row, err
		}
	}
	if idx.cpuUsageMin >= 0 && idx.cpuUsageMin < len(record) && record[idx.cpuUsageMin] != "" {
		row.CPUUsageMinMC, err = CoreToMillicores(record[idx.cpuUsageMin])
		if err != nil {
			return row, err
		}
	}
	if idx.cpuThrottleAvg >= 0 && idx.cpuThrottleAvg < len(record) && record[idx.cpuThrottleAvg] != "" {
		row.CPUThrottleAvgMC, err = CoreToMillicores(record[idx.cpuThrottleAvg])
		if err != nil {
			return row, err
		}
	}
	if idx.cpuThrottleMax >= 0 && idx.cpuThrottleMax < len(record) && record[idx.cpuThrottleMax] != "" {
		row.CPUThrottleMaxMC, err = CoreToMillicores(record[idx.cpuThrottleMax])
		if err != nil {
			return row, err
		}
	}
	if idx.memLimitSum >= 0 && idx.memLimitSum < len(record) && record[idx.memLimitSum] != "" {
		row.MemLimitSumKiB, err = BytesToKiB(record[idx.memLimitSum])
		if err != nil {
			return row, err
		}
		row.MemoryLimitHardBytes = row.MemLimitSumKiB * 1024
	}
	if idx.cpuRequestUsed >= 0 && idx.cpuRequestUsed < len(record) && record[idx.cpuRequestUsed] != "" {
		row.CPURequestUsedMC, err = CoreToMillicores(record[idx.cpuRequestUsed])
		if err != nil {
			return row, err
		}
	}
	if idx.cpuLimitUsed >= 0 && idx.cpuLimitUsed < len(record) && record[idx.cpuLimitUsed] != "" {
		row.CPULimitUsedMC, err = CoreToMillicores(record[idx.cpuLimitUsed])
		if err != nil {
			return row, err
		}
	}
	if idx.memRequestUsed >= 0 && idx.memRequestUsed < len(record) && record[idx.memRequestUsed] != "" {
		usedKiB, err := BytesToKiB(record[idx.memRequestUsed])
		if err != nil {
			return row, err
		}
		row.MemoryRequestUsedBytes = usedKiB * 1024
	}
	if idx.memLimitUsed >= 0 && idx.memLimitUsed < len(record) && record[idx.memLimitUsed] != "" {
		usedKiB, err := BytesToKiB(record[idx.memLimitUsed])
		if err != nil {
			return row, err
		}
		row.MemoryLimitUsedBytes = usedKiB * 1024
	}
	if idx.memUsageMax >= 0 && idx.memUsageMax < len(record) && record[idx.memUsageMax] != "" {
		row.MemUsageMaxKiB, err = BytesToKiB(record[idx.memUsageMax])
		if err != nil {
			return row, err
		}
	}
	if idx.memUsageMin >= 0 && idx.memUsageMin < len(record) && record[idx.memUsageMin] != "" {
		row.MemUsageMinKiB, err = BytesToKiB(record[idx.memUsageMin])
		if err != nil {
			return row, err
		}
	}
	if idx.memRSSUsageAvg >= 0 && idx.memRSSUsageAvg < len(record) && record[idx.memRSSUsageAvg] != "" {
		row.MemRSSAvgKiB, err = BytesToKiB(record[idx.memRSSUsageAvg])
		if err != nil {
			return row, err
		}
	}
	if idx.memRSSUsageMax >= 0 && idx.memRSSUsageMax < len(record) && record[idx.memRSSUsageMax] != "" {
		row.MemRSSMaxKiB, err = BytesToKiB(record[idx.memRSSUsageMax])
		if err != nil {
			return row, err
		}
	}

	return row, nil
}

// ValidateNamespaceMetricRow checks that core numeric fields in a
// NamespaceMetricRow are non-negative. Returns an error describing the first
// invalid field found.
func ValidateNamespaceMetricRow(row NamespaceMetricRow) error {
	checks := []struct {
		name string
		val  int64
	}{
		{"CPURequestSumMC", row.CPURequestSumMC},
		{"CPULimitSumMC", row.CPULimitSumMC},
		{"CPUUsageAvgMC", row.CPUUsageAvgMC},
		{"MemRequestSumKiB", row.MemRequestSumKiB},
		{"MemLimitSumKiB", row.MemLimitSumKiB},
		{"MemUsageAvgKiB", row.MemUsageAvgKiB},
	}
	for _, c := range checks {
		if c.val < 0 {
			return fmt.Errorf("ValidateNamespaceMetricRow: %s is negative (%d)", c.name, c.val)
		}
	}
	return nil
}

// GroupNamespaceCSVRows groups namespace metric rows by (namespace, day) for all_hours.
func GroupNamespaceCSVRows(rows []NamespaceMetricRow, orgID, clusterUUID string) map[NamespaceDigestKey][]NamespaceMetricRow {
	return GroupNamespaceCSVRowsForStream(rows, orgID, clusterUUID, ScheduleTypeAllHours, nil)
}

// NamespaceRowWeightFunc returns schedule weight for a namespace CSV row.
type NamespaceRowWeightFunc func(NamespaceMetricRow) float64

// GroupNamespaceCSVRowsForStream groups rows by namespace-day and schedule_type.
func GroupNamespaceCSVRowsForStream(
	rows []NamespaceMetricRow,
	orgID, clusterUUID string,
	scheduleType ScheduleType,
	weightFn NamespaceRowWeightFunc,
) map[NamespaceDigestKey][]NamespaceMetricRow {
	groups := make(map[NamespaceDigestKey][]NamespaceMetricRow)
	for _, row := range rows {
		if weightFn != nil {
			if w := weightFn(row); w <= 0 {
				continue
			}
		}
		bucketDate := time.Date(
			row.IntervalStart.Year(), row.IntervalStart.Month(), row.IntervalStart.Day(),
			0, 0, 0, 0, time.UTC,
		)
		key := NamespaceDigestKey{
			OrgID:        orgID,
			ClusterUUID:  clusterUUID,
			Namespace:    row.Namespace,
			BucketDate:   bucketDate,
			ScheduleType: scheduleType,
		}
		groups[key] = append(groups[key], row)
	}
	return groups
}

func namespaceBusinessHoursRowWeightFn(sched bhschedule.Schedule) NamespaceRowWeightFunc {
	if !sched.Enabled {
		return nil
	}
	skipZero := sched.OffHoursWeight == 0
	return func(row NamespaceMetricRow) float64 {
		w := bhschedule.ScheduleWeight(row.IntervalStart, sched)
		if skipZero && w <= 0 {
			return 0
		}
		return w
	}
}

// ComputeNamespaceDigest computes digest columns for a namespace-day group.
func ComputeNamespaceDigest(key NamespaceDigestKey, rows []NamespaceMetricRow) NamespaceDigestResult {
	return ComputeNamespaceDigestWeighted(key, rows, nil)
}

// ComputeNamespaceDigestWeighted computes namespace digests with optional per-row weights.
func ComputeNamespaceDigestWeighted(key NamespaceDigestKey, rows []NamespaceMetricRow, weightFn NamespaceRowWeightFunc) NamespaceDigestResult {
	var cpuReqD, cpuUseD, memReqD, memUseD Digest
	if weightFn == nil {
		cpuRequests := extractNSField(rows, func(r NamespaceMetricRow) int64 { return r.CPURequestSumMC })
		cpuUsages := extractNSField(rows, func(r NamespaceMetricRow) int64 { return r.CPUUsageAvgMC })
		memRequests := extractNSField(rows, func(r NamespaceMetricRow) int64 { return r.MemRequestSumKiB })
		memUsages := extractNSField(rows, func(r NamespaceMetricRow) int64 { return r.MemUsageAvgKiB })
		cpuReqD = ComputeDigest(cpuRequests)
		cpuUseD = ComputeDigest(cpuUsages)
		memReqD = ComputeDigest(memRequests)
		memUseD = ComputeDigest(memUsages)
	} else {
		cpuReqD = computeWeightedNSFieldDigest(rows, weightFn, func(r NamespaceMetricRow) int64 { return r.CPURequestSumMC })
		cpuUseD = computeWeightedNSFieldDigest(rows, weightFn, func(r NamespaceMetricRow) int64 { return r.CPUUsageAvgMC })
		memReqD = computeWeightedNSFieldDigest(rows, weightFn, func(r NamespaceMetricRow) int64 { return r.MemRequestSumKiB })
		memUseD = computeWeightedNSFieldDigest(rows, weightFn, func(r NamespaceMetricRow) int64 { return r.MemUsageAvgKiB })
	}

	// For max, use the per-interval max column if available; fall back to
	// the digest max of the avg column.
	cpuUsageMax := cpuUseD.Max
	if maxVals := extractNSField(rows, func(r NamespaceMetricRow) int64 { return r.CPUUsageMaxMC }); len(maxVals) > 0 {
		d := ComputeDigest(maxVals)
		if d.Max > cpuUsageMax {
			cpuUsageMax = d.Max
		}
	}
	memUsageMax := memUseD.Max
	if maxVals := extractNSField(rows, func(r NamespaceMetricRow) int64 { return r.MemUsageMaxKiB }); len(maxVals) > 0 {
		d := ComputeDigest(maxVals)
		if d.Max > memUsageMax {
			memUsageMax = d.Max
		}
	}

	quotaHardUsed := computeNamespaceQuotaSnapshot(rows)

	return NamespaceDigestResult{
		Key:              key,
		CPURequestP50MC:  cpuReqD.P50,
		CPURequestP60MC:  cpuReqD.P60,
		CPURequestP95MC:  cpuReqD.P95,
		CPURequestP98MC:  cpuReqD.P98,
		CPURequestP99MC:  cpuReqD.P99,
		CPUUsageP50MC:    cpuUseD.P50,
		CPUUsageP60MC:    cpuUseD.P60,
		CPUUsageP95MC:    cpuUseD.P95,
		CPUUsageP98MC:    cpuUseD.P98,
		CPUUsageP99MC:    cpuUseD.P99,
		CPUUsageMaxMC:    cpuUsageMax,
		MemRequestP50KiB: memReqD.P50,
		MemRequestP60KiB: memReqD.P60,
		MemRequestP95KiB: memReqD.P95,
		MemRequestP98KiB: memReqD.P98,
		MemRequestP99KiB: memReqD.P99,
		MemUsageP50KiB:   memUseD.P50,
		MemUsageP60KiB:   memUseD.P60,
		MemUsageP95KiB:   memUseD.P95,
		MemUsageP98KiB:   memUseD.P98,
		MemUsageP99KiB:   memUseD.P99,
		MemUsageMaxKiB:   memUsageMax,
		CPUUsageMeanMC:   cpuUseD.Mean,
		MemUsageMeanKiB:  memUseD.Mean,
		SampleCount:      cpuUseD.Count,

		CPURequestHardMC:       quotaHardUsed.CPURequestHardMC,
		CPULimitHardMC:         quotaHardUsed.CPULimitHardMC,
		MemoryRequestHardBytes: quotaHardUsed.MemoryRequestHardBytes,
		MemoryLimitHardBytes:   quotaHardUsed.MemoryLimitHardBytes,
		CPURequestUsedMC:       quotaHardUsed.CPURequestUsedMC,
		CPULimitUsedMC:         quotaHardUsed.CPULimitUsedMC,
		MemoryRequestUsedBytes: quotaHardUsed.MemoryRequestUsedBytes,
		MemoryLimitUsedBytes:   quotaHardUsed.MemoryLimitUsedBytes,
	}
}

func computeNamespaceQuotaSnapshot(rows []NamespaceMetricRow) NamespaceDigestResult {
	var snap NamespaceDigestResult
	for _, r := range rows {
		snap.CPURequestHardMC = maxInt64NS(snap.CPURequestHardMC, r.CPURequestHardMC)
		snap.CPULimitHardMC = maxInt64NS(snap.CPULimitHardMC, r.CPULimitHardMC)
		snap.MemoryRequestHardBytes = maxInt64NS(snap.MemoryRequestHardBytes, r.MemoryRequestHardBytes)
		snap.MemoryLimitHardBytes = maxInt64NS(snap.MemoryLimitHardBytes, r.MemoryLimitHardBytes)
		snap.CPURequestUsedMC = maxInt64NS(snap.CPURequestUsedMC, r.CPURequestUsedMC)
		snap.CPULimitUsedMC = maxInt64NS(snap.CPULimitUsedMC, r.CPULimitUsedMC)
		snap.MemoryRequestUsedBytes = maxInt64NS(snap.MemoryRequestUsedBytes, r.MemoryRequestUsedBytes)
		snap.MemoryLimitUsedBytes = maxInt64NS(snap.MemoryLimitUsedBytes, r.MemoryLimitUsedBytes)
	}
	return snap
}

func maxInt64NS(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func extractNSField(rows []NamespaceMetricRow, fn func(NamespaceMetricRow) int64) []int64 {
	vals := make([]int64, len(rows))
	for i, r := range rows {
		vals[i] = fn(r)
	}
	return vals
}

func computeWeightedNSFieldDigest(rows []NamespaceMetricRow, weightFn NamespaceRowWeightFunc, fieldFn func(NamespaceMetricRow) int64) Digest {
	vals := make([]int64, 0, len(rows))
	weights := make([]float64, 0, len(rows))
	for _, r := range rows {
		w := weightFn(r)
		if w <= 0 {
			continue
		}
		vals = append(vals, fieldFn(r))
		weights = append(weights, w)
	}
	return ComputeWeightedDigest(vals, weights)
}

func buildNamespaceBusinessHoursGroups(
	rows []NamespaceMetricRow,
	orgID, clusterUUID string,
	cache *bhschedule.Cache,
) map[NamespaceDigestKey][]NamespaceMetricRow {
	if cache == nil {
		return nil
	}
	byNS := make(map[string][]NamespaceMetricRow)
	for _, row := range rows {
		byNS[row.Namespace] = append(byNS[row.Namespace], row)
	}
	out := make(map[NamespaceDigestKey][]NamespaceMetricRow)
	for ns, nsRows := range byNS {
		sched := cache.Resolve(ns)
		if !sched.Enabled {
			continue
		}
		weightFn := namespaceBusinessHoursRowWeightFn(sched)
		for k, g := range GroupNamespaceCSVRowsForStream(nsRows, orgID, clusterUUID, ScheduleTypeBusinessHours, weightFn) {
			out[k] = g
		}
	}
	return out
}

func mergeNamespaceDigestGroups(all, bh map[NamespaceDigestKey][]NamespaceMetricRow) map[NamespaceDigestKey][]NamespaceMetricRow {
	merged := make(map[NamespaceDigestKey][]NamespaceMetricRow, len(all)+len(bh))
	for k, g := range all {
		merged[k] = g
	}
	for k, g := range bh {
		merged[k] = g
	}
	return merged
}

func namespaceRowWeightFnForKey(key NamespaceDigestKey, cache *bhschedule.Cache) NamespaceRowWeightFunc {
	if key.ScheduleType != ScheduleTypeBusinessHours || cache == nil {
		return nil
	}
	sched := cache.Resolve(key.Namespace)
	if !sched.Enabled {
		return nil
	}
	return namespaceBusinessHoursRowWeightFn(sched)
}

func upsertNamespaceDigests(
	ctx context.Context,
	pool *pgxpool.Pool,
	grouped map[NamespaceDigestKey][]NamespaceMetricRow,
	scheduleCache *bhschedule.Cache,
) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin namespace digest tx: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for key, group := range grouped {
		weightFn := namespaceRowWeightFnForKey(key, scheduleCache)
		d := ComputeNamespaceDigestWeighted(key, group, weightFn)
		batch.Queue(`
			INSERT INTO daily_namespace_digests (
				bucket_date, org_id, cluster_uuid, namespace, schedule_type,
				cpu_request_p50_mc, cpu_request_p60_mc, cpu_request_p95_mc, cpu_request_p98_mc, cpu_request_p99_mc,
				cpu_usage_p50_mc, cpu_usage_p60_mc, cpu_usage_p95_mc, cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
				memory_request_p50_kib, memory_request_p60_kib, memory_request_p95_kib, memory_request_p98_kib, memory_request_p99_kib,
				memory_usage_p50_kib, memory_usage_p60_kib, memory_usage_p95_kib, memory_usage_p98_kib, memory_usage_p99_kib, memory_usage_max_kib,
				cpu_usage_mean_mc, memory_usage_mean_kib, sample_count,
				cpu_request_hard_millicores, cpu_limit_hard_millicores,
				memory_request_hard_bytes, memory_limit_hard_bytes,
				cpu_request_used_millicores, cpu_limit_used_millicores,
				memory_request_used_bytes, memory_limit_used_bytes
			) VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8, $9, $10,
				$11, $12, $13, $14, $15, $16,
				$17, $18, $19, $20, $21,
				$22, $23, $24, $25, $26, $27,
				$28, $29, $30,
				$31, $32, $33, $34, $35, $36, $37, $38
			)
			ON CONFLICT (org_id, cluster_uuid, namespace, bucket_date, schedule_type)
			DO UPDATE SET
				cpu_request_p50_mc = EXCLUDED.cpu_request_p50_mc,
				cpu_request_p60_mc = EXCLUDED.cpu_request_p60_mc,
				cpu_request_p95_mc = EXCLUDED.cpu_request_p95_mc,
				cpu_request_p98_mc = EXCLUDED.cpu_request_p98_mc,
				cpu_request_p99_mc = EXCLUDED.cpu_request_p99_mc,
				cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc,
				cpu_usage_p60_mc = EXCLUDED.cpu_usage_p60_mc,
				cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc,
				cpu_usage_p98_mc = EXCLUDED.cpu_usage_p98_mc,
				cpu_usage_p99_mc = EXCLUDED.cpu_usage_p99_mc,
				cpu_usage_max_mc = EXCLUDED.cpu_usage_max_mc,
				memory_request_p50_kib = EXCLUDED.memory_request_p50_kib,
				memory_request_p60_kib = EXCLUDED.memory_request_p60_kib,
				memory_request_p95_kib = EXCLUDED.memory_request_p95_kib,
				memory_request_p98_kib = EXCLUDED.memory_request_p98_kib,
				memory_request_p99_kib = EXCLUDED.memory_request_p99_kib,
				memory_usage_p50_kib = EXCLUDED.memory_usage_p50_kib,
				memory_usage_p60_kib = EXCLUDED.memory_usage_p60_kib,
				memory_usage_p95_kib = EXCLUDED.memory_usage_p95_kib,
				memory_usage_p98_kib = EXCLUDED.memory_usage_p98_kib,
				memory_usage_p99_kib = EXCLUDED.memory_usage_p99_kib,
				memory_usage_max_kib = EXCLUDED.memory_usage_max_kib,
				cpu_usage_mean_mc = EXCLUDED.cpu_usage_mean_mc,
				memory_usage_mean_kib = EXCLUDED.memory_usage_mean_kib,
				sample_count = EXCLUDED.sample_count,
				cpu_request_hard_millicores = EXCLUDED.cpu_request_hard_millicores,
				cpu_limit_hard_millicores = EXCLUDED.cpu_limit_hard_millicores,
				memory_request_hard_bytes = EXCLUDED.memory_request_hard_bytes,
				memory_limit_hard_bytes = EXCLUDED.memory_limit_hard_bytes,
				cpu_request_used_millicores = EXCLUDED.cpu_request_used_millicores,
				cpu_limit_used_millicores = EXCLUDED.cpu_limit_used_millicores,
				memory_request_used_bytes = EXCLUDED.memory_request_used_bytes,
				memory_limit_used_bytes = EXCLUDED.memory_limit_used_bytes`,
			key.BucketDate.Format("2006-01-02"),
			key.OrgID, key.ClusterUUID, key.Namespace, string(key.ScheduleType),
			d.CPURequestP50MC, d.CPURequestP60MC, d.CPURequestP95MC, d.CPURequestP98MC, d.CPURequestP99MC,
			d.CPUUsageP50MC, d.CPUUsageP60MC, d.CPUUsageP95MC, d.CPUUsageP98MC, d.CPUUsageP99MC, d.CPUUsageMaxMC,
			d.MemRequestP50KiB, d.MemRequestP60KiB, d.MemRequestP95KiB, d.MemRequestP98KiB, d.MemRequestP99KiB,
			d.MemUsageP50KiB, d.MemUsageP60KiB, d.MemUsageP95KiB, d.MemUsageP98KiB, d.MemUsageP99KiB, d.MemUsageMaxKiB,
			d.CPUUsageMeanMC, d.MemUsageMeanKiB, d.SampleCount,
			d.CPURequestHardMC, d.CPULimitHardMC, d.MemoryRequestHardBytes, d.MemoryLimitHardBytes,
			d.CPURequestUsedMC, d.CPULimitUsedMC, d.MemoryRequestUsedBytes, d.MemoryLimitUsedBytes,
		)
	}

	br := tx.SendBatch(ctx, batch)
	for range grouped {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return fmt.Errorf("upsert namespace digest: %w", err)
		}
	}
	br.Close()

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit namespace digests: %w", err)
	}
	return nil
}

// EnsureNamespaceSamplePartitions creates monthly partitions of
// namespace_usage_samples for every month present in the ingested rows.
func EnsureNamespaceSamplePartitions(ctx context.Context, pool *pgxpool.Pool, rows []NamespaceMetricRow) {
	months := map[time.Time]struct{}{}
	for _, r := range rows {
		monthStart := time.Date(r.IntervalStart.Year(), r.IntervalStart.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[monthStart] = struct{}{}
	}
	for monthStart := range months {
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("namespace_usage_samples_%s", monthStart.Format("200601"))
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF namespace_usage_samples FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			logging.GetLogger().Warnf("EnsureNamespaceSamplePartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}

// upsertNamespaceUsageSamples batch-upserts raw namespace CSV rows into namespace_usage_samples.
func upsertNamespaceUsageSamples(ctx context.Context, pool *pgxpool.Pool, rows []NamespaceMetricRow, orgID, clusterUUID string) error {
	if len(rows) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, r := range rows {
		batch.Queue(`
			INSERT INTO namespace_usage_samples (
				sample_time, org_id, cluster_uuid, namespace,
				cpu_usage_mc, mem_usage_kib
			) VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (org_id, cluster_uuid, namespace, sample_time)
			DO UPDATE SET
				cpu_usage_mc = EXCLUDED.cpu_usage_mc,
				mem_usage_kib = EXCLUDED.mem_usage_kib`,
			r.IntervalStart, orgID, clusterUUID, r.Namespace,
			r.CPUUsageAvgMC, r.MemUsageAvgKiB,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range rows {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("upsert namespace usage sample: %w", err)
		}
	}
	return nil
}

// EnsureNamespaceDigestPartitions creates monthly partitions of
// daily_namespace_digests for months that appear in the grouped data.
func EnsureNamespaceDigestPartitions(ctx context.Context, pool *pgxpool.Pool, keys []NamespaceDigestKey) {
	months := map[time.Time]struct{}{}
	for _, k := range keys {
		monthStart := time.Date(k.BucketDate.Year(), k.BucketDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[monthStart] = struct{}{}
	}
	for monthStart := range months {
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("daily_namespace_digests_%s", monthStart.Format("200601"))
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_namespace_digests FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			logging.GetLogger().Warnf("EnsureNamespaceDigestPartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}

// ProcessNamespaceCSVToDigests is the namespace ingestion pipeline:
// parse CSV -> group by namespace+day -> compute digests -> upsert to DB.
func ProcessNamespaceCSVToDigests(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseNamespaceCSVRows(r)
	if err != nil {
		return fmt.Errorf("parse namespace CSV: %w", err)
	}
	if len(rows) == 0 {
		logging.ForOrg(orgID, clusterUUID).Info("ProcessNamespaceCSVToDigests: no rows parsed")
		return nil
	}

	// Persist raw samples for namespace boxplot computation at query time.
	EnsureNamespaceSamplePartitions(ctx, pool, rows)
	if err := upsertNamespaceUsageSamples(ctx, pool, rows, orgID, clusterUUID); err != nil {
		return fmt.Errorf("upsert namespace usage samples: %w", err)
	}

	groupedAll := GroupNamespaceCSVRows(rows, orgID, clusterUUID)

	var scheduleCache *bhschedule.Cache
	if BusinessHoursAggregationEnabled() {
		var loadErr error
		scheduleCache, loadErr = bhschedule.LoadSchedules(ctx, pool, orgID, clusterUUID)
		if loadErr != nil {
			return fmt.Errorf("load business hours schedules: %w", loadErr)
		}
		if scheduleCache != nil && !scheduleCache.ProducesBusinessHoursDigests() {
			if err := pruneBusinessHoursDigests(ctx, pool, orgID, clusterUUID); err != nil {
				return err
			}
		}
	}
	groupedBH := buildNamespaceBusinessHoursGroups(rows, orgID, clusterUUID, scheduleCache)
	grouped := mergeNamespaceDigestGroups(groupedAll, groupedBH)
	logging.ForOrg(orgID, clusterUUID).Infof("ProcessNamespaceCSVToDigests: %d rows -> %d all_hours groups, %d business_hours groups",
		len(rows), len(groupedAll), len(groupedBH))

	digestKeys := make([]NamespaceDigestKey, 0, len(grouped))
	for k := range grouped {
		digestKeys = append(digestKeys, k)
	}
	EnsureNamespaceDigestPartitions(ctx, pool, digestKeys)

	if err := upsertNamespaceDigests(ctx, pool, grouped, scheduleCache); err != nil {
		return err
	}

	logging.ForOrg(orgID, clusterUUID).Infof("ProcessNamespaceCSVToDigests: upserted %d digests",
		len(grouped))
	return nil
}
