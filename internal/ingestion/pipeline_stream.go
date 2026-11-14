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

func parseAndDigestCSVStream(
	ctx context.Context,
	pool *pgxpool.Pool,
	r io.Reader,
	orgID, clusterUUID string,
	opts ParseDigestOptions,
) error {
	groupedAll := make(map[DigestKey][]metricSample, defaultGroupedAllCapacity)
	groupedBH := make(map[DigestKey][]metricSample, defaultGroupedBHCapacity)
	var gpuAccum *gpuStreamAccumulator
	var gpuBHAccum *gpuStreamAccumulator
	var nodeAccum map[NodeDayKey]*NodeDayAccumulator
	var nodeBHAccum map[NodeDayKey]*NodeDayAccumulator
	var nodeClusterSched bhschedule.Schedule
	writeNodeBH := false
	writeGPUBH := false
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
			return fmt.Errorf("load business hours schedules: %w", loadErr)
		}
		if scheduleCache != nil && !scheduleCache.ProducesBusinessHoursDigests() {
			if err := pruneBusinessHoursDigests(ctx, pool, orgID, clusterUUID); err != nil {
				return err
			}
		}
		if scheduleCache != nil && !scheduleCache.ProducesNodeBusinessHoursDigests() {
			if err := bhschedule.PruneClusterNodeBusinessHoursDigests(ctx, pool, orgID, clusterUUID); err != nil {
				return err
			}
		}
		if opts.EnableNode && scheduleCache != nil && scheduleCache.ProducesNodeBusinessHoursDigests() {
			nodeBHAccum = make(map[NodeDayKey]*NodeDayAccumulator, defaultNodeAccumCapacity)
			nodeClusterSched = scheduleCache.ResolveCluster()
			writeNodeBH = true
		}
		if opts.EnableGPU && scheduleCache != nil && scheduleCache.ProducesBusinessHoursDigests() {
			gpuBHAccum = newGPUStreamAccumulator()
			writeGPUBH = true
		}
	}

	startTime := time.Now()

	rowCount, err := forEachCSVRow(ctx, r, func(row MetricRow) error {
		appendGroupedRow(groupedAll, row, orgID, clusterUUID, ScheduleTypeAllHours, nil)
		appendBusinessHoursRow(groupedBH, row, orgID, clusterUUID, scheduleCache)

		// #822 (owner decision 2026-11-14, option a): no mid-stream
		// flush — the stream paths do NOT honor
		// ROS_INGEST_FLUSH_BATCH_SIZE (staged-path-only). The mid-stream
		// flush replaced complete container-day digests with fragments:
		// the threshold crossed on the row creating the Nth group, so
		// that group flushed partial and its remaining rows REPLACED it
		// at the next flush (replacement upserts). Stream-path memory is
		// bounded by the input size (ROS_CSV_MAX_BODY_BYTES caps a file
		// at 500 MiB; digest grouping compresses samples, though BH
		// dual-streaming duplicates them) — not a fixed heap bound.

		if gpuAccum != nil {
			gpuAccum.add(row)
		}
		if writeGPUBH {
			sched := scheduleCache.Resolve(row.Namespace)
			gpuBHAccum.addIfWeight(row, bhschedule.ScheduleWeight(row.IntervalStart, sched))
		}
		addNodeMetricRow(nodeAccum, row, 1)
		if writeNodeBH {
			addNodeMetricRow(nodeBHAccum, row, bhschedule.ScheduleWeight(row.IntervalStart, nodeClusterSched))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if rowCount == 0 {
		logging.ForOrg(orgID, clusterUUID).Info("ProcessCSVToDigests: no rows parsed")
		return nil
	}

	grouped := mergeDigestGroups(groupedAll, groupedBH)
	useSingleIngestTx := singleIngestTxEligible(rowCount, len(grouped), 0)
	metrics.SetIngestGroupsInMemory(len(grouped))
	streamElapsed := time.Since(startTime).Round(time.Millisecond)
	logging.ForOrg(orgID, clusterUUID).WithFields(map[string]interface{}{
		"stream_elapsed": streamElapsed,
	}).Infof("ProcessCSVToDigests: %d rows -> %d digest groups at EOF", rowCount, len(grouped))

	upsertStart := time.Now()
	// #822: the ingest-flush metrics now ride the single EOF batch write
	// (the successor of the removed mid-stream flush measurement). The
	// section includes the GPU/node writes on the multi-write path (the
	// single-tx path commits them together by design) — a deliberate
	// sub-phase overlapping the enclosing parse timer. Success-gated: a
	// failed write does not count as a flush.
	eofWriteOK := false
	defer func() {
		if eofWriteOK {
			metrics.ObservePipelinePhase(metrics.PhaseWriteDigests, upsertStart)
			metrics.ObserveIngestFlush(upsertStart)
			metrics.IncIngestFlushTotal()
		}
	}()

	if err := ensureDigestPartitionsForKeys(ctx, pool, grouped); err != nil {
		return fmt.Errorf("digest partitions: %w", err)
	}

	if useSingleIngestTx {
		if gpuAccum != nil {
			months := map[time.Time]struct{}{}
			for k := range gpuAccum.groups {
				monthStart := time.Date(k.date.Year(), k.date.Month(), 1, 0, 0, 0, 0, time.UTC)
				months[monthStart] = struct{}{}
			}
			if gpuBHAccum != nil {
				for k := range gpuBHAccum.groups {
					monthStart := time.Date(k.date.Year(), k.date.Month(), 1, 0, 0, 0, 0, time.UTC)
					months[monthStart] = struct{}{}
				}
			}
			ensureGPUDigestPartitionsForMonths(ctx, pool, months)
		}
		if len(nodeAccum) > 0 {
			EnsureNodeDigestPartitions(ctx, pool, nodeAccum)
		}
		if len(nodeBHAccum) > 0 {
			EnsureNodeDigestPartitions(ctx, pool, nodeBHAccum)
		}
		if err := commitIngestInSingleTx(ctx, pool, grouped, gpuAccum, gpuBHAccum, nodeAccum, nodeBHAccum, scheduleCache, orgID, clusterUUID); err != nil {
			return err
		}
		logging.ForOrg(orgID, clusterUUID).WithFields(map[string]interface{}{
			"upsert_elapsed": time.Since(upsertStart).Round(time.Millisecond),
			"total_elapsed":  time.Since(startTime).Round(time.Millisecond),
		}).Infof("ProcessCSVToDigests: upserted %d digests", len(grouped))
	} else {
		if err := upsertContainerDigests(ctx, pool, grouped, scheduleCache); err != nil {
			return err
		}
		logging.ForOrg(orgID, clusterUUID).WithFields(map[string]interface{}{
			"upsert_elapsed": time.Since(upsertStart).Round(time.Millisecond),
			"total_elapsed":  time.Since(startTime).Round(time.Millisecond),
		}).Infof("ProcessCSVToDigests: upserted %d digests", len(grouped))

		if gpuAccum != nil {
			if err := gpuAccum.flush(ctx, pool, orgID, clusterUUID); err != nil {
				return fmt.Errorf("GPU digest upsert: %w", err)
			}
		}
		if gpuBHAccum != nil {
			if err := gpuBHAccum.flushWithSchedule(ctx, pool, orgID, clusterUUID, ScheduleTypeBusinessHours); err != nil {
				return fmt.Errorf("GPU business_hours digest upsert: %w", err)
			}
		}
		if len(nodeAccum) > 0 {
			cfg := config.GetConfig()
			if err := FlushNodeDigests(ctx, pool, nodeAccum, orgID, clusterUUID, cfg.NodeAllocatableFactor); err != nil {
				return fmt.Errorf("node digest upsert: %w", err)
			}
		}
		if len(nodeBHAccum) > 0 {
			cfg := config.GetConfig()
			if err := FlushNodeDigestsWithSchedule(ctx, pool, nodeBHAccum, orgID, clusterUUID, cfg.NodeAllocatableFactor, ScheduleTypeBusinessHours); err != nil {
				return fmt.Errorf("node business_hours digest upsert: %w", err)
			}
		}
	}

	if len(nodeAccum) > 0 && config.HourlyNodeDigestsEnabled() {
		hourlyMap := BuildHourlyNodeDigests(nodeAccum)
		if err := UpsertHourlyNodeDigests(ctx, pool, orgID, clusterUUID, hourlyMap); err != nil {
			return fmt.Errorf("hourly node digest upsert: %w", err)
		}
		logging.ForOrg(orgID, clusterUUID).Infof(
			"ProcessCSVToDigests: upserted %d hourly node digests", len(hourlyMap))
	}

	eofWriteOK = true
	return nil
}
