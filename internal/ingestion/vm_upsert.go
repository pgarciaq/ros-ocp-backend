package ingestion

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UpsertDailyVMDigests writes VM daily digests and per-GPU device rows as all_hours.
func UpsertDailyVMDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, digests []VMDigestResult) error {
	return UpsertDailyVMDigestsWithSchedule(ctx, pool, orgID, clusterUUID, string(ScheduleTypeAllHours), digests)
}

// UpsertDailyVMDigestsWithSchedule writes VM daily digests for one digest_schedule_type.
func UpsertDailyVMDigestsWithSchedule(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, scheduleType string, digests []VMDigestResult) error {
	if len(digests) == 0 {
		return nil
	}
	if scheduleType == "" {
		return fmt.Errorf("upserting VM digests: schedule_type is required")
	}

	for _, d := range digests {
		var digestID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO daily_vm_digests (
				org_id, cluster_uuid, vm_name, namespace, node_name, guest_os, bucket_date,
				cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
				cpu_request_mc, cpu_limit_mc,
				mem_usage_p50_kib, mem_usage_p95_kib, mem_usage_p99_kib, mem_usage_max_kib,
				mem_request_kib,
				mem_available_p50_kib, mem_available_p95_kib,
				disk_allocated_max_bytes,
				filesystem_used_max_bytes, filesystem_capacity_bytes,
				disk_read_iops_p95, disk_write_iops_p95, disk_read_bps_p95, disk_write_bps_p95,
				sample_count, agent_sample_count, restart_count_sum,
				gpu_count, gpu_model, gpu_util_avg_bp, gpu_util_max_bp,
				gpu_fb_used_avg_mib, gpu_fb_used_max_mib, gpu_sm_active_avg_bp,
				gpu_tensor_avg_bp, gpu_dram_avg_bp, gpu_mig_profile, gpu_max_slices, has_gpu,
				net_throughput_p95_bps, net_pps_p95, net_drop_ratio_max_bp,
				schedule_type
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7,
				$8, $9, $10, $11, $12, $13,
				$14, $15, $16, $17, $18,
				$19, $20,
				$21,
				$22, $23,
				$24, $25, $26, $27,
				$28, $29, $30,
				$31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42,
				$43, $44, $45,
				$46::digest_schedule_type
			)
			ON CONFLICT (org_id, cluster_uuid, vm_name, namespace, bucket_date, schedule_type)
			DO UPDATE SET
				node_name = EXCLUDED.node_name,
				guest_os = EXCLUDED.guest_os,
				cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc,
				cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc,
				cpu_usage_p99_mc = EXCLUDED.cpu_usage_p99_mc,
				cpu_usage_max_mc = EXCLUDED.cpu_usage_max_mc,
				cpu_request_mc = EXCLUDED.cpu_request_mc,
				cpu_limit_mc = EXCLUDED.cpu_limit_mc,
				mem_usage_p50_kib = EXCLUDED.mem_usage_p50_kib,
				mem_usage_p95_kib = EXCLUDED.mem_usage_p95_kib,
				mem_usage_p99_kib = EXCLUDED.mem_usage_p99_kib,
				mem_usage_max_kib = EXCLUDED.mem_usage_max_kib,
				mem_request_kib = EXCLUDED.mem_request_kib,
				mem_available_p50_kib = EXCLUDED.mem_available_p50_kib,
				mem_available_p95_kib = EXCLUDED.mem_available_p95_kib,
				disk_allocated_max_bytes = EXCLUDED.disk_allocated_max_bytes,
				filesystem_used_max_bytes = EXCLUDED.filesystem_used_max_bytes,
				filesystem_capacity_bytes = EXCLUDED.filesystem_capacity_bytes,
				disk_read_iops_p95 = EXCLUDED.disk_read_iops_p95,
				disk_write_iops_p95 = EXCLUDED.disk_write_iops_p95,
				disk_read_bps_p95 = EXCLUDED.disk_read_bps_p95,
				disk_write_bps_p95 = EXCLUDED.disk_write_bps_p95,
				sample_count = EXCLUDED.sample_count,
				agent_sample_count = EXCLUDED.agent_sample_count,
				restart_count_sum = EXCLUDED.restart_count_sum,
				gpu_count = EXCLUDED.gpu_count,
				gpu_model = EXCLUDED.gpu_model,
				gpu_util_avg_bp = EXCLUDED.gpu_util_avg_bp,
				gpu_util_max_bp = EXCLUDED.gpu_util_max_bp,
				gpu_fb_used_avg_mib = EXCLUDED.gpu_fb_used_avg_mib,
				gpu_fb_used_max_mib = EXCLUDED.gpu_fb_used_max_mib,
				gpu_sm_active_avg_bp = EXCLUDED.gpu_sm_active_avg_bp,
				gpu_tensor_avg_bp = EXCLUDED.gpu_tensor_avg_bp,
				gpu_dram_avg_bp = EXCLUDED.gpu_dram_avg_bp,
				gpu_mig_profile = EXCLUDED.gpu_mig_profile,
				gpu_max_slices = EXCLUDED.gpu_max_slices,
				has_gpu = EXCLUDED.has_gpu,
				net_throughput_p95_bps = EXCLUDED.net_throughput_p95_bps,
				net_pps_p95 = EXCLUDED.net_pps_p95,
				net_drop_ratio_max_bp = EXCLUDED.net_drop_ratio_max_bp
			RETURNING id`,
			orgID, clusterUUID, d.VMName, d.Namespace, d.NodeName, d.GuestOS, d.BucketDate,
			d.CPUUsageP50MC, d.CPUUsageP95MC, d.CPUUsageP99MC, d.CPUUsageMaxMC,
			d.CPURequestMC, d.CPULimitMC,
			d.MemUsageP50KiB, d.MemUsageP95KiB, d.MemUsageP99KiB, d.MemUsageMaxKiB,
			d.MemRequestKiB,
			d.MemAvailableP50KiB, d.MemAvailableP95KiB,
			d.DiskAllocatedMaxBytes,
			d.FilesystemUsedMaxBytes, d.FilesystemCapacityBytes,
			d.DiskReadIOPSP95, d.DiskWriteIOPSP95, d.DiskReadBPS95, d.DiskWriteBPS95,
			d.SampleCount, d.AgentSampleCount, d.RestartCountSum,
			d.GPUCount, d.GPUModel, d.GPUUtilAvgBP, d.GPUUtilMaxBP,
			d.GPUFBUsedAvgMiB, d.GPUFBUsedMaxMiB, d.GPUSMActiveAvgBP,
			d.GPUTensorAvgBP, d.GPUDRAMAvgBP, d.GPUMIGProfile, d.GPUMaxSlices, d.HasGPU,
			d.NetThroughputP95BPS, d.NetPPSP95, d.NetDropRatioMaxBP,
			scheduleType,
		).Scan(&digestID)
		if err != nil {
			return fmt.Errorf("upserting VM digest %s/%s: %w", d.Namespace, d.VMName, err)
		}
		if len(d.GPUDevices) > 0 {
			if err := UpsertVMGPUDevices(ctx, pool, digestID, d.GPUDevices); err != nil {
				return err
			}
		}
	}
	return nil
}
