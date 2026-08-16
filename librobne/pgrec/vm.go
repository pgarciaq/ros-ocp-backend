package pgrec

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
)

// WriteVMRecommendations upserts VM recommendations, removes stale terms, and
// appends history snapshots. Prune stays in the processor (uses product config).
func WriteVMRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []vm.VMRecommendation, validTerms []string) error {
	if len(recs) == 0 {
		return nil
	}

	orgID := recs[0].OrgID
	clusterUUID := recs[0].ClusterUUID

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

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
				category, power_off_idle_ratio, is_network_bound,
				is_redundant_placement, has_shared_storage, numa_oversized,
				io_read_iops_p95, io_write_iops_p95, io_read_bps_p95, io_write_bps_p95, io_hint, io_pattern,
				disk_days_until_full, disk_growth_gib_per_day, disk_recommended_expand_gib,
				notifications,
				gpu_count, gpu_model, gpu_classification, recommended_gpu_action,
				recommended_gpu_profile, recommended_time_slice_count,
				gpu_timeslice_confidence, gpu_timeslice_rationale, recommended_vgpu_profile,
				gpu_utilization_avg_bp,
				estimated_savings_cents, savings_currency,
				last_recommended_at, updated_at,`+types.VMExplSQLColumns+`
			) VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8, $9,
				$10, $11, $12,
				$13, $14,
				$15, $16, $17, $18,
				$19, $20, $21,
				$22, $23, $24,
				$25, $26, $27, $28, $29, $30,
				$31, $32, $33,
				$34,
				$35, $36, $37, $38, $39, $40, $41, $42, $43, $44,
				$45, $46, $47, now(), $48, $49, $50, $51, $52, $53, $54, $55, $56, $57, $58, $59, $60, $61, $62
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
				category = EXCLUDED.category,
				power_off_idle_ratio = EXCLUDED.power_off_idle_ratio,
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
				updated_at = now(),`+types.VMExplUpdateSet,
				append([]any{
					r.OrgID, r.ClusterUUID, r.VMName, r.Namespace, r.GuestOS,
					r.CurrentVCPU, r.CurrentMemoryGiB, r.CurrentDiskGiB, r.CurrentInstanceType,
					r.RecommendedVCPU, r.RecommendedMemoryGiB, r.RecommendedDiskGiB,
					r.RecommendedInstanceType, r.RecommendedSeries,
					r.GuestAgentDetected, r.Confidence, r.Term, r.Engine,
					r.Category, r.PowerOffIdleRatio, r.IsNetworkBound,
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
				}, types.AppendVMExplArgs(nil, vm.VMExplFromRecommendation(r))...)...,
			)
		}
		if err := flushBatch(ctx, tx, batch); err != nil {
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
	return nil
}

// AppendVMRecommendationHistory inserts history snapshots inside the caller's transaction.
func AppendVMRecommendationHistory(ctx context.Context, tx pgx.Tx, recs []vm.VMRecommendation) error {
	if len(recs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, r := range recs {
		instType := ""
		if r.RecommendedInstanceType != nil {
			instType = *r.RecommendedInstanceType
		}
		batch.Queue(`
			INSERT INTO vm_recommendation_history (
				org_id, cluster_id, vm_name, namespace, term, engine,
				recommended_vcpu, recommended_memory_gib, recommended_instance_type,
				gpu_classification, recommended_gpu_action,
				category, confidence,`+types.VMExplSQLColumns+`
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28)`,
			append([]any{
				r.OrgID, r.ClusterUUID.String(), r.VMName, r.Namespace, r.Term, r.Engine,
				r.RecommendedVCPU, float64(r.RecommendedMemoryGiB), instType,
				r.GPUClassification, r.RecommendedGPUAction,
				r.Category, r.Confidence,
			}, types.AppendVMExplArgs(nil, vm.VMExplFromRecommendation(r))...)...,
		)
	}
	if err := flushBatch(ctx, tx, batch); err != nil {
		return fmt.Errorf("insert VM rec history: %w", err)
	}
	return nil
}
