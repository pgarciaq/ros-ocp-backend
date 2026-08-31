package ingestion

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// NamespaceMetricRow is a parsed ROS namespace CSV row (librobne/csv.NamespaceRow).
// Extra ClusterID exists on the shared type; ingest may ignore it.
// CPURequestMC / CPUUsageMC / MemRequestKiB / MemUsageKiB are the *_sum / *_avg
// columns (formerly CPURequestSumMC / CPUUsageAvgMC / MemRequestSumKiB / MemUsageAvgKiB).
type NamespaceMetricRow = libcsv.NamespaceRow

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

// forEachNamespaceCSVRow parses namespace CSV rows one at a time without retaining a full-slice copy.
func forEachNamespaceCSVRow(ctx context.Context, r io.Reader, fn func(NamespaceMetricRow) error) (int, error) {
	count := 0
	validatorSkipped := 0
	skipped, err := libcsv.ForEachNamespace(ctx, r, func(row libcsv.NamespaceRow) error {
		if valErr := ValidateNamespaceMetricRow(row); valErr != nil {
			logging.GetLogger().Debugf("ParseNamespaceCSVRows: skipping row: %v", valErr)
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
		metrics.IncCSVRowsSkipped("namespace", totalSkipped)
		logging.GetLogger().Warnf("ParseNamespaceCSVRows: skipped %d malformed or invalid rows", totalSkipped)
	}
	return count, err
}

// ParseNamespaceCSVRows reads a namespace metrics CSV and converts numeric
// columns to integer types (millicores, KiB). Malformed rows are skipped.
func ParseNamespaceCSVRows(r io.Reader) ([]NamespaceMetricRow, error) {
	var rows []NamespaceMetricRow
	_, err := forEachNamespaceCSVRow(context.Background(), r, func(row NamespaceMetricRow) error {
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

// ValidateNamespaceMetricRow checks that core numeric fields in a
// NamespaceMetricRow are non-negative. Returns an error describing the first
// invalid field found.
func ValidateNamespaceMetricRow(row NamespaceMetricRow) error {
	checks := []struct {
		name string
		val  int64
	}{
		{"CPURequestMC", row.CPURequestMC},
		{"CPULimitMC", row.CPULimitMC},
		{"CPUUsageMC", row.CPUUsageMC},
		{"MemRequestKiB", row.MemRequestKiB},
		{"MemLimitKiB", row.MemLimitKiB},
		{"MemUsageKiB", row.MemUsageKiB},
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

// dedupeNamespaceRowsForUsageDigest keeps one row per namespace+interval when the operator
// emits multiple CSV lines per namespace (one per ResourceQuota object).
func dedupeNamespaceRowsForUsageDigest(rows []NamespaceMetricRow) []NamespaceMetricRow {
	seen := make(map[string]struct{})
	out := make([]NamespaceMetricRow, 0, len(rows))
	for _, row := range rows {
		key := row.Namespace + "|" + row.IntervalStart.UTC().Format(time.RFC3339Nano)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	return out
}

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
		cpuRequests := extractNSField(rows, func(r NamespaceMetricRow) int64 { return r.CPURequestMC })
		cpuUsages := extractNSField(rows, func(r NamespaceMetricRow) int64 { return r.CPUUsageMC })
		memRequests := extractNSField(rows, func(r NamespaceMetricRow) int64 { return r.MemRequestKiB })
		memUsages := extractNSField(rows, func(r NamespaceMetricRow) int64 { return r.MemUsageKiB })
		cpuReqD = ComputeDigest(cpuRequests)
		cpuUseD = ComputeDigest(cpuUsages)
		memReqD = ComputeDigest(memRequests)
		memUseD = ComputeDigest(memUsages)
	} else {
		cpuReqD = computeWeightedNSFieldDigest(rows, weightFn, func(r NamespaceMetricRow) int64 { return r.CPURequestMC })
		cpuUseD = computeWeightedNSFieldDigest(rows, weightFn, func(r NamespaceMetricRow) int64 { return r.CPUUsageMC })
		memReqD = computeWeightedNSFieldDigest(rows, weightFn, func(r NamespaceMetricRow) int64 { return r.MemRequestKiB })
		memUseD = computeWeightedNSFieldDigest(rows, weightFn, func(r NamespaceMetricRow) int64 { return r.MemUsageKiB })
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
// stream CSV rows -> group by namespace+day -> compute digests -> upsert to DB.
func ProcessNamespaceCSVToDigests(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	_, err := parseAndDigestNamespaceCSVStream(ctx, pool, r, orgID, clusterUUID)
	if err != nil {
		return fmt.Errorf("parse namespace CSV: %w", err)
	}
	return nil
}
