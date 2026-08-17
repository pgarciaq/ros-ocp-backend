package pgdigest

import (
	"context"
	"fmt"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
)

// ReadGPUContainerDigests loads all_hours GPU container days whose interval_start
// falls in [start, end] (end is inclusive as a calendar day). Unique key has no org_id.
// Empty result is not an error.
func ReadGPUContainerDigests(ctx context.Context, q Querier, clusterUUID string, start, end time.Time) (map[gpu.GPUContainerKey][]gpu.GPUDigestRow, error) {
	return ReadGPUContainerDigestsWithSchedule(ctx, q, clusterUUID, start, end, ScheduleAllHours)
}

// ReadGPUContainerDigestsWithSchedule loads GPU container days for one digest_schedule_type.
func ReadGPUContainerDigestsWithSchedule(ctx context.Context, q Querier, clusterUUID string, start, end time.Time, scheduleType string) (map[gpu.GPUContainerKey][]gpu.GPUDigestRow, error) {
	if err := requireCluster(clusterUUID); err != nil {
		return nil, err
	}
	if scheduleType == "" {
		return nil, fmt.Errorf("pgdigest: schedule_type is required")
	}
	if err := requireQuerier(q); err != nil {
		return nil, err
	}
	endExclusive := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	rows, err := q.Query(ctx, `
		SELECT interval_start, namespace, workload, container_name,
			gpu_model_name, gpu_profile_name, COALESCE(node_name, ''),
			COALESCE(fb_usage_min_mib, 0), COALESCE(fb_usage_max_mib, 0), COALESCE(fb_usage_avg_mib, 0),
			COALESCE(tensor_pipe_active_min, 0), COALESCE(tensor_pipe_active_max, 0), COALESCE(tensor_pipe_active_avg, 0),
			COALESCE(dram_active_min, 0), COALESCE(dram_active_max, 0), COALESCE(dram_active_avg, 0),
			COALESCE(sm_active_min, 0), COALESCE(sm_active_max, 0), COALESCE(sm_active_avg, 0),
			COALESCE(gpu_count, 1)
		FROM gpu_container_digests
		WHERE cluster_uuid = $1
		  AND interval_start >= $2 AND interval_start < $3
		  AND schedule_type = $4
		ORDER BY namespace, workload, container_name, gpu_model_name, interval_start`,
		clusterUUID, startDay, endExclusive, scheduleType)
	if err != nil {
		return nil, fmt.Errorf("pgdigest: query GPU digests: %w", err)
	}
	defer rows.Close()
	out := make(map[gpu.GPUContainerKey][]gpu.GPUDigestRow)
	for rows.Next() {
		var k gpu.GPUContainerKey
		var d gpu.GPUDigestRow
		var profile *string
		if err := rows.Scan(
			&d.IntervalStart, &k.Namespace, &k.Workload, &k.ContainerName,
			&d.GPUModelName, &profile, &d.NodeName,
			&d.FBUsageMinMiB, &d.FBUsageMaxMiB, &d.FBUsageAvgMiB,
			&d.TensorPipeActiveMin, &d.TensorPipeActiveMax, &d.TensorPipeActiveAvg,
			&d.DRAMActiveMin, &d.DRAMActiveMax, &d.DRAMActiveAvg,
			&d.SMActiveMin, &d.SMActiveMax, &d.SMActiveAvg,
			&d.GPUCount,
		); err != nil {
			return nil, fmt.Errorf("pgdigest: scan GPU digest: %w", err)
		}
		d.GPUProfileName = derefString(profile)
		out[k] = append(out[k], d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgdigest: iterate GPU digests: %w", err)
	}
	return out, nil
}
