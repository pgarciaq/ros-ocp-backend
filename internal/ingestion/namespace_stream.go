package ingestion

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

func namespaceDigestGroupCount(all, bh map[NamespaceDigestKey][]NamespaceMetricRow) int {
	return len(all) + len(bh)
}

func namespaceUsageDedupeKey(row NamespaceMetricRow) string {
	return row.Namespace + "|" + row.IntervalStart.UTC().Format(time.RFC3339Nano)
}

func appendNamespaceUsageDigestRow(
	groups map[NamespaceDigestKey][]NamespaceMetricRow,
	dedupeSeen map[string]struct{},
	row NamespaceMetricRow,
	orgID, clusterUUID string,
) {
	key := namespaceUsageDedupeKey(row)
	if _, ok := dedupeSeen[key]; ok {
		return
	}
	dedupeSeen[key] = struct{}{}

	bucketDate := time.Date(
		row.IntervalStart.Year(), row.IntervalStart.Month(), row.IntervalStart.Day(),
		0, 0, 0, 0, time.UTC,
	)
	digestKey := NamespaceDigestKey{
		OrgID:        orgID,
		ClusterUUID:  clusterUUID,
		Namespace:    row.Namespace,
		BucketDate:   bucketDate,
		ScheduleType: ScheduleTypeAllHours,
	}
	groups[digestKey] = append(groups[digestKey], row)
}

func appendNamespaceBusinessHoursRow(
	groups map[NamespaceDigestKey][]NamespaceMetricRow,
	row NamespaceMetricRow,
	orgID, clusterUUID string,
	cache *bhschedule.Cache,
) {
	if cache == nil {
		return
	}
	sched := cache.Resolve(row.Namespace)
	if !sched.Enabled {
		return
	}
	weightFn := namespaceBusinessHoursRowWeightFn(sched)
	if weightFn != nil {
		if w := weightFn(row); w <= 0 {
			return
		}
	}
	bucketDate := time.Date(
		row.IntervalStart.Year(), row.IntervalStart.Month(), row.IntervalStart.Day(),
		0, 0, 0, 0, time.UTC,
	)
	digestKey := NamespaceDigestKey{
		OrgID:        orgID,
		ClusterUUID:  clusterUUID,
		Namespace:    row.Namespace,
		BucketDate:   bucketDate,
		ScheduleType: ScheduleTypeBusinessHours,
	}
	groups[digestKey] = append(groups[digestKey], row)
}

func ensureNamespaceDigestPartitionsForKeys(ctx context.Context, pool *pgxpool.Pool, grouped map[NamespaceDigestKey][]NamespaceMetricRow) {
	keys := make([]NamespaceDigestKey, 0, len(grouped))
	for k := range grouped {
		keys = append(keys, k)
	}
	EnsureNamespaceDigestPartitions(ctx, pool, keys)
}

func ensureNamespaceSamplePartitionMonth(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("namespace_usage_samples_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF namespace_usage_samples FOR VALUES FROM ('%s') TO ('%s')`,
		partName,
		monthStart.Format("2006-01-02"),
		monthEnd.Format("2006-01-02"),
	)
	if _, err := pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("EnsureNamespaceSamplePartitionMonth %s: %w", partName, err)
	}
	return nil
}

func flushNamespaceDigestGroupBatch(
	ctx context.Context,
	pool *pgxpool.Pool,
	groupedAll, groupedBH map[NamespaceDigestKey][]NamespaceMetricRow,
	scheduleCache *bhschedule.Cache,
	orgID, clusterUUID string,
) error {
	if len(groupedAll) == 0 && len(groupedBH) == 0 {
		return nil
	}
	grouped := mergeNamespaceDigestGroups(groupedAll, groupedBH)
	start := time.Now()
	defer func() {
		metrics.ObserveIngestFlush(start)
		metrics.IncIngestFlushTotal()
	}()

	ensureNamespaceDigestPartitionsForKeys(ctx, pool, grouped)
	if err := upsertNamespaceDigests(ctx, pool, grouped, scheduleCache); err != nil {
		return err
	}

	clear(groupedAll)
	clear(groupedBH)

	logging.ForOrg(orgID, clusterUUID).Infof(
		"ProcessNamespaceCSVToDigests: flushed %d digest groups (incremental)", len(grouped))
	return nil
}

