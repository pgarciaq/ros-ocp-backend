package ingestion

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// ingestSingleTxRowThreshold: above this row count the ingest path uses separate transactions per phase.
const ingestSingleTxRowThreshold = 25000

// ingestSingleTxGroupThreshold: above this digest group count the ingest path uses separate transactions per phase.
const ingestSingleTxGroupThreshold = 5000

// singleIngestTxEligible reports whether EOF commit can use the single-transaction fast path.
func singleIngestTxEligible(rowCount, groupCount, digestBatchesFlushed int) bool {
	return rowCount <= ingestSingleTxRowThreshold &&
		groupCount <= ingestSingleTxGroupThreshold &&
		digestBatchesFlushed == 0
}

// pgxBatchSender matches *pgxpool.Pool and pgx.Tx for SendBatch (chunked batches must run on one tx).
type pgxBatchSender interface {
	SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
}

func flushQueuedBatch(ctx context.Context, sender pgxBatchSender, batch *pgx.Batch, queued int) error {
	if queued == 0 {
		return nil
	}
	br := sender.SendBatch(ctx, batch)
	defer br.Close()
	for range queued {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDigestPartitions creates monthly partitions of daily_container_digests
// for every month that appears in the grouped data. The migration only creates
// partitions for the current + next 2 months, so historical data (e.g. from
// the prior month) will fail with "no partition" unless we create it first.
// This is idempotent — IF NOT EXISTS prevents errors on re-runs.
func EnsureDigestPartitions(ctx context.Context, pool *pgxpool.Pool, keys []DigestKey) error {
	months := map[time.Time]struct{}{}
	for _, k := range keys {
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

// ParseAndDigestCSV parses container CSV in a single streaming pass, groups by
// container-day, upserts usage samples (flushed every 1000 rows) and digests.
func ParseAndDigestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string, opts ...ParseDigestOptions) ([]MetricRow, error) {
	var o ParseDigestOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	_, err := parseAndDigestCSVStream(ctx, pool, r, orgID, clusterUUID, o)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// ProcessCSVToDigests parses container CSV and upserts container digests, then always runs GPU and node
// digest upserts. Used by CLI/tools and tests; the Kafka native path uses services.processContainerDigestFallback
// instead so ROS_ENABLED_PLUGINS can disable GPU/node upserts when the container ingestor falls back.
func ProcessCSVToDigests(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	_, err := ParseAndDigestCSV(ctx, pool, r, orgID, clusterUUID, ParseDigestOptions{
		EnableGPU:  true,
		EnableNode: true,
	})
	return err
}

// UpsertNodeDigests aggregates container rows by node and day, then writes
// daily_node_digests. Rows without a node field are silently skipped.
func UpsertNodeDigests(ctx context.Context, pool *pgxpool.Pool, rows []MetricRow, orgID, clusterUUID string) error {
	accumulators := AggregateNodeDigests(rows)
	if len(accumulators) == 0 {
		return nil
	}
	cfg := config.GetConfig()
	return FlushNodeDigests(ctx, pool, accumulators, orgID, clusterUUID, cfg.NodeAllocatableFactor)
}

// EnsureGPUDigestPartitions creates monthly partitions of gpu_container_digests.
func EnsureGPUDigestPartitions(ctx context.Context, pool *pgxpool.Pool, months map[time.Time]struct{}) {
	for monthStart := range months {
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("gpu_container_digests_%s", monthStart.Format("200601"))
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF gpu_container_digests FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			logging.GetLogger().Warnf("EnsureGPUDigestPartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}

// UpsertGPUDigests extracts GPU rows from parsed CSV and writes daily aggregates
// to gpu_container_digests. Dual-writes business_hours when a namespace schedule
// applies (ProducesBusinessHoursDigests). Weight <= 0 drops the sample; otherwise
// the full sample is included (no fractional min/max/mean).
func UpsertGPUDigests(ctx context.Context, pool *pgxpool.Pool, rows []MetricRow, orgID, clusterUUID string) error {
	accum := newGPUStreamAccumulator()
	bhAccum := newGPUStreamAccumulator()
	var cache *bhschedule.Cache
	if BusinessHoursAggregationEnabled() {
		var loadErr error
		cache, loadErr = bhschedule.LoadSchedules(ctx, pool, orgID, clusterUUID)
		if loadErr != nil {
			return fmt.Errorf("load business hours schedules: %w", loadErr)
		}
		if cache != nil && !cache.ProducesBusinessHoursDigests() {
			if err := pruneBusinessHoursDigests(ctx, pool, orgID, clusterUUID); err != nil {
				return err
			}
		}
	}
	writeBH := cache != nil && cache.ProducesBusinessHoursDigests()
	for _, r := range rows {
		accum.add(r)
		if writeBH {
			sched := cache.Resolve(r.Namespace)
			bhAccum.addIfWeight(r, bhschedule.ScheduleWeight(r.IntervalStart, sched))
		}
	}
	if err := accum.flushWithSchedule(ctx, pool, orgID, clusterUUID, ScheduleTypeAllHours); err != nil {
		return err
	}
	if writeBH {
		if err := bhAccum.flushWithSchedule(ctx, pool, orgID, clusterUUID, ScheduleTypeBusinessHours); err != nil {
			return err
		}
	}
	return nil
}

func safeMeanInt32(sum int64, count int) int32 {
	if count <= 0 {
		return 0
	}
	return int32((sum + int64(count)/2) / int64(count))
}
