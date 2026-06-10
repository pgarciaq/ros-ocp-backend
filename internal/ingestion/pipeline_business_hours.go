package ingestion

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
)

func buildBusinessHoursGroups(
	rows []MetricRow,
	orgID, clusterUUID string,
	cache *bhschedule.Cache,
) map[DigestKey][]MetricRow {
	if cache == nil {
		return nil
	}
	out := make(map[DigestKey][]MetricRow, len(rows)/24+1)
	for _, row := range rows {
		sched := cache.Resolve(row.Namespace)
		if !sched.Enabled {
			continue
		}
		weightFn := BusinessHoursRowWeightFn(sched)
		if weightFn != nil && weightFn(row) <= 0 {
			continue
		}
		bucketDate := time.Date(
			row.IntervalStart.Year(), row.IntervalStart.Month(), row.IntervalStart.Day(),
			0, 0, 0, 0, time.UTC,
		)
		key := DigestKey{
			OrgID:         orgID,
			ClusterUUID:   clusterUUID,
			Namespace:     row.Namespace,
			Workload:      row.WorkloadName,
			WorkloadType:  row.WorkloadType,
			ContainerName: row.ContainerName,
			BucketDate:    bucketDate,
			ScheduleType:  ScheduleTypeBusinessHours,
		}
		out[key] = append(out[key], row)
	}
	return out
}

func mergeDigestGroups(all, bh map[DigestKey][]MetricRow) map[DigestKey][]MetricRow {
	merged := make(map[DigestKey][]MetricRow, len(all)+len(bh))
	for k, g := range all {
		merged[k] = g
	}
	for k, g := range bh {
		merged[k] = g
	}
	return merged
}

func rowWeightFnForDigestKey(key DigestKey, cache *bhschedule.Cache) RowWeightFunc {
	if key.ScheduleType != ScheduleTypeBusinessHours || cache == nil {
		return nil
	}
	sched := cache.Resolve(key.Namespace)
	if !sched.Enabled {
		return nil
	}
	return BusinessHoursRowWeightFn(sched)
}

// pruneBusinessHoursDigests removes business_hours digest rows when no enabled schedule applies
// (e.g. after DELETE of the last schedule row and re-ingestion).
func pruneBusinessHoursDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	return bhschedule.PruneClusterBusinessHoursDigests(ctx, pool, orgID, clusterUUID)
}

func upsertContainerDigests(
	ctx context.Context,
	pool *pgxpool.Pool,
	grouped map[DigestKey][]MetricRow,
	scheduleCache *bhschedule.Cache,
) error {
	txDigests, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for container digests: %w", err)
	}
	defer txDigests.Rollback(ctx)
	if err := db.SetLocalIngestStatementTimeout(ctx, txDigests); err != nil {
		return fmt.Errorf("set ingest statement timeout: %w", err)
	}
	if err := upsertContainerDigestsOnSender(ctx, txDigests, grouped, scheduleCache); err != nil {
		return err
	}
	if err := txDigests.Commit(ctx); err != nil {
		return fmt.Errorf("commit container digests tx: %w", err)
	}
	return nil
}

func upsertContainerDigestsOnSender(
	ctx context.Context,
	sender pgxBatchSender,
	grouped map[DigestKey][]MetricRow,
	scheduleCache *bhschedule.Cache,
) error {
	digestKeys := make([]DigestKey, 0, len(grouped))
	for k := range grouped {
		digestKeys = append(digestKeys, k)
	}

	for chunkStart := 0; chunkStart < len(digestKeys); chunkStart += maxPgxBatchQueue {
		chunkEnd := chunkStart + maxPgxBatchQueue
		if chunkEnd > len(digestKeys) {
			chunkEnd = len(digestKeys)
		}
		batch := &pgx.Batch{}
		for _, key := range digestKeys[chunkStart:chunkEnd] {
			group := grouped[key]
			weightFn := rowWeightFnForDigestKey(key, scheduleCache)
			d := ComputeContainerDigestWeighted(key, group, weightFn)
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
				desired_replicas, available_replicas
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
				$42, $43
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
				workload_type = EXCLUDED.workload_type`,
				key.BucketDate.Format("2006-01-02"),
				key.OrgID, key.ClusterUUID,
				key.Namespace, key.Workload, key.WorkloadType, key.ContainerName, string(key.ScheduleType),
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
		if err := flushQueuedBatch(ctx, sender, batch, chunkEnd-chunkStart); err != nil {
			return fmt.Errorf("upsert digest: %w", err)
		}
	}
	return nil
}
