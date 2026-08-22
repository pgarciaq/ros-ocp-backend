package vm

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// QueryDailyVMDigests returns VM daily digests for a cluster since the given date.
func QueryDailyVMDigests(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID, since time.Time) ([]Digest, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			id, org_id, cluster_uuid, vm_name, namespace, node_name, guest_os, bucket_date,
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
		FROM daily_vm_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND bucket_date >= $3::date
		  AND schedule_type = 'all_hours'
		ORDER BY vm_name, namespace, bucket_date`,
		orgID, clusterUUID, since.Format("2006-01-02"),
	)
	if err != nil {
		return nil, fmt.Errorf("query VM digests: %w", err)
	}
	defer rows.Close()

	result := make([]Digest, 0, 256)
	for rows.Next() {
		var d Digest
		err := rows.Scan(
			&d.ID, &d.OrgID, &d.ClusterUUID, &d.VMName, &d.Namespace, &d.NodeName, &d.GuestOS, &d.BucketDate,
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
		)
		if err != nil {
			return nil, fmt.Errorf("scan VM digest: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate VM digests: %w", err)
	}
	if err := AttachGPUDevicesToDigests(ctx, pool, result); err != nil {
		return nil, err
	}
	if err := AttachPVCsToDigests(ctx, pool, result); err != nil {
		return nil, err
	}
	return result, nil
}

// PersistVMRecommendations upserts VM recommendations and removes stale terms.
//
// No advisory lock is needed here — unlike PersistNodeRecommendations (which
// uses pg_advisory_xact_lock(nodeRecsAdvisoryLock) to serialize with migration
// 000058's PK rebuild on node_recommendations), no concurrent migration modifies
// the vm_recommendations primary key. If a future migration requires a PK
// rebuild or other DDL on vm_recommendations under concurrent writes, add an
// advisory lock following the pattern in recommend_nodes.go:nodeRecsAdvisoryLock.
func PersistVMRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []Recommendation, validTerms []string) error {
	if len(recs) == 0 {
		return nil
	}

	t0 := time.Now()
	defer func() { metrics.ObserveDB("persist_vm_recommendations", t0) }()

	orgID := recs[0].OrgID
	clusterUUID := recs[0].ClusterUUID

	if err := pgrec.WriteVMRecommendations(ctx, pool, recs, validTerms); err != nil {
		return err
	}

	if err := PruneVMRecommendationHistory(ctx, pool); err != nil {
		logging.ForOrg(orgID, clusterUUID.String()).Warnf(
			"PruneVMRecommendationHistory failed (non-fatal): %v", err,
		)
	}

	logging.ForOrg(orgID, clusterUUID.String()).Infof("PersistVMRecommendations: upserted %d recs", len(recs))
	return nil
}
