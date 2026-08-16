package pgdigest

import (
	"context"
	"fmt"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
)

// ReadVMDigests loads VM daily parents in [start, end] and attaches
// vm_gpu_device_digests. Empty result is not an error. PVC companions are not
// stored by WriteVMDigests.
func ReadVMDigests(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time) ([]vm.DailyVMDigest, error) {
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return nil, err
	}
	if err := requireQuerier(q); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT id, vm_name, namespace, COALESCE(node_name, ''), COALESCE(guest_os, ''), bucket_date,
			COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p95_mc, 0), COALESCE(cpu_usage_p99_mc, 0), COALESCE(cpu_usage_max_mc, 0),
			COALESCE(cpu_request_mc, 0), COALESCE(cpu_limit_mc, 0),
			COALESCE(mem_usage_p50_kib, 0), COALESCE(mem_usage_p95_kib, 0), COALESCE(mem_usage_p99_kib, 0), COALESCE(mem_usage_max_kib, 0),
			COALESCE(mem_request_kib, 0),
			mem_available_p50_kib, mem_available_p95_kib,
			COALESCE(disk_allocated_max_bytes, 0),
			filesystem_used_max_bytes, filesystem_capacity_bytes,
			disk_read_iops_p95, disk_write_iops_p95, disk_read_bps_p95, disk_write_bps_p95,
			COALESCE(sample_count, 0), COALESCE(agent_sample_count, 0), COALESCE(restart_count_sum, 0),
			COALESCE(gpu_count, 0), COALESCE(gpu_model, ''), COALESCE(gpu_util_avg_bp, 0), COALESCE(gpu_util_max_bp, 0),
			COALESCE(gpu_fb_used_avg_mib, 0), COALESCE(gpu_fb_used_max_mib, 0), COALESCE(gpu_sm_active_avg_bp, 0),
			COALESCE(gpu_tensor_avg_bp, 0), COALESCE(gpu_dram_avg_bp, 0), COALESCE(gpu_mig_profile, ''), COALESCE(gpu_max_slices, 0), COALESCE(has_gpu, false),
			COALESCE(net_throughput_p95_bps, 0), COALESCE(net_pps_p95, 0), COALESCE(net_drop_ratio_max_bp, 0)
		FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		ORDER BY namespace, vm_name, bucket_date`,
		orgID, clusterUUID, start.Format(dateLayout), end.Format(dateLayout))
	if err != nil {
		return nil, fmt.Errorf("pgdigest: query VM digests: %w", err)
	}
	defer rows.Close()
	var out []vm.DailyVMDigest
	var ids []int64
	byID := make(map[int64]int)
	for rows.Next() {
		var d vm.DailyVMDigest
		if err := rows.Scan(
			&d.ID, &d.VMName, &d.Namespace, &d.NodeName, &d.GuestOS, &d.BucketDate,
			&d.CPUUsageP50MC, &d.CPUUsageP95MC, &d.CPUUsageP99MC, &d.CPUUsageMaxMC,
			&d.CPURequestMC, &d.CPULimitMC,
			&d.MemUsageP50KiB, &d.MemUsageP95KiB, &d.MemUsageP99KiB, &d.MemUsageMaxKiB,
			&d.MemRequestKiB,
			&d.MemAvailableP50KiB, &d.MemAvailableP95KiB,
			&d.DiskAllocatedMaxBytes,
			&d.FilesystemUsedMaxBytes, &d.FilesystemCapacityBytes,
			&d.DiskReadIOPSP95, &d.DiskWriteIOPSP95, &d.DiskReadBPS95, &d.DiskWriteBPS95,
			&d.SampleCount, &d.AgentSampleCount, &d.RestartCountSum,
			&d.GPUCount, &d.GPUModel, &d.GPUUtilAvgBP, &d.GPUUtilMaxBP,
			&d.GPUFBUsedAvgMiB, &d.GPUFBUsedMaxMiB, &d.GPUSMActiveAvgBP,
			&d.GPUTensorAvgBP, &d.GPUDRAMAvgBP, &d.GPUMIGProfile, &d.GPUMaxSlices, &d.HasGPU,
			&d.NetThroughputP95BPS, &d.NetPPSP95, &d.NetDropRatioMaxBP,
		); err != nil {
			return nil, fmt.Errorf("pgdigest: scan VM digest: %w", err)
		}
		byID[d.ID] = len(out)
		ids = append(ids, d.ID)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgdigest: iterate VM digests: %w", err)
	}
	if len(ids) == 0 {
		return out, nil
	}
	devRows, err := q.Query(ctx, `
		SELECT vm_digest_id, gpu_uuid, COALESCE(gpu_model, ''),
			COALESCE(util_avg_bp, 0), COALESCE(util_max_bp, 0),
			COALESCE(fb_used_avg_mib, 0), COALESCE(fb_used_max_mib, 0),
			COALESCE(sm_active_avg_bp, 0), COALESCE(tensor_avg_bp, 0), COALESCE(dram_avg_bp, 0),
			COALESCE(mig_profile, ''), COALESCE(max_slices, 0)
		FROM vm_gpu_device_digests
		WHERE vm_digest_id = ANY($1)
		ORDER BY vm_digest_id, gpu_uuid`,
		ids)
	if err != nil {
		return nil, fmt.Errorf("pgdigest: query VM GPU devices: %w", err)
	}
	defer devRows.Close()
	for devRows.Next() {
		var id int64
		var dev vm.GPUDeviceDigest
		if err := devRows.Scan(
			&id, &dev.UUID, &dev.Model,
			&dev.UtilAvgBP, &dev.UtilMaxBP,
			&dev.FBUsedAvgMiB, &dev.FBUsedMaxMiB,
			&dev.SMActiveAvgBP, &dev.TensorAvgBP, &dev.DRAMAvgBP,
			&dev.MIGProfile, &dev.MaxSlices,
		); err != nil {
			return nil, fmt.Errorf("pgdigest: scan VM GPU device: %w", err)
		}
		idx, ok := byID[id]
		if !ok {
			continue
		}
		out[idx].Devices = append(out[idx].Devices, dev)
	}
	if err := devRows.Err(); err != nil {
		return nil, fmt.Errorf("pgdigest: iterate VM GPU devices: %w", err)
	}
	return out, nil
}
