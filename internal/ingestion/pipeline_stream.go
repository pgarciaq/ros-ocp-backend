package ingestion

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgdigest"
)

const (
	defaultGroupedAllCapacity = 4096
	defaultGroupedBHCapacity  = 1024
	defaultNodeAccumCapacity  = 256
)

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
		if err := EnsureDigestPartitionMonth(ctx, pool, monthStart); err != nil {
			logging.GetLogger().Warnf("EnsureIngestPartitionsAtStartup digest %s: %v", monthStart.Format("200601"), err)
		}
		months := map[time.Time]struct{}{monthStart: {}}
		ensureGPUDigestPartitionsForMonths(ctx, pool, months)
		ensureNodeDigestPartitionsForMonths(ctx, pool, months)
	}
}

// EnsureIngestPartitionsForWindow pre-creates digest, GPU, and node partitions
// for a 3-month window (previous, current, next month). Called once before
// manifest file processing to avoid redundant CREATE TABLE IF NOT EXISTS
// checks during hot-path CSV parsing.
func EnsureIngestPartitionsForWindow(ctx context.Context, pool *pgxpool.Pool) {
	now := time.Now().UTC()
	for i := -1; i <= 1; i++ {
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, i, 0)
		if err := EnsureDigestPartitionMonth(ctx, pool, monthStart); err != nil {
			logging.GetLogger().Warnf("EnsureIngestPartitionsForWindow digest %s: %v", monthStart.Format("200601"), err)
		}
		months := map[time.Time]struct{}{monthStart: {}}
		ensureGPUDigestPartitionsForMonths(ctx, pool, months)
		ensureNodeDigestPartitionsForMonths(ctx, pool, months)
	}
}

// EnsureDigestPartitionMonth creates a daily_container_digests partition for one month.
func EnsureDigestPartitionMonth(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	return pgdigest.EnsurePartitionMonth(ctx, pool, monthStart)
}

func ensureDigestPartitionsForKeys(ctx context.Context, pool *pgxpool.Pool, grouped map[DigestKey][]metricSample) error {
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
	groups map[DigestKey][]metricSample,
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
	groups[key] = append(groups[key], metricSampleFromRow(row))
}

func appendBusinessHoursRow(
	groups map[DigestKey][]metricSample,
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

func digestGroupCount(all, bh map[DigestKey][]metricSample) int {
	return len(all) + len(bh)
}

func ingestFlushBatchSize() int {
	size := config.GetConfig().IngestFlushBatchSize
	if size <= 0 {
		return math.MaxInt32
	}
	return size
}

func flushDigestGroupBatch(
	ctx context.Context,
	pool *pgxpool.Pool,
	groupedAll, groupedBH map[DigestKey][]metricSample,
	scheduleCache *bhschedule.Cache,
	orgID, clusterUUID string,
) error {
	if len(groupedAll) == 0 && len(groupedBH) == 0 {
		return nil
	}
	grouped := mergeDigestGroups(groupedAll, groupedBH)
	start := time.Now()
	defer func() {
		metrics.ObservePipelinePhase(metrics.PhaseWriteDigests, start)
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
	groupedAll := make(map[DigestKey][]metricSample, defaultGroupedAllCapacity)
	groupedBH := make(map[DigestKey][]metricSample, defaultGroupedBHCapacity)
	digestBatchesFlushed := 0
	flushBatchSize := ingestFlushBatchSize()
	var gpuAccum *gpuStreamAccumulator
	var nodeAccum map[NodeDayKey]*NodeDayAccumulator
	if opts.EnableGPU {
		gpuAccum = newGPUStreamAccumulator()
	}
	if opts.EnableNode {
		nodeAccum = make(map[NodeDayKey]*NodeDayAccumulator, defaultNodeAccumCapacity)
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

	startTime := time.Now()

	rowCount, err := forEachCSVRow(ctx, r, func(row MetricRow) error {
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

	grouped := mergeDigestGroups(groupedAll, groupedBH)
	useSingleIngestTx := singleIngestTxEligible(rowCount, len(grouped), digestBatchesFlushed)
	metrics.SetIngestGroupsInMemory(len(grouped))
	streamElapsed := time.Since(startTime).Round(time.Millisecond)
	logging.ForOrg(orgID, clusterUUID).WithFields(map[string]interface{}{
		"stream_elapsed": streamElapsed,
	}).Infof("ProcessCSVToDigests: %d rows -> %d digest groups at EOF (incremental flushes: %d)",
		rowCount, len(grouped), digestBatchesFlushed)

	upsertStart := time.Now()

	if err := ensureDigestPartitionsForKeys(ctx, pool, grouped); err != nil {
		return rowCount, fmt.Errorf("digest partitions: %w", err)
	}

	if useSingleIngestTx {
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
		if err := commitIngestInSingleTx(ctx, pool, grouped, gpuAccum, nodeAccum, scheduleCache, orgID, clusterUUID); err != nil {
			return rowCount, err
		}
		logging.ForOrg(orgID, clusterUUID).WithFields(map[string]interface{}{
			"upsert_elapsed": time.Since(upsertStart).Round(time.Millisecond),
			"total_elapsed":  time.Since(startTime).Round(time.Millisecond),
		}).Infof("ProcessCSVToDigests: upserted %d digests", len(grouped))
	} else {
		if err := upsertContainerDigests(ctx, pool, grouped, scheduleCache); err != nil {
			return rowCount, err
		}
		logging.ForOrg(orgID, clusterUUID).WithFields(map[string]interface{}{
			"upsert_elapsed": time.Since(upsertStart).Round(time.Millisecond),
			"total_elapsed":  time.Since(startTime).Round(time.Millisecond),
		}).Infof("ProcessCSVToDigests: upserted %d digests", len(grouped))

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
	}

	if nodeAccum != nil && len(nodeAccum) > 0 && config.HourlyNodeDigestsEnabled() {
		hourlyMap := BuildHourlyNodeDigests(nodeAccum)
		if err := UpsertHourlyNodeDigests(ctx, pool, orgID, clusterUUID, hourlyMap); err != nil {
			return rowCount, fmt.Errorf("hourly node digest upsert: %w", err)
		}
		logging.ForOrg(orgID, clusterUUID).Infof(
			"ProcessCSVToDigests: upserted %d hourly node digests", len(hourlyMap))
	}

	return rowCount, nil
}
