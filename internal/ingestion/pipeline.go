package ingestion

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

// maxPgxBatchQueue caps pgx.Batch queue depth to avoid unbounded RAM on large clusters.
const maxPgxBatchQueue = 500

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
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, sample_time)
			DO UPDATE SET
				cpu_usage_mc = EXCLUDED.cpu_usage_mc,
				mem_usage_kib = EXCLUDED.mem_usage_kib`,
				r.IntervalStart, orgID, clusterUUID,
				r.Namespace, r.WorkloadName, r.WorkloadType, r.ContainerName,
				r.CPUUsageMC, r.MemUsageKiB,
			)
		}
		if err := flushQueuedBatch(ctx, tx, batch, end-start); err != nil {
			return fmt.Errorf("upsert usage sample: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit usage samples tx: %w", err)
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

// ParseAndDigestCSV parses container CSV rows with [ParseCSVRows] (which validates each row),
// groups by container and day, upserts usage samples and daily_container_digests,
// and returns the parsed rows for downstream ingest hooks.
//
// It does not run GPU or node digest upserts; callers that need the full legacy
// pipeline should use [ProcessCSVToDigests], which wraps this function.
func ParseAndDigestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]MetricRow, error) {
	rows, err := ParseCSVRows(r)
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}
	if len(rows) == 0 {
		log.Infof("ProcessCSVToDigests: no rows parsed for org=%s cluster=%s", orgID, clusterUUID)
		return nil, nil
	}

	// Persist raw samples for boxplot computation at query time.
	if err := EnsureSamplePartitions(ctx, pool, rows); err != nil {
		return nil, fmt.Errorf("sample partitions: %w", err)
	}
	if err := upsertUsageSamples(ctx, pool, rows, orgID, clusterUUID); err != nil {
		return nil, fmt.Errorf("upsert usage samples: %w", err)
	}

	grouped := GroupCSVRows(rows, orgID, clusterUUID)
	log.Infof("ProcessCSVToDigests: %d rows -> %d groups for org=%s cluster=%s",
		len(rows), len(grouped), orgID, clusterUUID)

	digestKeys := make([]DigestKey, 0, len(grouped))
	for k := range grouped {
		digestKeys = append(digestKeys, k)
	}
	if err := EnsureDigestPartitions(ctx, pool, digestKeys); err != nil {
		return nil, fmt.Errorf("digest partitions: %w", err)
	}

	txDigests, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx for container digests: %w", err)
	}
	defer txDigests.Rollback(ctx)

	for chunkStart := 0; chunkStart < len(digestKeys); chunkStart += maxPgxBatchQueue {
		chunkEnd := chunkStart + maxPgxBatchQueue
		if chunkEnd > len(digestKeys) {
			chunkEnd = len(digestKeys)
		}
		batch := &pgx.Batch{}
		for _, key := range digestKeys[chunkStart:chunkEnd] {
			group := grouped[key]
			d := ComputeContainerDigest(key, group)
			batch.Queue(`
			INSERT INTO daily_container_digests (
				bucket_date, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
				cpu_request_p50_mc, cpu_request_p60_mc, cpu_request_p95_mc, cpu_request_p98_mc, cpu_request_p99_mc,
				cpu_usage_p50_mc, cpu_usage_p60_mc, cpu_usage_p95_mc, cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
				cpu_throttle_p95_mc, cpu_throttle_max_mc,
				memory_request_p50_kib, memory_request_p60_kib, memory_request_p95_kib, memory_request_p98_kib, memory_request_p99_kib,
				memory_usage_p50_kib, memory_usage_p60_kib, memory_usage_p95_kib, memory_usage_p98_kib, memory_usage_p99_kib, memory_usage_max_kib,
				memory_rss_p95_kib, memory_rss_max_kib,
				oom_count_sum, cpu_usage_mean_mc, memory_usage_mean_kib, sample_count,
				pod_count_min, pod_count_max, pod_count_avg,
				desired_replicas, available_replicas
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7,
				$8, $9, $10, $11, $12,
				$13, $14, $15, $16, $17, $18,
				$19, $20,
				$21, $22, $23, $24, $25,
				$26, $27, $28, $29, $30, $31,
				$32, $33,
				$34, $35, $36, $37,
				$38, $39, $40,
				$41, $42
			)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, bucket_date)
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
				cpu_throttle_p95_mc = EXCLUDED.cpu_throttle_p95_mc,
				cpu_throttle_max_mc = EXCLUDED.cpu_throttle_max_mc,
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
				memory_rss_p95_kib = EXCLUDED.memory_rss_p95_kib,
				memory_rss_max_kib = EXCLUDED.memory_rss_max_kib,
				oom_count_sum = EXCLUDED.oom_count_sum,
				cpu_usage_mean_mc = EXCLUDED.cpu_usage_mean_mc,
				memory_usage_mean_kib = EXCLUDED.memory_usage_mean_kib,
				sample_count = EXCLUDED.sample_count,
				pod_count_min = EXCLUDED.pod_count_min,
				pod_count_max = EXCLUDED.pod_count_max,
				pod_count_avg = EXCLUDED.pod_count_avg,
				desired_replicas = EXCLUDED.desired_replicas,
				available_replicas = EXCLUDED.available_replicas,
				workload_type = EXCLUDED.workload_type`,
				key.BucketDate.Format("2006-01-02"),
				orgID, clusterUUID,
				key.Namespace, key.Workload, key.WorkloadType, key.ContainerName,
				d.CPURequestP50MC, d.CPURequestP60MC, d.CPURequestP95MC, d.CPURequestP98MC, d.CPURequestP99MC,
				d.CPUUsageP50MC, d.CPUUsageP60MC, d.CPUUsageP95MC, d.CPUUsageP98MC, d.CPUUsageP99MC, d.CPUUsageMaxMC,
				d.CPUThrottleP95MC, d.CPUThrottleMaxMC,
				d.MemRequestP50KiB, d.MemRequestP60KiB, d.MemRequestP95KiB, d.MemRequestP98KiB, d.MemRequestP99KiB,
				d.MemUsageP50KiB, d.MemUsageP60KiB, d.MemUsageP95KiB, d.MemUsageP98KiB, d.MemUsageP99KiB, d.MemUsageMaxKiB,
				d.MemRSSP95KiB, d.MemRSSMaxKiB,
				d.OOMCountSum, d.CPUUsageMeanMC, d.MemUsageMeanKiB, d.SampleCount,
				d.PodCountMin, d.PodCountMax, d.PodCountAvg,
				d.DesiredReplicas, d.AvailableReplicas,
			)
		}
		if err := flushQueuedBatch(ctx, txDigests, batch, chunkEnd-chunkStart); err != nil {
			return nil, fmt.Errorf("upsert digest: %w", err)
		}
	}
	if err := txDigests.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit container digests tx: %w", err)
	}

	log.Infof("ProcessCSVToDigests: upserted %d digests for org=%s cluster=%s",
		len(grouped), orgID, clusterUUID)

	return rows, nil
}

// ProcessCSVToDigests parses container CSV and upserts container digests, then always runs GPU and node
// digest upserts. Used by CLI/tools and tests; the Kafka native path uses services.processContainerDigestFallback
// instead so ROS_ENABLED_PLUGINS can disable GPU/node upserts when the container ingestor falls back.
func ProcessCSVToDigests(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseAndDigestCSV(ctx, pool, r, orgID, clusterUUID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	if err := UpsertGPUDigests(ctx, pool, rows, orgID, clusterUUID); err != nil {
		return fmt.Errorf("GPU digest upsert: %w", err)
	}

	if err := UpsertNodeDigests(ctx, pool, rows, orgID, clusterUUID); err != nil {
		return fmt.Errorf("node digest upsert: %w", err)
	}

	return nil
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
			log.Warnf("EnsureGPUDigestPartitions: %s: %v (non-fatal)", partName, err)
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
		fbMinVal     float64
		fbMaxVal     float64
		fbAvgSum     float64
		tensorMinVal float64
		tensorMaxVal float64
		tensorAvgSum float64
		dramMinVal   float64
		dramMaxVal   float64
		dramAvgSum   float64
		smMinVal     float64
		smMaxVal     float64
		smAvgSum     float64
	}

	groups := map[gpuKey]*gpuAgg{}
	for _, r := range rows {
		if !r.HasGPU() {
			continue
		}
		day := time.Date(r.IntervalStart.Year(), r.IntervalStart.Month(), r.IntervalStart.Day(), 0, 0, 0, 0, time.UTC)
		k := gpuKey{date: day, namespace: r.Namespace, workload: r.WorkloadName, container: r.ContainerName}
		g, ok := groups[k]
		if !ok {
			g = &gpuAgg{
				workloadType: r.WorkloadType,
				modelName:    r.AcceleratorModelName,
				profileName:  r.AcceleratorProfileName,
				fbMinVal:     r.AcceleratorFBUsageMin,
				fbMaxVal:     r.AcceleratorFBUsageMax,
				tensorMinVal: r.TensorPipeActiveMin,
				tensorMaxVal: r.TensorPipeActiveMax,
				dramMinVal:   r.DRAMActiveMin,
				dramMaxVal:   r.DRAMActiveMax,
				smMinVal:     r.SMActiveMin,
				smMaxVal:     r.SMActiveMax,
			}
			groups[k] = g
		} else {
			if r.AcceleratorFBUsageMin < g.fbMinVal {
				g.fbMinVal = r.AcceleratorFBUsageMin
			}
			if r.AcceleratorFBUsageMax > g.fbMaxVal {
				g.fbMaxVal = r.AcceleratorFBUsageMax
			}
			if r.TensorPipeActiveMin < g.tensorMinVal {
				g.tensorMinVal = r.TensorPipeActiveMin
			}
			if r.TensorPipeActiveMax > g.tensorMaxVal {
				g.tensorMaxVal = r.TensorPipeActiveMax
			}
			if r.DRAMActiveMin < g.dramMinVal {
				g.dramMinVal = r.DRAMActiveMin
			}
			if r.DRAMActiveMax > g.dramMaxVal {
				g.dramMaxVal = r.DRAMActiveMax
			}
			if r.SMActiveMin < g.smMinVal {
				g.smMinVal = r.SMActiveMin
			}
			if r.SMActiveMax > g.smMaxVal {
				g.smMaxVal = r.SMActiveMax
			}
		}
		if r.Node != "" {
			g.nodeName = r.Node
		}
		g.count++
		g.fbAvgSum += r.AcceleratorFBUsageAvg
		g.tensorAvgSum += r.TensorPipeActiveAvg
		g.dramAvgSum += r.DRAMActiveAvg
		g.smAvgSum += r.SMActiveAvg
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
				g.fbMinVal, g.fbMaxVal, safeMean(g.fbAvgSum, g.count),
				g.tensorMinVal, g.tensorMaxVal, safeMean(g.tensorAvgSum, g.count),
				g.dramMinVal, g.dramMaxVal, safeMean(g.dramAvgSum, g.count),
				g.smMinVal, g.smMaxVal, safeMean(g.smAvgSum, g.count),
			)
		}
		if err := flushQueuedBatch(ctx, txGPU, batch, chunkEnd-chunkStart); err != nil {
			return fmt.Errorf("upsert GPU digest: %w", err)
		}
	}
	if err := txGPU.Commit(ctx); err != nil {
		return fmt.Errorf("commit GPU digests tx: %w", err)
	}

	log.Infof("ProcessCSVToDigests: upserted %d GPU digests for org=%s cluster=%s",
		len(groups), orgID, clusterUUID)
	return nil
}

func minFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func maxFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func meanFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func safeMean(sum float64, count int) float64 {
	if count <= 0 {
		return 0
	}
	return sum / float64(count)
}
