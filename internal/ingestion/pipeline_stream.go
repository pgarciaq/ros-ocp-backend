package ingestion

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

const streamSampleFlushRows = 1000

// ParseDigestOptions configures optional GPU/node side effects during streaming ingest.
type ParseDigestOptions struct {
	EnableGPU  bool
	EnableNode bool
}

// EnsureIngestPartitionsAtStartup pre-creates monthly partitions for current and next month.
func EnsureIngestPartitionsAtStartup(ctx context.Context, pool *pgxpool.Pool) {
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, i, 0)
		if err := EnsureSamplePartitionMonth(ctx, pool, monthStart); err != nil {
			logging.GetLogger().Warnf("EnsureIngestPartitionsAtStartup sample %s: %v", monthStart.Format("200601"), err)
		}
		if err := EnsureDigestPartitionMonth(ctx, pool, monthStart); err != nil {
			logging.GetLogger().Warnf("EnsureIngestPartitionsAtStartup digest %s: %v", monthStart.Format("200601"), err)
		}
		months := map[time.Time]struct{}{monthStart: {}}
		ensureGPUDigestPartitionsForMonths(ctx, pool, months)
		ensureNodeDigestPartitionsForMonths(ctx, pool, months)
	}
}

// EnsureSamplePartitionMonth creates a container_usage_samples partition for one month.
func EnsureSamplePartitionMonth(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("container_usage_samples_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF container_usage_samples FOR VALUES FROM ('%s') TO ('%s')`,
		partName,
		monthStart.Format("2006-01-02"),
		monthEnd.Format("2006-01-02"),
	)
	if _, err := pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("EnsureSamplePartitionMonth %s: %w", partName, err)
	}
	return nil
}

// EnsureDigestPartitionMonth creates a daily_container_digests partition for one month.
func EnsureDigestPartitionMonth(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("daily_container_digests_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_container_digests FOR VALUES FROM ('%s') TO ('%s')`,
		partName,
		monthStart.Format("2006-01-02"),
		monthEnd.Format("2006-01-02"),
	)
	if _, err := pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("EnsureDigestPartitionMonth %s: %w", partName, err)
	}
	return nil
}

