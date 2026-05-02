package ingestion

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
)

// EnsureSamplePartitions creates monthly partitions of container_usage_samples
// for every month that appears in the ingested data. Idempotent via IF NOT EXISTS.
func EnsureSamplePartitions(ctx context.Context, pool *pgxpool.Pool, rows []MetricRow) {
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
			log.Warnf("EnsureSamplePartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}

// upsertUsageSamples batch-upserts raw CSV rows into container_usage_samples.
func upsertUsageSamples(ctx context.Context, pool *pgxpool.Pool, rows []MetricRow, orgID, clusterUUID string) error {
	if len(rows) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, r := range rows {
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

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range rows {
		if _, err := br.Exec(); err != nil {
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
func EnsureDigestPartitions(ctx context.Context, pool *pgxpool.Pool, keys []DigestKey) {
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
			log.Warnf("EnsureDigestPartitions: %s: %v (non-fatal)", partName, err)
		}
	}
}

// ProcessCSVToDigests is the full native engine ingestion pipeline:
// parse CSV -> validate -> group by container+day -> compute digests -> upsert to DB.
func ProcessCSVToDigests(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	rows, err := ParseCSVRows(r)
	if err != nil {
		return fmt.Errorf("parse CSV: %w", err)
	}
	if len(rows) == 0 {
		log.Infof("ProcessCSVToDigests: no rows parsed for org=%s cluster=%s", orgID, clusterUUID)
		return nil
	}

	// Persist raw samples for boxplot computation at query time.
	EnsureSamplePartitions(ctx, pool, rows)
	if err := upsertUsageSamples(ctx, pool, rows, orgID, clusterUUID); err != nil {
		return fmt.Errorf("upsert usage samples: %w", err)
	}

	grouped := GroupCSVRows(rows, orgID, clusterUUID)
	log.Infof("ProcessCSVToDigests: %d rows -> %d groups for org=%s cluster=%s",
		len(rows), len(grouped), orgID, clusterUUID)

	digestKeys := make([]DigestKey, 0, len(grouped))
	for k := range grouped {
		digestKeys = append(digestKeys, k)
	}
	EnsureDigestPartitions(ctx, pool, digestKeys)

	batch := &pgx.Batch{}
	for key, group := range grouped {
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
				pod_count_min, pod_count_max, pod_count_avg
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7,
				$8, $9, $10, $11, $12,
				$13, $14, $15, $16, $17, $18,
				$19, $20,
				$21, $22, $23, $24, $25,
				$26, $27, $28, $29, $30, $31,
				$32, $33,
				$34, $35, $36, $37,
				$38, $39, $40
			)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, bucket_date)
			DO UPDATE SET
				cpu_request_p50_mc = EXCLUDED.cpu_request_p50_mc,
				cpu_request_p60_mc = EXCLUDED.cpu_request_p60_mc,
				cpu_request_p95_mc = EXCLUDED.cpu_request_p95_mc,
				cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc,
				cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc,
				cpu_usage_max_mc = EXCLUDED.cpu_usage_max_mc,
				memory_request_p50_kib = EXCLUDED.memory_request_p50_kib,
				memory_request_p60_kib = EXCLUDED.memory_request_p60_kib,
				memory_request_p95_kib = EXCLUDED.memory_request_p95_kib,
				memory_usage_p50_kib = EXCLUDED.memory_usage_p50_kib,
				memory_usage_p60_kib = EXCLUDED.memory_usage_p60_kib,
				memory_usage_p95_kib = EXCLUDED.memory_usage_p95_kib,
				memory_usage_max_kib = EXCLUDED.memory_usage_max_kib,
				oom_count_sum = EXCLUDED.oom_count_sum,
				cpu_usage_mean_mc = EXCLUDED.cpu_usage_mean_mc,
				memory_usage_mean_kib = EXCLUDED.memory_usage_mean_kib,
				sample_count = EXCLUDED.sample_count,
				pod_count_min = EXCLUDED.pod_count_min,
				pod_count_max = EXCLUDED.pod_count_max,
				pod_count_avg = EXCLUDED.pod_count_avg`,
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
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range grouped {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("upsert digest: %w", err)
		}
	}

	log.Infof("ProcessCSVToDigests: upserted %d digests for org=%s cluster=%s",
		len(grouped), orgID, clusterUUID)

	if err := upsertGPUDigests(ctx, pool, rows, orgID, clusterUUID); err != nil {
		log.Warnf("ProcessCSVToDigests: GPU digest upsert failed (non-fatal): %v", err)
	}

	return nil
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

// upsertGPUDigests extracts GPU rows from parsed CSV and writes daily aggregates
// to the gpu_container_digests table. Rows without GPU data (HasGPU()==false)
// are silently skipped.
func upsertGPUDigests(ctx context.Context, pool *pgxpool.Pool, rows []MetricRow, orgID, clusterUUID string) error {
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
		fbMinVals    []float64
		fbMaxVals    []float64
		fbAvgVals    []float64
		tensorMin    []float64
		tensorMax    []float64
		tensorAvg    []float64
		dramMin      []float64
		dramMax      []float64
		dramAvg      []float64
		smMin        []float64
		smMax        []float64
		smAvg        []float64
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
			}
			groups[k] = g
		}
		if r.Node != "" {
			g.nodeName = r.Node
		}
		g.fbMinVals = append(g.fbMinVals, r.AcceleratorFBUsageMin)
		g.fbMaxVals = append(g.fbMaxVals, r.AcceleratorFBUsageMax)
		g.fbAvgVals = append(g.fbAvgVals, r.AcceleratorFBUsageAvg)
		g.tensorMin = append(g.tensorMin, r.TensorPipeActiveMin)
		g.tensorMax = append(g.tensorMax, r.TensorPipeActiveMax)
		g.tensorAvg = append(g.tensorAvg, r.TensorPipeActiveAvg)
		g.dramMin = append(g.dramMin, r.DRAMActiveMin)
		g.dramMax = append(g.dramMax, r.DRAMActiveMax)
		g.dramAvg = append(g.dramAvg, r.DRAMActiveAvg)
		g.smMin = append(g.smMin, r.SMActiveMin)
		g.smMax = append(g.smMax, r.SMActiveMax)
		g.smAvg = append(g.smAvg, r.SMActiveAvg)
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

	batch := &pgx.Batch{}
	for k, g := range groups {
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
			minFloat(g.fbMinVals), maxFloat(g.fbMaxVals), meanFloat(g.fbAvgVals),
			minFloat(g.tensorMin), maxFloat(g.tensorMax), meanFloat(g.tensorAvg),
			minFloat(g.dramMin), maxFloat(g.dramMax), meanFloat(g.dramAvg),
			minFloat(g.smMin), maxFloat(g.smMax), meanFloat(g.smAvg),
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for range groups {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("upsert GPU digest: %w", err)
		}
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
