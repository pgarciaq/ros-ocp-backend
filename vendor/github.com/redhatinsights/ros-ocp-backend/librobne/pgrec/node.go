package pgrec

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// NodeRecsAdvisoryLock is the pg_advisory_xact_lock key shared with migration
// 000058 (PK rebuild) to prevent deadlocks without requiring worker shutdown.
const NodeRecsAdvisoryLock = 7358001

// WriteNodeRecommendations upserts node recommendations and deletes rows for
// terms no longer in validTerms (product SQL, extracted as-is).
func WriteNodeRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, recs []node.Rec, validTerms []string) error {
	if len(recs) == 0 {
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", NodeRecsAdvisoryLock)); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}

	for chunkStart := 0; chunkStart < len(recs); chunkStart += maxPgxBatchQueue {
		chunkEnd := min(chunkStart+maxPgxBatchQueue, len(recs))
		batch := &pgx.Batch{}
		for _, r := range recs[chunkStart:chunkEnd] {
			recommendedCPUCores := float64(r.RecommendedCPUMC) / 1000.0
			recommendedMemGiB := float64(r.RecommendedMemKiB) / (1024.0 * 1024.0)
			batch.Queue(`
			INSERT INTO node_recommendations (
				org_id, cluster_uuid, node, term, engine,
				cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
				cpu_overcommit_ratio, category, idle_state,
				stranded_resource, pod_count, pod_capacity, machineset_name, trend_slope, notification_codes,
				recommended_cpu_cores, recommended_memory_gib, node_count_reduction,
				estimated_savings_cents, instance_type,
				suggested_instance_type, instance_type_reason,
				confidence_level, data_days,
				expl_data_days, expl_target_utilization_bp,
				expl_current_cpu_mc, expl_current_mem_kib,
				expl_max_cpu_usage_p95_mc, expl_max_mem_usage_p95_kib,
				expl_pod_scheduling_headroom_bp, expl_ema_imbalance_bp,
				expl_consolidation_applied, expl_sizing_formula,
				updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,now())
			ON CONFLICT (org_id, cluster_uuid, node, term, engine) DO UPDATE SET
				cpu_util_p50 = EXCLUDED.cpu_util_p50,
				cpu_util_p95 = EXCLUDED.cpu_util_p95,
				mem_util_p50 = EXCLUDED.mem_util_p50,
				mem_util_p95 = EXCLUDED.mem_util_p95,
				cpu_overcommit_ratio = EXCLUDED.cpu_overcommit_ratio,
				category = EXCLUDED.category,
				idle_state = EXCLUDED.idle_state,
				stranded_resource = EXCLUDED.stranded_resource,
				pod_count = EXCLUDED.pod_count,
				pod_capacity = EXCLUDED.pod_capacity,
				machineset_name = EXCLUDED.machineset_name,
				trend_slope = EXCLUDED.trend_slope,
				notification_codes = EXCLUDED.notification_codes,
				recommended_cpu_cores = EXCLUDED.recommended_cpu_cores,
				recommended_memory_gib = EXCLUDED.recommended_memory_gib,
				node_count_reduction = EXCLUDED.node_count_reduction,
				estimated_savings_cents = EXCLUDED.estimated_savings_cents,
				instance_type = EXCLUDED.instance_type,
				suggested_instance_type = EXCLUDED.suggested_instance_type,
				instance_type_reason = EXCLUDED.instance_type_reason,
				confidence_level = EXCLUDED.confidence_level,
				data_days = EXCLUDED.data_days,
				expl_data_days = EXCLUDED.expl_data_days,
				expl_target_utilization_bp = EXCLUDED.expl_target_utilization_bp,
				expl_current_cpu_mc = EXCLUDED.expl_current_cpu_mc,
				expl_current_mem_kib = EXCLUDED.expl_current_mem_kib,
				expl_max_cpu_usage_p95_mc = EXCLUDED.expl_max_cpu_usage_p95_mc,
				expl_max_mem_usage_p95_kib = EXCLUDED.expl_max_mem_usage_p95_kib,
				expl_pod_scheduling_headroom_bp = EXCLUDED.expl_pod_scheduling_headroom_bp,
				expl_ema_imbalance_bp = EXCLUDED.expl_ema_imbalance_bp,
				expl_consolidation_applied = EXCLUDED.expl_consolidation_applied,
				expl_sizing_formula = EXCLUDED.expl_sizing_formula,
				updated_at = now()`,
				orgID, clusterUUID, r.Node, r.Term, r.Engine,
				r.CPUUtilP50, r.CPUUtilP95, r.MemUtilP50, r.MemUtilP95,
				r.CPUOvercommitRatio, r.Category, types.IdleStateForWrite(r.IdleState),
				r.StrandedResource, r.PodCount, nullInt64PodCapacity(r.PodCapacity), nullableString(r.MachineSetName), r.TrendSlope, r.NotificationCodes,
				recommendedCPUCores, recommendedMemGiB, r.NodeCountReduction,
				r.EstimatedMonthlySavingsCents, r.InstanceType,
				nullableString(r.SuggestedInstanceType), nullableString(r.InstanceTypeReason),
				r.ConfidenceLevel, r.DataDays,
				types.NullIntExpl(r.Expl.DataDays),
				types.NullInt32Expl(r.Expl.TargetUtilizationBP),
				types.NullInt64Expl(r.Expl.CurrentCPUMC),
				types.NullInt64Expl(r.Expl.CurrentMemKiB),
				types.NullInt64Expl(r.Expl.MaxCPUUsageP95MC),
				types.NullInt64Expl(r.Expl.MaxMemUsageP95KiB),
				types.NullInt32Expl(r.Expl.PodSchedulingHeadroomBP),
				types.NullInt32Expl(r.Expl.EMAImbalanceBP),
				r.Expl.ConsolidationApplied,
				types.NullStringExpl(r.Expl.SizingFormula),
			)
		}
		if err := flushBatch(ctx, tx, batch); err != nil {
			return fmt.Errorf("batch node recs chunk %d: %w", chunkStart, err)
		}
	}

	if len(validTerms) > 0 {
		_, err = tx.Exec(ctx, `
			DELETE FROM node_recommendations
			WHERE org_id = $1 AND cluster_uuid = $2
			  AND term != ALL($3)`,
			orgID, clusterUUID, validTerms,
		)
		if err != nil {
			return fmt.Errorf("cleanup stale terms: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit node recs: %w", err)
	}
	return nil
}
