package pgdigest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

type namespaceWrite struct {
	Namespace string
	Row       types.DigestRow
}

// WriteNamespaceDigests upserts already-computed namespace usage days as all_hours.
// Hard/used quota columns on daily_namespace_digests are written as 0 (CLI DigestRow
// does not carry them). Empty grouped is a no-op.
func WriteNamespaceDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, grouped map[namespace.NamespaceKey][]types.DigestRow) error {
	return WriteNamespaceDigestsWithSchedule(ctx, pool, orgID, clusterUUID, ScheduleAllHours, grouped)
}

// WriteNamespaceDigestsWithSchedule upserts namespace days with scheduleType.
func WriteNamespaceDigestsWithSchedule(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, scheduleType string, grouped map[namespace.NamespaceKey][]types.DigestRow) error {
	rows := flattenNamespaceWrites(grouped)
	if len(rows) == 0 {
		return nil
	}
	if scheduleType == "" {
		return fmt.Errorf("pgdigest: schedule_type is required")
	}
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return err
	}
	months := make([]time.Time, len(rows))
	for i, r := range rows {
		months[i] = r.Row.BucketDate
	}
	if err := ensureRangePartitions(ctx, pool, "daily_namespace_digests", months); err != nil {
		return err
	}
	return withWriteTx(ctx, pool, func(tx pgx.Tx) error {
		if err := flushQueued(ctx, tx, len(rows), func(batch *pgx.Batch, i int) {
			queueNamespaceInsert(batch, orgID, clusterUUID, scheduleType, rows[i])
		}); err != nil {
			return fmt.Errorf("upsert namespace digest: %w", err)
		}
		return nil
	})
}

func flattenNamespaceWrites(grouped map[namespace.NamespaceKey][]types.DigestRow) []namespaceWrite {
	var out []namespaceWrite
	for k, days := range grouped {
		for _, row := range days {
			out = append(out, namespaceWrite{Namespace: k.Namespace, Row: row})
		}
	}
	slices.SortFunc(out, func(a, b namespaceWrite) int {
		if c := cmp.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		return a.Row.BucketDate.Compare(b.Row.BucketDate)
	})
	return out
}

func queueNamespaceInsert(batch *pgx.Batch, orgID, clusterUUID, scheduleType string, w namespaceWrite) {
	d := w.Row
	batch.Queue(`
			INSERT INTO daily_namespace_digests (
				bucket_date, org_id, cluster_uuid, namespace, schedule_type,
				cpu_request_p50_mc, cpu_request_p60_mc, cpu_request_p95_mc, cpu_request_p98_mc, cpu_request_p99_mc,
				cpu_usage_p50_mc, cpu_usage_p60_mc, cpu_usage_p95_mc, cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
				memory_request_p50_kib, memory_request_p60_kib, memory_request_p95_kib, memory_request_p98_kib, memory_request_p99_kib,
				memory_usage_p50_kib, memory_usage_p60_kib, memory_usage_p95_kib, memory_usage_p98_kib, memory_usage_p99_kib, memory_usage_max_kib,
				cpu_usage_mean_mc, memory_usage_mean_kib, sample_count,
				cpu_request_hard_millicores, cpu_limit_hard_millicores,
				memory_request_hard_bytes, memory_limit_hard_bytes,
				cpu_request_used_millicores, cpu_limit_used_millicores,
				memory_request_used_bytes, memory_limit_used_bytes
			) VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8, $9, $10,
				$11, $12, $13, $14, $15, $16,
				$17, $18, $19, $20, $21,
				$22, $23, $24, $25, $26, $27,
				$28, $29, $30,
				$31, $32, $33, $34, $35, $36, $37, $38
			)
			ON CONFLICT (org_id, cluster_uuid, namespace, bucket_date, schedule_type)
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
				cpu_usage_mean_mc = EXCLUDED.cpu_usage_mean_mc,
				memory_usage_mean_kib = EXCLUDED.memory_usage_mean_kib,
				sample_count = EXCLUDED.sample_count,
				cpu_request_hard_millicores = EXCLUDED.cpu_request_hard_millicores,
				cpu_limit_hard_millicores = EXCLUDED.cpu_limit_hard_millicores,
				memory_request_hard_bytes = EXCLUDED.memory_request_hard_bytes,
				memory_limit_hard_bytes = EXCLUDED.memory_limit_hard_bytes,
				cpu_request_used_millicores = EXCLUDED.cpu_request_used_millicores,
				cpu_limit_used_millicores = EXCLUDED.cpu_limit_used_millicores,
				memory_request_used_bytes = EXCLUDED.memory_request_used_bytes,
				memory_limit_used_bytes = EXCLUDED.memory_limit_used_bytes`,
		d.BucketDate.Format("2006-01-02"),
		orgID, clusterUUID, w.Namespace, scheduleType,
		d.CPURequestP50MC, d.CPURequestP60MC, d.CPURequestP95MC, d.CPURequestP98MC, d.CPURequestP99MC,
		d.CPUUsageP50MC, d.CPUUsageP60MC, d.CPUUsageP95MC, d.CPUUsageP98MC, d.CPUUsageP99MC, d.CPUUsageMaxMC,
		d.MemRequestP50KiB, d.MemRequestP60KiB, d.MemRequestP95KiB, d.MemRequestP98KiB, d.MemRequestP99KiB,
		d.MemUsageP50KiB, d.MemUsageP60KiB, d.MemUsageP95KiB, d.MemUsageP98KiB, d.MemUsageP99KiB, d.MemUsageMaxKiB,
		d.CPUUsageMeanMC, d.MemUsageMeanKiB, d.SampleCount,
		int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0),
	)
}
