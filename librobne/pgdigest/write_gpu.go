package pgdigest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
)

// DefaultGPUWorkloadType is stamped when GPUContainerKey has no workload type
// (table default; unique key does not include workload_type).
const DefaultGPUWorkloadType = "deployment"

// GPUContainerDigest is one already-computed gpu_container_digests row.
type GPUContainerDigest struct {
	Key          gpu.GPUContainerKey
	WorkloadType string
	Row          gpu.GPUDigestRow
}

// WriteGPUContainerDigests upserts already-computed GPU container days as all_hours
// with last-write-wins (same as ingest). Unique key includes org_id (#512 PR-3).
// Empty grouped is a no-op.
func WriteGPUContainerDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, grouped map[gpu.GPUContainerKey][]gpu.GPUDigestRow) error {
	return WriteGPUContainerDigestsWithSchedule(ctx, pool, orgID, clusterUUID, ScheduleAllHours, grouped)
}

// WriteGPUContainerDigestsWithSchedule upserts GPU container days with scheduleType.
func WriteGPUContainerDigestsWithSchedule(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, scheduleType string, grouped map[gpu.GPUContainerKey][]gpu.GPUDigestRow) error {
	rows := flattenGPUWrites(grouped)
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
		months[i] = r.Row.IntervalStart
	}
	if err := ensureRangePartitions(ctx, pool, "gpu_container_digests", months); err != nil {
		return err
	}
	return withWriteTx(ctx, pool, func(tx pgx.Tx) error {
		if err := flushQueued(ctx, tx, len(rows), func(batch *pgx.Batch, i int) {
			queueGPUInsert(batch, orgID, clusterUUID, scheduleType, rows[i])
		}); err != nil {
			return fmt.Errorf("upsert GPU digest: %w", err)
		}
		return nil
	})
}

func flattenGPUWrites(grouped map[gpu.GPUContainerKey][]gpu.GPUDigestRow) []GPUContainerDigest {
	var out []GPUContainerDigest
	for k, days := range grouped {
		for _, row := range days {
			out = append(out, GPUContainerDigest{Key: k, WorkloadType: DefaultGPUWorkloadType, Row: row})
		}
	}
	slices.SortFunc(out, func(a, b GPUContainerDigest) int {
		if c := cmp.Compare(a.Key.Namespace, b.Key.Namespace); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Key.Workload, b.Key.Workload); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Key.ContainerName, b.Key.ContainerName); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Row.GPUModelName, b.Row.GPUModelName); c != 0 {
			return c
		}
		return a.Row.IntervalStart.Compare(b.Row.IntervalStart)
	})
	return out
}

func queueGPUInsert(batch *pgx.Batch, orgID, clusterUUID, scheduleType string, w GPUContainerDigest) {
	wt := w.WorkloadType
	if wt == "" {
		wt = DefaultGPUWorkloadType
	}
	d := w.Row
	k := w.Key
	batch.Queue(`
			INSERT INTO gpu_container_digests (
				interval_start, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
				gpu_model_name, gpu_profile_name, node_name,
				fb_usage_min_mib, fb_usage_max_mib, fb_usage_avg_mib,
				tensor_pipe_active_min, tensor_pipe_active_max, tensor_pipe_active_avg,
				dram_active_min, dram_active_max, dram_active_avg,
				sm_active_min, sm_active_max, sm_active_avg,
				gpu_count, schedule_type
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, gpu_model_name, interval_start, schedule_type)
			DO UPDATE SET
				workload_type = EXCLUDED.workload_type,
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
				sm_active_avg = EXCLUDED.sm_active_avg,
				gpu_count = EXCLUDED.gpu_count`,
		d.IntervalStart, orgID, clusterUUID, k.Namespace, k.Workload, wt, k.ContainerName,
		d.GPUModelName, nullableString(d.GPUProfileName), d.NodeName,
		d.FBUsageMinMiB, d.FBUsageMaxMiB, d.FBUsageAvgMiB,
		d.TensorPipeActiveMin, d.TensorPipeActiveMax, d.TensorPipeActiveAvg,
		d.DRAMActiveMin, d.DRAMActiveMax, d.DRAMActiveAvg,
		d.SMActiveMin, d.SMActiveMax, d.SMActiveAvg,
		d.GPUCount, scheduleType,
	)
}