func ensureDigestPartitionsForKeys(ctx context.Context, pool *pgxpool.Pool, grouped map[DigestKey][]MetricRow) error {
	months := map[time.Time]struct{}{}
	for k := range grouped {
		monthStart := time.Date(k.BucketDate.Year(), k.BucketDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[monthStart] = struct{}{}
	}
	for monthStart := range months {
		if err := EnsureDigestPartitionMonth(ctx, pool, monthStart); err != nil {
			return err
		}
	}
	return nil
}

func appendGroupedRow(
	groups map[DigestKey][]MetricRow,
	row MetricRow,
	orgID, clusterUUID string,
	scheduleType ScheduleType,
	weightFn RowWeightFunc,
) {
	if weightFn != nil {
		if w := weightFn(row); w <= 0 {
			return
		}
	}
	bucketDate := time.Date(
		row.IntervalStart.Year(), row.IntervalStart.Month(), row.IntervalStart.Day(),
		0, 0, 0, 0, time.UTC,
	)
	key := DigestKey{
		OrgID: orgID, ClusterUUID: clusterUUID,
		Namespace: row.Namespace, Workload: row.WorkloadName,
		WorkloadType: row.WorkloadType, ContainerName: row.ContainerName,
		BucketDate: bucketDate, ScheduleType: scheduleType,
	}
	groups[key] = append(groups[key], row)
}

func appendBusinessHoursRow(
	groups map[DigestKey][]MetricRow,
	row MetricRow,
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
	appendGroupedRow(groups, row, orgID, clusterUUID, ScheduleTypeBusinessHours, BusinessHoursRowWeightFn(sched))
}

func digestGroupCount(all, bh map[DigestKey][]MetricRow) int {
	return len(all) + len(bh)
}

func ingestFlushBatchSize() int {
	size := config.GetConfig().IngestFlushBatchSize
	if size <= 0 {
		return 1000
	}
	return size
}

func flushDigestGroupBatch(
	ctx context.Context,
	pool *pgxpool.Pool,
	groupedAll, groupedBH map[DigestKey][]MetricRow,
	scheduleCache *bhschedule.Cache,
	orgID, clusterUUID string,
) error {
	if len(groupedAll) == 0 && len(groupedBH) == 0 {
		return nil
	}
	grouped := mergeDigestGroups(groupedAll, groupedBH)
	start := time.Now()
	defer func() {
		metrics.ObserveIngestFlush(start)
		metrics.IncIngestFlushTotal()
	}()

	if err := ensureDigestPartitionsForKeys(ctx, pool, grouped); err != nil {
		return fmt.Errorf("digest partitions: %w", err)
	}
	if err := upsertContainerDigests(ctx, pool, grouped, scheduleCache); err != nil {
		return err
	}

	clear(groupedAll)
	clear(groupedBH)
	metrics.SetIngestGroupsInMemory(0)

	logging.ForOrg(orgID, clusterUUID).Infof(
		"ProcessCSVToDigests: flushed %d digest groups (incremental)", len(grouped))
	return nil
}

func parseAndDigestCSVStream(
	ctx context.Context,
	pool *pgxpool.Pool,
	r io.Reader,
	orgID, clusterUUID string,
	opts ParseDigestOptions,
) (int, error) {
	groupedAll := make(map[DigestKey][]MetricRow, 256)
	groupedBH := make(map[DigestKey][]MetricRow)
	sampleBatch := make([]MetricRow, 0, streamSampleFlushRows)
	var deferredSamples []MetricRow
	useSingleIngestTx := false
	digestBatchesFlushed := 0
	flushBatchSize := ingestFlushBatchSize()
	var gpuAccum *gpuStreamAccumulator
	var nodeAccum map[NodeDayKey]*NodeDayAccumulator
	if opts.EnableGPU {
		gpuAccum = newGPUStreamAccumulator()
	}
	if opts.EnableNode {
		nodeAccum = make(map[NodeDayKey]*NodeDayAccumulator)
	}

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

	flushSamples := func(batch []MetricRow) error {
		if len(batch) == 0 {
			return nil
		}
		if useSingleIngestTx {
			deferredSamples = append(deferredSamples, batch...)
			return nil
		}
		for _, row := range batch {
			monthStart := time.Date(row.IntervalStart.Year(), row.IntervalStart.Month(), 1, 0, 0, 0, 0, time.UTC)
			if err := EnsureSamplePartitionMonth(ctx, pool, monthStart); err != nil {
				return fmt.Errorf("sample partitions: %w", err)
			}
		}
		return upsertUsageSamples(ctx, pool, batch, orgID, clusterUUID)
	}

	rowCount, err := forEachCSVRow(r, func(row MetricRow) error {
		sampleBatch = append(sampleBatch, row)
		if len(sampleBatch) >= streamSampleFlushRows {
			if err := flushSamples(sampleBatch); err != nil {
				return fmt.Errorf("upsert usage samples: %w", err)
			}
			sampleBatch = sampleBatch[:0]
		}

		appendGroupedRow(groupedAll, row, orgID, clusterUUID, ScheduleTypeAllHours, nil)
		appendBusinessHoursRow(groupedBH, row, orgID, clusterUUID, scheduleCache)

		groupCount := digestGroupCount(groupedAll, groupedBH)
		if groupCount >= flushBatchSize {
			if err := flushDigestGroupBatch(ctx, pool, groupedAll, groupedBH, scheduleCache, orgID, clusterUUID); err != nil {
				return fmt.Errorf("incremental digest flush: %w", err)
			}
			digestBatchesFlushed++
		}

		if gpuAccum != nil {
			gpuAccum.add(row)
		}
		if nodeAccum != nil && row.Node != "" {
			day := time.Date(row.IntervalStart.Year(), row.IntervalStart.Month(), row.IntervalStart.Day(), 0, 0, 0, 0, time.UTC)
			key := NodeDayKey{Node: row.Node, BucketDate: day}
			acc, ok := nodeAccum[key]
			if !ok {
				acc = newNodeDayAccumulator()
				nodeAccum[key] = acc
			}
			acc.AddRow(row)
		}
		return nil
	})
	if err != nil {
		return rowCount, err
	}
	if rowCount == 0 {
		logging.ForOrg(orgID, clusterUUID).Info("ProcessCSVToDigests: no rows parsed")
		return 0, nil
	}

	useSingleIngestTx = rowCount <= ingestSingleTxRowThreshold && digestBatchesFlushed == 0

	if err := flushSamples(sampleBatch); err != nil {
		return rowCount, err
	}

	grouped := mergeDigestGroups(groupedAll, groupedBH)
	metrics.SetIngestGroupsInMemory(len(grouped))
	logging.ForOrg(orgID, clusterUUID).Infof("ProcessCSVToDigests: %d rows -> %d digest groups at EOF (incremental flushes: %d)",
		rowCount, len(grouped), digestBatchesFlushed)

	if err := ensureDigestPartitionsForKeys(ctx, pool, grouped); err != nil {
		return rowCount, fmt.Errorf("digest partitions: %w", err)
	}

	if useSingleIngestTx {
		for _, row := range deferredSamples {
			monthStart := time.Date(row.IntervalStart.Year(), row.IntervalStart.Month(), 1, 0, 0, 0, 0, time.UTC)
			if err := EnsureSamplePartitionMonth(ctx, pool, monthStart); err != nil {
				return rowCount, fmt.Errorf("sample partitions: %w", err)
			}
		}
		if gpuAccum != nil {
			months := map[time.Time]struct{}{}
			for k := range gpuAccum.groups {
				monthStart := time.Date(k.date.Year(), k.date.Month(), 1, 0, 0, 0, 0, time.UTC)
				months[monthStart] = struct{}{}
			}
			ensureGPUDigestPartitionsForMonths(ctx, pool, months)
		}
		if nodeAccum != nil && len(nodeAccum) > 0 {
			EnsureNodeDigestPartitions(ctx, pool, nodeAccum)
		}
		if err := commitIngestInSingleTx(ctx, pool, deferredSamples, grouped, gpuAccum, nodeAccum, scheduleCache, orgID, clusterUUID); err != nil {
			return rowCount, err
		}
		logging.ForOrg(orgID, clusterUUID).Infof("ProcessCSVToDigests: upserted %d digests", len(grouped))
		return rowCount, nil
	}

	if err := upsertContainerDigests(ctx, pool, grouped, scheduleCache); err != nil {
		return rowCount, err
	}
	logging.ForOrg(orgID, clusterUUID).Infof("ProcessCSVToDigests: upserted %d digests", len(grouped))

	if gpuAccum != nil {
		if err := gpuAccum.flush(ctx, pool, orgID, clusterUUID); err != nil {
			return rowCount, fmt.Errorf("GPU digest upsert: %w", err)
		}
	}
	if nodeAccum != nil && len(nodeAccum) > 0 {
		cfg := config.GetConfig()
		if err := FlushNodeDigests(ctx, pool, nodeAccum, orgID, clusterUUID, cfg.NodeAllocatableFactor); err != nil {
			return rowCount, fmt.Errorf("node digest upsert: %w", err)
		}
	}
	return rowCount, nil
}
