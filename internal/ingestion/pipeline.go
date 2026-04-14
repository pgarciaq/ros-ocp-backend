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
				memory_request_p50_kib, memory_request_p95_kib,
				memory_usage_p50_kib, memory_usage_p95_kib, memory_usage_max_kib,
				memory_rss_p95_kib, memory_rss_max_kib,
				oom_count_sum, cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7,
				$8, $9, $10, $11, $12,
				$13, $14, $15, $16, $17, $18,
				$19, $20, $21, $22, $23, $24, $25,
				$26, $27, $28, $29, $30, $31
			)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, bucket_date)
			DO UPDATE SET
				cpu_request_p50_mc = EXCLUDED.cpu_request_p50_mc,
				cpu_request_p60_mc = EXCLUDED.cpu_request_p60_mc,
				cpu_request_p95_mc = EXCLUDED.cpu_request_p95_mc,
				cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc,
				cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc,
				cpu_usage_max_mc = EXCLUDED.cpu_usage_max_mc,
				memory_usage_p50_kib = EXCLUDED.memory_usage_p50_kib,
				memory_usage_p95_kib = EXCLUDED.memory_usage_p95_kib,
				memory_usage_max_kib = EXCLUDED.memory_usage_max_kib,
				oom_count_sum = EXCLUDED.oom_count_sum,
				cpu_usage_mean_mc = EXCLUDED.cpu_usage_mean_mc,
				memory_usage_mean_kib = EXCLUDED.memory_usage_mean_kib,
				sample_count = EXCLUDED.sample_count`,
			key.BucketDate.Format("2006-01-02"),
			orgID, clusterUUID,
			key.Namespace, key.Workload, key.WorkloadType, key.ContainerName,
			d.CPURequestP50MC, d.CPURequestP60MC, d.CPURequestP95MC, d.CPURequestP98MC, d.CPURequestP99MC,
			d.CPUUsageP50MC, d.CPUUsageP60MC, d.CPUUsageP95MC, d.CPUUsageP98MC, d.CPUUsageP99MC, d.CPUUsageMaxMC,
			d.CPUThrottleP95MC, d.CPUThrottleMaxMC,
			d.MemRequestP50KiB, d.MemRequestP95KiB,
			d.MemUsageP50KiB, d.MemUsageP95KiB, d.MemUsageMaxKiB,
			d.MemRSSP95KiB, d.MemRSSMaxKiB,
			d.OOMCountSum, d.CPUUsageMeanMC, d.MemUsageMeanKiB, d.SampleCount,
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
	return nil
}
