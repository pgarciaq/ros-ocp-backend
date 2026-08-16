package pgdigest

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
)

// WriteVMDigests upserts already-computed VM daily rows (heap table, no partitions)
// then replaces vm_gpu_device_digests for each parent. Empty slice is a no-op.
// orgID and clusterUUID are stamped from caller identity.
func WriteVMDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, rows []vm.DailyVMDigest) error {
	if len(rows) == 0 {
		return nil
	}
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return err
	}
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b vm.DailyVMDigest) int {
		if c := cmp.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		if c := cmp.Compare(a.VMName, b.VMName); c != 0 {
			return c
		}
		return a.BucketDate.Compare(b.BucketDate)
	})
	return withWriteTx(ctx, pool, func(tx pgx.Tx) error {
		for _, d := range sorted {
			id, err := upsertVMDigest(ctx, tx, orgID, clusterUUID, d)
			if err != nil {
				return err
			}
			if err := replaceVMGPUDevices(ctx, tx, id, d.Devices); err != nil {
				return fmt.Errorf("replace VM GPU devices %s/%s: %w", d.Namespace, d.VMName, err)
			}
		}
		return nil
	})
}

func upsertVMDigest(ctx context.Context, tx pgx.Tx, orgID, clusterUUID string, d vm.DailyVMDigest) (int64, error) {
	var digestID int64
	err := tx.QueryRow(ctx, `
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
				net_throughput_p95_bps, net_pps_p95, net_drop_ratio_max_bp
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
				$43, $44, $45
			)
			ON CONFLICT (org_id, cluster_uuid, vm_name, namespace, bucket_date)
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
	).Scan(&digestID)
	if err != nil {
		return 0, fmt.Errorf("upsert VM digest %s/%s: %w", d.Namespace, d.VMName, err)
	}
	return digestID, nil
}

func replaceVMGPUDevices(ctx context.Context, tx pgx.Tx, vmDigestID int64, devices []vm.GPUDeviceDigest) error {
	if _, err := tx.Exec(ctx, `DELETE FROM vm_gpu_device_digests WHERE vm_digest_id = $1`, vmDigestID); err != nil {
		return fmt.Errorf("delete GPU devices: %w", err)
	}
	for _, dev := range devices {
		if _, err := tx.Exec(ctx, `
			INSERT INTO vm_gpu_device_digests (
				vm_digest_id, gpu_uuid, gpu_model,
				util_avg_bp, util_max_bp,
				fb_used_avg_mib, fb_used_max_mib,
				sm_active_avg_bp, tensor_avg_bp, dram_avg_bp,
				mig_profile, max_slices
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			vmDigestID, dev.UUID, dev.Model,
			dev.UtilAvgBP, dev.UtilMaxBP,
			dev.FBUsedAvgMiB, dev.FBUsedMaxMiB,
			dev.SMActiveAvgBP, dev.TensorAvgBP, dev.DRAMAvgBP,
			dev.MIGProfile, dev.MaxSlices,
		); err != nil {
			return fmt.Errorf("insert GPU device %s: %w", dev.UUID, err)
		}
	}
	return nil
}
