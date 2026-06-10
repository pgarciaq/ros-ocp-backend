package ingestion

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/fixedpoint"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

// maxPgxBatchQueue caps pgx.Batch queue depth to avoid unbounded RAM on large clusters.
const maxPgxBatchQueue = 500

// ingestSingleTxRowThreshold above this row count uses separate transactions per phase.
const ingestSingleTxRowThreshold = 50000

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

// EnsureSamplePartitions creates monthly partitions of container_usage_samples
// for every month that appears in the ingested data. Idempotent via IF NOT EXISTS.
func EnsureSamplePartitions(ctx context.Context, pool *pgxpool.Pool, rows []MetricRow) error {
	months := map[time.Time]struct{}{}
	for _, r := range rows {
		monthStart := time.Date(r.IntervalStart.Year(), r.IntervalStart.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[monthStart] = struct{}{}
	}
	for monthStart := range months {
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("container_usage_samples_%s", monthStart.Format("200601"))
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF container_usage_samples FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("EnsureSamplePartitions %s: %w", partName, err)
		}
	}
	return nil
}

// upsertUsageSamples batch-upserts raw CSV rows into container_usage_samples.
func upsertUsageSamples(ctx context.Context, pool *pgxpool.Pool, rows []MetricRow, orgID, clusterUUID string) error {
	if len(rows) == 0 {
		return nil
	}

	t0 := time.Now()
	defer func() { metrics.ObserveDB("upsert_usage_samples", t0) }()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for usage samples: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := db.SetLocalIngestStatementTimeout(ctx, tx); err != nil {
		return fmt.Errorf("set ingest statement timeout: %w", err)
	}

	if err := upsertUsageSamplesOnSender(ctx, tx, rows, orgID, clusterUUID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit usage samples tx: %w", err)
	}
	return nil
}

