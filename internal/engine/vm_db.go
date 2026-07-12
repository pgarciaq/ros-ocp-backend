package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// QueryDailyVMDigests returns VM daily digests for a cluster since the given date.
func QueryDailyVMDigests(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID, since time.Time) ([]model.DailyVMDigest, error) {
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
		ORDER BY vm_name, namespace, bucket_date`,
		orgID, clusterUUID, since.Format("2006-01-02"),
	)
	if err != nil {
		return nil, fmt.Errorf("query VM digests: %w", err)
	}
	defer rows.Close()

	var result []model.DailyVMDigest
	for rows.Next() {
		var d model.DailyVMDigest
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
	return result, nil
}

// PersistVMRecommendations upserts VM recommendations and removes stale terms.
func PersistVMRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []model.VMRecommendation, validTerms []string) error {
	if len(recs) == 0 {
		return nil
	}

	t0 := time.Now()
	defer func() { metrics.ObserveDB("persist_vm_recommendations", t0) }()

	orgID := recs[0].OrgID
	clusterUUID := recs[0].ClusterUUID

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for chunkStart := 0; chunkStart < len(recs); chunkStart += maxPgxBatchQueue {
		chunkEnd := min(chunkStart+maxPgxBatchQueue, len(recs))
		batch := &pgx.Batch{}
		for _, r := range recs[chunkStart:chunkEnd] {
			batch.Queue(`
			INSERT INTO vm_recommendations (
				org_id, cluster_uuid, vm_name, namespace, guest_os,
				current_vcpu, current_memory_gib, current_disk_gib, current_instance_type,
				recommended_vcpu, recommended_memory_gib, recommended_disk_gib,
				recommended_instance_type, recommended_series,
				guest_agent_detected, confidence, term, engine,
				is_idle, is_abandoned, is_power_off_candidate, power_off_idle_ratio,
				is_oversized, is_network_bound,
				is_redundant_placement, has_shared_storage, numa_oversized,
				io_read_iops_p95, io_write_iops_p95, io_read_bps_p95, io_write_bps_p95, io_hint, io_pattern,
				disk_days_until_full, disk_growth_gib_per_day, disk_recommended_expand_gib,
				notifications,
				gpu_count, gpu_model, gpu_classification, recommended_gpu_action,
				recommended_gpu_profile, recommended_time_slice_count,
				gpu_timeslice_confidence, gpu_timeslice_rationale, recommended_vgpu_profile,
				gpu_utilization_avg_bp,
				estimated_savings_cents, savings_currency,
				last_recommended_at, updated_at,`+vmExplSQLColumns+`
			) VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8, $9,
				$10, $11, $12,
				$13, $14,
				$15, $16, $17, $18,
				$19, $20, $21, $22, $23, $24, $25, $26, $27,
				$28, $29, $30, $31, $32, $33,
				$34, $35, $36,
				$37,
				$38, $39, $40, $41, $42, $43, $44, $45, $46, $47,
				$48, $49, $50, now(), $51, $52, $53, $54, $55, $56, $57, $58, $59, $60, $61, $62, $63, $64, $65
			)
			ON CONFLICT (org_id, cluster_uuid, vm_name, namespace, term, engine) DO UPDATE SET
				guest_os = EXCLUDED.guest_os,
				current_vcpu = EXCLUDED.current_vcpu,
				current_memory_gib = EXCLUDED.current_memory_gib,
				current_disk_gib = EXCLUDED.current_disk_gib,
				current_instance_type = EXCLUDED.current_instance_type,
				recommended_vcpu = EXCLUDED.recommended_vcpu,
				recommended_memory_gib = EXCLUDED.recommended_memory_gib,
				recommended_disk_gib = EXCLUDED.recommended_disk_gib,
				recommended_instance_type = EXCLUDED.recommended_instance_type,
				recommended_series = EXCLUDED.recommended_series,
				guest_agent_detected = EXCLUDED.guest_agent_detected,
				confidence = EXCLUDED.confidence,
				is_idle = EXCLUDED.is_idle,
				is_abandoned = EXCLUDED.is_abandoned,
				is_power_off_candidate = EXCLUDED.is_power_off_candidate,
				power_off_idle_ratio = EXCLUDED.power_off_idle_ratio,
				is_oversized = EXCLUDED.is_oversized,
				is_network_bound = EXCLUDED.is_network_bound,
				is_redundant_placement = EXCLUDED.is_redundant_placement,
				has_shared_storage = EXCLUDED.has_shared_storage,
				numa_oversized = EXCLUDED.numa_oversized,
				io_read_iops_p95 = EXCLUDED.io_read_iops_p95,
				io_write_iops_p95 = EXCLUDED.io_write_iops_p95,
				io_read_bps_p95 = EXCLUDED.io_read_bps_p95,
				io_write_bps_p95 = EXCLUDED.io_write_bps_p95,
				io_hint = EXCLUDED.io_hint,
				io_pattern = EXCLUDED.io_pattern,
				disk_days_until_full = EXCLUDED.disk_days_until_full,
				disk_growth_gib_per_day = EXCLUDED.disk_growth_gib_per_day,
				disk_recommended_expand_gib = EXCLUDED.disk_recommended_expand_gib,
				notifications = EXCLUDED.notifications,
				gpu_count = EXCLUDED.gpu_count,
				gpu_model = EXCLUDED.gpu_model,
				gpu_classification = EXCLUDED.gpu_classification,
				recommended_gpu_action = EXCLUDED.recommended_gpu_action,
				recommended_gpu_profile = EXCLUDED.recommended_gpu_profile,
				recommended_time_slice_count = EXCLUDED.recommended_time_slice_count,
				gpu_timeslice_confidence = EXCLUDED.gpu_timeslice_confidence,
				gpu_timeslice_rationale = EXCLUDED.gpu_timeslice_rationale,
				recommended_vgpu_profile = EXCLUDED.recommended_vgpu_profile,
				gpu_utilization_avg_bp = EXCLUDED.gpu_utilization_avg_bp,
				estimated_savings_cents = EXCLUDED.estimated_savings_cents,
				savings_currency = EXCLUDED.savings_currency,
				last_recommended_at = EXCLUDED.last_recommended_at,
				updated_at = now(),`+vmExplUpdateSet,
				append([]any{
					r.OrgID, r.ClusterUUID, r.VMName, r.Namespace, r.GuestOS,
					r.CurrentVCPU, r.CurrentMemoryGiB, r.CurrentDiskGiB, r.CurrentInstanceType,
					r.RecommendedVCPU, r.RecommendedMemoryGiB, r.RecommendedDiskGiB,
					r.RecommendedInstanceType, r.RecommendedSeries,
					r.GuestAgentDetected, r.Confidence, r.Term, r.Engine,
					r.IsIdle, r.IsAbandoned, r.IsPowerOffCandidate, r.PowerOffIdleRatio,
					r.IsOversized, r.IsNetworkBound,
					r.IsRedundantPlacement, r.HasSharedStorage, r.NUMAOversized,
					r.IOReadIOPSP95, r.IOWriteIOPSP95, r.IOReadBPS95, r.IOWriteBPS95, r.IOHint, r.IOPattern,
					r.DiskDaysUntilFull, r.DiskGrowthGiBPerDay, r.DiskRecommendedExpandGiB,
					r.Notifications,
					r.GPUCount, r.GPUModel, r.GPUClassification, r.RecommendedGPUAction,
					r.RecommendedGPUProfile, r.RecommendedTimeSliceCount,
					r.GPUTimeSliceConfidence, r.GPUTimeSliceRationale, r.RecommendedVGPUProfile,
					r.GPUUtilizationAvgBP,
					r.EstimatedSavingsCents, r.SavingsCurrency,
					r.LastRecommendedAt,
				}, appendVMExplArgs(nil, vmExplFromRecommendation(r))...)...,
			)
		}
		if err := flushRecommendationBatch(ctx, tx, batch, chunkEnd-chunkStart); err != nil {
			return fmt.Errorf("batch VM recs chunk %d: %w", chunkStart, err)
		}
	}

	if len(validTerms) > 0 {
		_, err = tx.Exec(ctx, `
			DELETE FROM vm_recommendations
			WHERE org_id = $1 AND cluster_uuid = $2
			  AND term != ALL($3)`,
			orgID, clusterUUID, validTerms,
		)
		if err != nil {
			return fmt.Errorf("cleanup stale VM terms: %w", err)
		}
	}

	if err := AppendVMRecommendationHistory(ctx, tx, recs); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit VM recs: %w", err)
	}

	if err := PruneVMRecommendationHistory(ctx, pool); err != nil {
		logging.ForOrg(orgID, clusterUUID.String()).Warnf(
			"PruneVMRecommendationHistory failed (non-fatal): %v", err,
		)
	}

	logging.ForOrg(orgID, clusterUUID.String()).Infof("PersistVMRecommendations: upserted %d recs", len(recs))
	return nil
}
