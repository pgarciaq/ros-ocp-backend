package ingestion

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
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

var ensuredSampleMonths sync.Map
var ensuredDigestMonths sync.Map

// EnsureSamplePartitionMonth creates a container_usage_samples partition for one month.
func EnsureSamplePartitionMonth(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	key := monthStart.Format("200601")
	if _, loaded := ensuredSampleMonths.LoadOrStore(key, struct{}{}); loaded {
		return nil
	}
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("container_usage_samples_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF container_usage_samples FOR VALUES FROM ('%s') TO ('%s')`,
		partName,
		monthStart.Format("2006-01-02"),
		monthEnd.Format("2006-01-02"),
	)
	if _, err := pool.Exec(ctx, sql); err != nil {
		ensuredSampleMonths.Delete(key)
		return fmt.Errorf("EnsureSamplePartitionMonth %s: %w", partName, err)
	}
	return nil
}

// EnsureDigestPartitionMonth creates a daily_container_digests partition for one month.
func EnsureDigestPartitionMonth(ctx context.Context, pool *pgxpool.Pool, monthStart time.Time) error {
	key := monthStart.Format("200601")
	if _, loaded := ensuredDigestMonths.LoadOrStore(key, struct{}{}); loaded {
		return nil
	}
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("daily_container_digests_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_container_digests FOR VALUES FROM ('%s') TO ('%s')`,
		partName,
		monthStart.Format("2006-01-02"),
		monthEnd.Format("2006-01-02"),
	)
	if _, err := pool.Exec(ctx, sql); err != nil {
		ensuredDigestMonths.Delete(key)
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

	if err := flushSamples(sampleBatch); err != nil {
		return rowCount, err
	}

	grouped := mergeDigestGroups(groupedAll, groupedBH)
	logging.ForOrg(orgID, clusterUUID).Infof("ProcessCSVToDigests: %d rows -> %d all_hours groups, %d business_hours groups",
		rowCount, len(groupedAll), len(groupedBH))

	if err := ensureDigestPartitionsForKeys(ctx, pool, grouped); err != nil {
		return rowCount, fmt.Errorf("digest partitions: %w", err)
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