func upsertUsageSamplesOnSender(ctx context.Context, sender pgxBatchSender, rows []MetricRow, orgID, clusterUUID string) error {
	for start := 0; start < len(rows); start += maxPgxBatchQueue {
		end := start + maxPgxBatchQueue
		if end > len(rows) {
			end = len(rows)
		}
		batch := &pgx.Batch{}
		for _, r := range rows[start:end] {
			batch.Queue(`
			INSERT INTO container_usage_samples (
				sample_time, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
				cpu_usage_mc, mem_usage_kib
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, sample_time)
			DO UPDATE SET
				cpu_usage_mc = EXCLUDED.cpu_usage_mc,
				mem_usage_kib = EXCLUDED.mem_usage_kib`,
				r.IntervalStart, orgID, clusterUUID,
				r.Namespace, r.WorkloadName, r.WorkloadType, r.ContainerName,
				r.CPUUsageMC, r.MemUsageKiB,
			)
		}
		if err := flushQueuedBatch(ctx, sender, batch, end-start); err != nil {
			return fmt.Errorf("upsert usage sample: %w", err)
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
		monthEnd := monthStart.AddDate(0, 1, 0)
		partName := fmt.Sprintf("daily_container_digests_%s", monthStart.Format("200601"))
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_container_digests FOR VALUES FROM ('%s') TO ('%s')`,
			partName,
			monthStart.Format("2006-01-02"),
			monthEnd.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("EnsureDigestPartitions %s: %w", partName, err)
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
// to the gpu_container_digests table. Rows without GPU data (HasGPU()==false)
// are silently skipped.
func UpsertGPUDigests(ctx context.Context, pool *pgxpool.Pool, rows []MetricRow, orgID, clusterUUID string) error {
	type gpuKey struct {
		date      time.Time
		namespace string
		workload  string
		container string
	}
	type gpuAgg struct {
		workloadType string
		modelName    string
		profileName  string
		nodeName     string // last-seen node for this container-day
		count        int
		fbMinVal     int32
		fbMaxVal     int32
		fbAvgSum     int64
		tensorMinVal int32
		tensorMaxVal int32
		tensorAvgSum int64
		dramMinVal   int32
		dramMaxVal   int32
		dramAvgSum   int64
		smMinVal     int32
		smMaxVal     int32
		smAvgSum     int64
	}

	groups := make(map[gpuKey]*gpuAgg, 64)
	for _, r := range rows {
		if !r.HasGPU() {
			continue
		}
		day := time.Date(r.IntervalStart.Year(), r.IntervalStart.Month(), r.IntervalStart.Day(), 0, 0, 0, 0, time.UTC)
		k := gpuKey{date: day, namespace: r.Namespace, workload: r.WorkloadName, container: r.ContainerName}
		fbMin := int32(math.Round(r.AcceleratorFBUsageMin))
		fbMax := int32(math.Round(r.AcceleratorFBUsageMax))
		fbAvg := int32(math.Round(r.AcceleratorFBUsageAvg))
		tensorMin := fixedpoint.FloatToBasisPoints(r.TensorPipeActiveMin)
		tensorMax := fixedpoint.FloatToBasisPoints(r.TensorPipeActiveMax)
		tensorAvg := fixedpoint.FloatToBasisPoints(r.TensorPipeActiveAvg)
		dramMin := fixedpoint.FloatToBasisPoints(r.DRAMActiveMin)
		dramMax := fixedpoint.FloatToBasisPoints(r.DRAMActiveMax)
		dramAvg := fixedpoint.FloatToBasisPoints(r.DRAMActiveAvg)
		smMin := fixedpoint.FloatToBasisPoints(r.SMActiveMin)
		smMax := fixedpoint.FloatToBasisPoints(r.SMActiveMax)
		smAvg := fixedpoint.FloatToBasisPoints(r.SMActiveAvg)

		g, ok := groups[k]
		if !ok {
			g = &gpuAgg{
				workloadType: r.WorkloadType,
				modelName:    r.AcceleratorModelName,
				profileName:  r.AcceleratorProfileName,
				fbMinVal:     fbMin,
				fbMaxVal:     fbMax,
				tensorMinVal: tensorMin,
				tensorMaxVal: tensorMax,
				dramMinVal:   dramMin,
				dramMaxVal:   dramMax,
				smMinVal:     smMin,
				smMaxVal:     smMax,
			}
			groups[k] = g
		} else {
			if fbMin < g.fbMinVal {
				g.fbMinVal = fbMin
			}
			if fbMax > g.fbMaxVal {
				g.fbMaxVal = fbMax
			}
			if tensorMin < g.tensorMinVal {
				g.tensorMinVal = tensorMin
			}
			if tensorMax > g.tensorMaxVal {
				g.tensorMaxVal = tensorMax
			}
			if dramMin < g.dramMinVal {
				g.dramMinVal = dramMin
			}
			if dramMax > g.dramMaxVal {
				g.dramMaxVal = dramMax
			}
			if smMin < g.smMinVal {
				g.smMinVal = smMin
			}
			if smMax > g.smMaxVal {
				g.smMaxVal = smMax
			}
		}
		if r.Node != "" {
			g.nodeName = r.Node
		}
		g.count++
		g.fbAvgSum += int64(fbAvg)
		g.tensorAvgSum += int64(tensorAvg)
		g.dramAvgSum += int64(dramAvg)
		g.smAvgSum += int64(smAvg)
	}

	if len(groups) == 0 {
		return nil
	}

	months := map[time.Time]struct{}{}
	for k := range groups {
		monthStart := time.Date(k.date.Year(), k.date.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[monthStart] = struct{}{}
	}
	EnsureGPUDigestPartitions(ctx, pool, months)

	txGPU, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for GPU digests: %w", err)
	}
	defer txGPU.Rollback(ctx)
	if err := db.SetLocalIngestStatementTimeout(ctx, txGPU); err != nil {
		return fmt.Errorf("set ingest statement timeout: %w", err)
	}

	type gpuGroupEntry struct {
		key gpuKey
		agg *gpuAgg
	}
	gpuEntries := make([]gpuGroupEntry, 0, len(groups))
	for k, g := range groups {
		gpuEntries = append(gpuEntries, gpuGroupEntry{key: k, agg: g})
	}
	for chunkStart := 0; chunkStart < len(gpuEntries); chunkStart += maxPgxBatchQueue {
		chunkEnd := chunkStart + maxPgxBatchQueue
		if chunkEnd > len(gpuEntries) {
			chunkEnd = len(gpuEntries)
		}
		batch := &pgx.Batch{}
		for _, entry := range gpuEntries[chunkStart:chunkEnd] {
			k, g := entry.key, entry.agg
			batch.Queue(`
			INSERT INTO gpu_container_digests (
				interval_start, cluster_uuid, namespace, workload, workload_type, container_name,
				gpu_model_name, gpu_profile_name, node_name,
				fb_usage_min_mib, fb_usage_max_mib, fb_usage_avg_mib,
				tensor_pipe_active_min, tensor_pipe_active_max, tensor_pipe_active_avg,
				dram_active_min, dram_active_max, dram_active_avg,
				sm_active_min, sm_active_max, sm_active_avg
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
			ON CONFLICT (cluster_uuid, namespace, workload, container_name, gpu_model_name, interval_start)
			DO UPDATE SET
				gpu_profile_name = EXCLUDED.gpu_profile_name,
				node_name = EXCLUDED.node_name,
				fb_usage_min_mib = EXCLUDED.fb_usage_min_mib,
				fb_usage_max_mib = EXCLUDED.fb_usage_max_mib,
				fb_usage_avg_mib = EXCLUDED.fb_usage_avg_mib,
				tensor_pipe_active_min = EXCLUDED.tensor_pipe_active_min,
				tensor_pipe_active_max = EXCLUDED.tensor_pipe_active_max,
				tensor_pipe_active_avg = EXCLUDED.tensor_pipe_active_avg,
				dram_active_min = EXCLUDED.dram_active_min,
				dram_active_max = EXCLUDED.dram_active_max,
				dram_active_avg = EXCLUDED.dram_active_avg,
				sm_active_min = EXCLUDED.sm_active_min,
				sm_active_max = EXCLUDED.sm_active_max,
				sm_active_avg = EXCLUDED.sm_active_avg`,
				k.date, clusterUUID, k.namespace, k.workload, g.workloadType, k.container,
				g.modelName, g.profileName, g.nodeName,
				g.fbMinVal, g.fbMaxVal, safeMeanInt32(g.fbAvgSum, g.count),
				g.tensorMinVal, g.tensorMaxVal, safeMeanInt32(g.tensorAvgSum, g.count),
				g.dramMinVal, g.dramMaxVal, safeMeanInt32(g.dramAvgSum, g.count),
				g.smMinVal, g.smMaxVal, safeMeanInt32(g.smAvgSum, g.count),
			)
		}
		if err := flushQueuedBatch(ctx, txGPU, batch, chunkEnd-chunkStart); err != nil {
			return fmt.Errorf("upsert GPU digest: %w", err)
		}
	}
	if err := txGPU.Commit(ctx); err != nil {
		return fmt.Errorf("commit GPU digests tx: %w", err)
	}

	logging.ForOrg(orgID, clusterUUID).Infof("ProcessCSVToDigests: upserted %d GPU digests",
		len(groups))
	return nil
}

func safeMeanInt32(sum int64, count int) int32 {
	if count <= 0 {
		return 0
	}
	return int32((sum + int64(count)/2) / int64(count))
}
