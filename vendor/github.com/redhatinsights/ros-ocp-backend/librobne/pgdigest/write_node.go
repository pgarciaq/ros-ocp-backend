package pgdigest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
)

// WriteNodeDigests upserts already-computed node daily rows. Empty slice is a no-op.
func WriteNodeDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, rows []node.DigestRow) error {
	if len(rows) == 0 {
		return nil
	}
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return err
	}
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b node.DigestRow) int {
		if c := cmp.Compare(a.Node, b.Node); c != 0 {
			return c
		}
		return a.BucketDate.Compare(b.BucketDate)
	})
	months := make([]time.Time, len(sorted))
	for i, r := range sorted {
		months[i] = r.BucketDate
	}
	if err := ensureRangePartitions(ctx, pool, "daily_node_digests", months); err != nil {
		return err
	}
	return withWriteTx(ctx, pool, func(tx pgx.Tx) error {
		if err := flushQueued(ctx, tx, len(sorted), func(batch *pgx.Batch, i int) {
			queueNodeInsert(batch, orgID, clusterUUID, sorted[i])
		}); err != nil {
			return fmt.Errorf("upsert node digest: %w", err)
		}
		return nil
	})
}

func queueNodeInsert(batch *pgx.Batch, orgID, clusterUUID string, d node.DigestRow) {
	batch.Queue(`
			INSERT INTO daily_node_digests (
				bucket_date, org_id, cluster_uuid, node,
				cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_max_mc,
				mem_usage_p50_kib, mem_usage_p95_kib, mem_usage_max_kib,
				max_cpu_allocatable_mc, max_mem_allocatable_kib,
				max_cpu_requests_mc, max_mem_requests_kib,
				max_pod_count, pod_capacity, instance_type, machineset_name, sample_count, node_gpu_count
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
			ON CONFLICT (org_id, cluster_uuid, node, bucket_date)
			DO UPDATE SET
				cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc,
				cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc,
				cpu_usage_max_mc = EXCLUDED.cpu_usage_max_mc,
				mem_usage_p50_kib = EXCLUDED.mem_usage_p50_kib,
				mem_usage_p95_kib = EXCLUDED.mem_usage_p95_kib,
				mem_usage_max_kib = EXCLUDED.mem_usage_max_kib,
				max_cpu_allocatable_mc = EXCLUDED.max_cpu_allocatable_mc,
				max_mem_allocatable_kib = EXCLUDED.max_mem_allocatable_kib,
				max_cpu_requests_mc = EXCLUDED.max_cpu_requests_mc,
				max_mem_requests_kib = EXCLUDED.max_mem_requests_kib,
				max_pod_count = EXCLUDED.max_pod_count,
				pod_capacity = EXCLUDED.pod_capacity,
				instance_type = EXCLUDED.instance_type,
				machineset_name = EXCLUDED.machineset_name,
				sample_count = EXCLUDED.sample_count,
				node_gpu_count = EXCLUDED.node_gpu_count`,
		d.BucketDate.Format("2006-01-02"), orgID, clusterUUID, d.Node,
		d.CPUUsageP50MC, d.CPUUsageP95MC, d.CPUUsageMaxMC,
		d.MemUsageP50KiB, d.MemUsageP95KiB, d.MemUsageMaxKiB,
		d.MaxCPUAllocMC, d.MaxMemAllocKiB,
		d.MaxCPURequestsMC, d.MaxMemRequestsKiB,
		d.MaxPodCount, nullInt64PodCapacity(d.PodCapacity),
		nullableString(d.InstanceType), nullableString(d.MachineSetName),
		d.SampleCount, d.NodeGPUCount,
	)
}
