package pgdigest

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// WriteContainerDigests upserts already-computed container digests as all_hours
// with last-write-wins (not ingest GREATEST; PVC/quota writers merge).
// orgID and clusterUUID are stamped from caller identity (YAML), not from CSV rows.
func WriteContainerDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, digests []types.KeyedDigest) error {
	return WriteContainerDigestsWithSchedule(ctx, pool, orgID, clusterUUID, ScheduleAllHours, digests)
}

// WriteContainerDigestsWithSchedule upserts container digests with scheduleType
// (all_hours or business_hours). Empty digests is a no-op.
func WriteContainerDigestsWithSchedule(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, scheduleType string, digests []types.KeyedDigest) error {
	if len(digests) == 0 {
		return nil
	}
	rows := make([]Row, len(digests))
	for i, d := range digests {
		rows[i] = Row{
			OrgID:        orgID,
			ClusterUUID:  clusterUUID,
			ScheduleType: scheduleType,
			Digest:       d,
		}
	}
	return WriteRows(ctx, pool, rows)
}

// WriteRows creates monthly partitions and upserts digest rows in one transaction.
func WriteRows(ctx context.Context, pool *pgxpool.Pool, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	if err := validateRows(rows); err != nil {
		return err
	}
	if err := ensurePartitionsForRows(ctx, pool, rows); err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for container digests: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := WriteRowsOnSender(ctx, tx, rows); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit container digests tx: %w", err)
	}
	return nil
}

// WriteRowsOnSender upserts on an existing tx/pool. Caller owns timeout, retry, and partitions.
func WriteRowsOnSender(ctx context.Context, sender pgxBatchSender, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	if err := validateRows(rows); err != nil {
		return err
	}
	sorted := slices.Clone(rows)
	sortRows(sorted)
	for chunkStart := 0; chunkStart < len(sorted); chunkStart += maxPgxBatchQueue {
		chunkEnd := min(chunkStart+maxPgxBatchQueue, len(sorted))
		batch := &pgx.Batch{}
		for _, r := range sorted[chunkStart:chunkEnd] {
			queueDigestInsert(batch, r)
		}
		if err := flushBatch(ctx, sender, batch); err != nil {
			return fmt.Errorf("upsert digest: %w", err)
		}
	}
	return nil
}

func validateRows(rows []Row) error {
	for i, r := range rows {
		if r.OrgID == "" {
			return fmt.Errorf("pgdigest: row %d: org_id is required", i)
		}
		if r.ClusterUUID == "" {
			return fmt.Errorf("pgdigest: row %d: cluster_uuid is required", i)
		}
		if r.ScheduleType == "" {
			return fmt.Errorf("pgdigest: row %d: schedule_type is required", i)
		}
	}
	return nil
}

func sortRows(rows []Row) {
	slices.SortFunc(rows, func(a, b Row) int {
		if c := cmp.Compare(a.OrgID, b.OrgID); c != 0 {
			return c
		}
		if c := cmp.Compare(a.ClusterUUID, b.ClusterUUID); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Digest.Key.Namespace, b.Digest.Key.Namespace); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Digest.Key.Workload, b.Digest.Key.Workload); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Digest.Key.WorkloadType, b.Digest.Key.WorkloadType); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Digest.Key.ContainerName, b.Digest.Key.ContainerName); c != 0 {
			return c
		}
		if c := a.Digest.Row.BucketDate.Compare(b.Digest.Row.BucketDate); c != 0 {
			return c
		}
		return cmp.Compare(a.ScheduleType, b.ScheduleType)
	})
}

func queueDigestInsert(batch *pgx.Batch, r Row) {
	k := r.Digest.Key
	d := r.Digest.Row
	batch.Queue(`
			INSERT INTO daily_container_digests (
				bucket_date, org_id, cluster_uuid, namespace, workload, workload_type, container_name, schedule_type,
				cpu_request_p50_mc, cpu_request_p60_mc, cpu_request_p95_mc, cpu_request_p98_mc, cpu_request_p99_mc,
				cpu_usage_p50_mc, cpu_usage_p60_mc, cpu_usage_p95_mc, cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
				cpu_throttle_p95_mc, cpu_throttle_max_mc,
				memory_request_p50_kib, memory_request_p60_kib, memory_request_p95_kib, memory_request_p98_kib, memory_request_p99_kib,
				memory_usage_p50_kib, memory_usage_p60_kib, memory_usage_p95_kib, memory_usage_p98_kib, memory_usage_p99_kib, memory_usage_max_kib,
				memory_rss_p95_kib, memory_rss_max_kib,
				oom_count_sum, cpu_usage_mean_mc, memory_usage_mean_kib, sample_count,
				pod_count_min, pod_count_max, pod_count_avg,
				desired_replicas, available_replicas,
				cpu_usage_cv_bp
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8,
				$9, $10, $11, $12, $13,
				$14, $15, $16, $17, $18, $19,
				$20, $21,
				$22, $23, $24, $25, $26,
				$27, $28, $29, $30, $31, $32,
				$33, $34,
				$35, $36, $37, $38,
				$39, $40, $41,
				$42, $43,
				$44
			)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, bucket_date, schedule_type)
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
				cpu_usage_cv_bp = EXCLUDED.cpu_usage_cv_bp,
				workload_type = EXCLUDED.workload_type`,
		d.BucketDate.Format("2006-01-02"),
		r.OrgID, r.ClusterUUID,
		k.Namespace, k.Workload, k.WorkloadType, k.ContainerName, r.ScheduleType,
		d.CPURequestP50MC, d.CPURequestP60MC, d.CPURequestP95MC, d.CPURequestP98MC, d.CPURequestP99MC,
		d.CPUUsageP50MC, d.CPUUsageP60MC, d.CPUUsageP95MC, d.CPUUsageP98MC, d.CPUUsageP99MC, d.CPUUsageMaxMC,
		d.CPUThrottleP95MC, d.CPUThrottleMaxMC,
		d.MemRequestP50KiB, d.MemRequestP60KiB, d.MemRequestP95KiB, d.MemRequestP98KiB, d.MemRequestP99KiB,
		d.MemUsageP50KiB, d.MemUsageP60KiB, d.MemUsageP95KiB, d.MemUsageP98KiB, d.MemUsageP99KiB, d.MemUsageMaxKiB,
		d.MemRSSP95KiB, d.MemRSSMaxKiB,
		d.OOMCountSum, d.CPUUsageMeanMC, d.MemUsageMeanKiB, d.SampleCount,
		d.PodCountMin, d.PodCountMax, d.PodCountAvg,
		d.DesiredReplicas, d.AvailableReplicas,
		d.CPUUsageCVBP,
	)
}