func accumulateNamespaceQuotaRow(
	aggs map[namespaceQuotaDigestKey]*namespaceQuotaDigestAgg,
	row NamespaceMetricRow,
	orgID, clusterUUID string,
) {
	if !row.hasQuotaHardOrUsed() {
		return
	}
	reportDate := time.Date(
		row.IntervalEnd.Year(), row.IntervalEnd.Month(), row.IntervalEnd.Day(),
		0, 0, 0, 0, time.UTC,
	)
	key := namespaceQuotaDigestKey{
		orgID:       orgID,
		clusterUUID: clusterUUID,
		namespace:   row.Namespace,
		quotaName:   row.QuotaName,
		reportDate:  reportDate,
	}
	agg, ok := aggs[key]
	if !ok {
		agg = &namespaceQuotaDigestAgg{key: key}
		aggs[key] = agg
	}
	agg.cpuRequestHard = maxInt64NS(agg.cpuRequestHard, row.CPURequestHardMC)
	agg.cpuRequestUsed = maxInt64NS(agg.cpuRequestUsed, row.CPURequestUsedMC)
	agg.cpuLimitHard = maxInt64NS(agg.cpuLimitHard, row.CPULimitHardMC)
	agg.cpuLimitUsed = maxInt64NS(agg.cpuLimitUsed, row.CPULimitUsedMC)
	agg.memoryRequestHard = maxInt64NS(agg.memoryRequestHard, row.MemoryRequestHardBytes)
	agg.memoryRequestUsed = maxInt64NS(agg.memoryRequestUsed, row.MemoryRequestUsedBytes)
	agg.memoryLimitHard = maxInt64NS(agg.memoryLimitHard, row.MemoryLimitHardBytes)
	agg.memoryLimitUsed = maxInt64NS(agg.memoryLimitUsed, row.MemoryLimitUsedBytes)
	agg.storageRequestHard = maxInt64NS(agg.storageRequestHard, row.StorageRequestHardBytes)
	agg.storageRequestUsed = maxInt64NS(agg.storageRequestUsed, row.StorageRequestUsedBytes)
	agg.podsHard = maxInt64NS(agg.podsHard, row.PodsHard)
	agg.podsUsed = maxInt64NS(agg.podsUsed, row.PodsUsed)
	agg.objectCountHard = maxInt64NS(agg.objectCountHard, row.ObjectCountHard)
	agg.objectCountUsed = maxInt64NS(agg.objectCountUsed, row.ObjectCountUsed)
}

func upsertNamespaceQuotaDigestsFromAggs(ctx context.Context, pool *pgxpool.Pool, aggs map[namespaceQuotaDigestKey]*namespaceQuotaDigestAgg) error {
	for _, agg := range aggs {
		if err := upsertNamespaceQuotaDigest(ctx, pool, agg); err != nil {
			return err
		}
	}
	return nil
}

func parseAndDigestNamespaceCSVStream(
	ctx context.Context,
	pool *pgxpool.Pool,
	r io.Reader,
	orgID, clusterUUID string,
) (int, error) {
	groupedAll := make(map[NamespaceDigestKey][]NamespaceMetricRow, 256)
	groupedBH := make(map[NamespaceDigestKey][]NamespaceMetricRow)
	quotaAggs := make(map[namespaceQuotaDigestKey]*namespaceQuotaDigestAgg)
	dedupeSeen := make(map[string]struct{})
	sampleBatch := make([]NamespaceMetricRow, 0, streamSampleFlushRows)
	digestBatchesFlushed := 0
	flushBatchSize := ingestFlushBatchSize()

	var scheduleCache *bhschedule.Cache
	if BusinessHoursAggregationEnabled() {
		var loadErr error
		scheduleCache, loadErr = bhschedule.LoadSchedules(ctx, pool, orgID, clusterUUID)
		if loadErr != nil {
			return 0, fmt.Errorf("load business hours schedules: %w", loadErr)
		}
		if scheduleCache != nil && !scheduleCache.ProducesBusinessHoursDigests() {
			if err := pruneBusinessHoursDigests(ctx, pool, orgID, clusterUUID); err != nil {
				return 0, err
			}
		}
	}

	flushSamples := func(batch []NamespaceMetricRow) error {
		if len(batch) == 0 {
			return nil
		}
		for _, row := range batch {
			monthStart := time.Date(row.IntervalStart.Year(), row.IntervalStart.Month(), 1, 0, 0, 0, 0, time.UTC)
			if err := ensureNamespaceSamplePartitionMonth(ctx, pool, monthStart); err != nil {
				return fmt.Errorf("namespace sample partitions: %w", err)
			}
		}
		return upsertNamespaceUsageSamples(ctx, pool, batch, orgID, clusterUUID)
	}

	startTime := time.Now()

	rowCount, err := forEachNamespaceCSVRow(r, func(row NamespaceMetricRow) error {
		sampleBatch = append(sampleBatch, row)
		if len(sampleBatch) >= streamSampleFlushRows {
			if err := flushSamples(sampleBatch); err != nil {
				return fmt.Errorf("upsert namespace usage samples: %w", err)
			}
			sampleBatch = sampleBatch[:0]
		}

		appendNamespaceUsageDigestRow(groupedAll, dedupeSeen, row, orgID, clusterUUID)
		appendNamespaceBusinessHoursRow(groupedBH, row, orgID, clusterUUID, scheduleCache)
		accumulateNamespaceQuotaRow(quotaAggs, row, orgID, clusterUUID)

		if namespaceDigestGroupCount(groupedAll, groupedBH) >= flushBatchSize {
			if err := flushNamespaceDigestGroupBatch(ctx, pool, groupedAll, groupedBH, scheduleCache, orgID, clusterUUID); err != nil {
				return fmt.Errorf("incremental namespace digest flush: %w", err)
			}
			digestBatchesFlushed++
		}
		return nil
	})
	if err != nil {
		return rowCount, err
	}
	if rowCount == 0 {
		logging.ForOrg(orgID, clusterUUID).Info("ProcessNamespaceCSVToDigests: no rows parsed")
		return 0, nil
	}

	if err := flushSamples(sampleBatch); err != nil {
		return rowCount, err
	}

	grouped := mergeNamespaceDigestGroups(groupedAll, groupedBH)
	streamElapsed := time.Since(startTime).Round(time.Millisecond)
	logging.ForOrg(orgID, clusterUUID).WithFields(map[string]interface{}{
		"stream_elapsed": streamElapsed,
	}).Infof(
		"ProcessNamespaceCSVToDigests: %d rows -> %d digest groups at EOF (incremental flushes: %d)",
		rowCount, len(grouped), digestBatchesFlushed)

	upsertStart := time.Now()

	if len(grouped) > 0 {
		ensureNamespaceDigestPartitionsForKeys(ctx, pool, grouped)
		if err := upsertNamespaceDigests(ctx, pool, grouped, scheduleCache); err != nil {
			return rowCount, err
		}
	}

	if len(quotaAggs) > 0 {
		if err := upsertNamespaceQuotaDigestsFromAggs(ctx, pool, quotaAggs); err != nil {
			return rowCount, fmt.Errorf("upsert namespace quota digests: %w", err)
		}
	}

	logging.ForOrg(orgID, clusterUUID).WithFields(map[string]interface{}{
		"upsert_elapsed": time.Since(upsertStart).Round(time.Millisecond),
		"total_elapsed":  time.Since(startTime).Round(time.Millisecond),
	}).Infof("ProcessNamespaceCSVToDigests: upserted %d digests", len(grouped))
	return rowCount, nil
}
