package pgrec

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// WriteRecommendations batch-upserts ContainerRec rows into recommendation_sets.
func WriteRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []types.ContainerRec) error {
	if len(recs) == 0 {
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx for recommendations: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for chunkStart := 0; chunkStart < len(recs); chunkStart += maxPgxBatchQueue {
		chunkEnd := min(chunkStart+maxPgxBatchQueue, len(recs))
		batch := &pgx.Batch{}
		for _, r := range recs[chunkStart:chunkEnd] {
			containerID := NativeContainerID(r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName)
			batch.Queue(`
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			term, engine, container_id,
			rec_cpu_request_millicores, rec_cpu_limit_millicores,
			rec_memory_request_kib, rec_memory_limit_kib,
			current_cpu_request_millicores, current_cpu_limit_millicores,
			current_memory_request_kib, current_memory_limit_kib,
			variation_cpu_request_pct, variation_cpu_limit_pct,
			variation_memory_request_pct, variation_memory_limit_pct,
			notification_codes, confidence_level, stale,
			pod_count_min, pod_count_max, pod_count_avg,
			desired_replicas, available_replicas,
			recommended_replicas, replica_confidence, replica_explanation,
			estimated_savings_cents,
			estimated_cpu_savings_cents, estimated_memory_savings_cents,
			idle_state, idle_since, idle_duration_days,
			estimated_waste_cents, peak_cpu_millicores, peak_memory_bytes,
			monitoring_start_time, monitoring_end_time,
			category, category_cpu, category_memory,`+types.ContainerExplSQLColumns+`,
			updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44,$45,$46,`+types.ContainerExplValuePlaceholders(47)+`,now())
		ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine)
		DO UPDATE SET
			rec_cpu_request_millicores = EXCLUDED.rec_cpu_request_millicores,
			rec_cpu_limit_millicores = EXCLUDED.rec_cpu_limit_millicores,
			rec_memory_request_kib = EXCLUDED.rec_memory_request_kib,
			rec_memory_limit_kib = EXCLUDED.rec_memory_limit_kib,
			current_cpu_request_millicores = EXCLUDED.current_cpu_request_millicores,
			current_cpu_limit_millicores = EXCLUDED.current_cpu_limit_millicores,
			current_memory_request_kib = EXCLUDED.current_memory_request_kib,
			current_memory_limit_kib = EXCLUDED.current_memory_limit_kib,
			variation_cpu_request_pct = EXCLUDED.variation_cpu_request_pct,
			variation_cpu_limit_pct = EXCLUDED.variation_cpu_limit_pct,
			variation_memory_request_pct = EXCLUDED.variation_memory_request_pct,
			variation_memory_limit_pct = EXCLUDED.variation_memory_limit_pct,
			notification_codes = EXCLUDED.notification_codes,
			confidence_level = EXCLUDED.confidence_level,
			stale = EXCLUDED.stale,
			pod_count_min = EXCLUDED.pod_count_min,
			pod_count_max = EXCLUDED.pod_count_max,
			pod_count_avg = EXCLUDED.pod_count_avg,
			desired_replicas = EXCLUDED.desired_replicas,
			available_replicas = EXCLUDED.available_replicas,
			recommended_replicas = EXCLUDED.recommended_replicas,
			replica_confidence = EXCLUDED.replica_confidence,
			replica_explanation = EXCLUDED.replica_explanation,
			estimated_savings_cents = EXCLUDED.estimated_savings_cents,
			estimated_cpu_savings_cents = EXCLUDED.estimated_cpu_savings_cents,
			estimated_memory_savings_cents = EXCLUDED.estimated_memory_savings_cents,
			idle_state = EXCLUDED.idle_state,
			idle_since = EXCLUDED.idle_since,
			idle_duration_days = EXCLUDED.idle_duration_days,
			estimated_waste_cents = EXCLUDED.estimated_waste_cents,
			peak_cpu_millicores = EXCLUDED.peak_cpu_millicores,
			peak_memory_bytes = EXCLUDED.peak_memory_bytes,
			monitoring_start_time = EXCLUDED.monitoring_start_time,
			monitoring_end_time = EXCLUDED.monitoring_end_time,
			container_id = EXCLUDED.container_id,
			category = EXCLUDED.category,
			category_cpu = EXCLUDED.category_cpu,
			category_memory = EXCLUDED.category_memory,`+types.ContainerExplUpdateSet+`,
			updated_at = now()`,
				types.AppendContainerExplArgs([]any{
					r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName,
					r.Term, r.Engine, containerID,
					r.RecCPURequestMC, r.RecCPULimitMC,
					r.RecMemRequestKiB, r.RecMemLimitKiB,
					r.CurrentCPURequestMC, r.CurrentCPULimitMC,
					r.CurrentMemRequestKiB, r.CurrentMemLimitKiB,
					r.VariationCPURequestPct, r.VariationCPULimitPct,
					r.VariationMemRequestPct, r.VariationMemLimitPct,
					r.NotificationCodes, r.ConfidenceLevel, r.Stale,
					r.PodCountMin, r.PodCountMax, r.PodCountAvg,
					r.DesiredReplicas, r.AvailableReplicas,
					types.NullIfZeroInt64(r.RecommendedReplicas), types.NullIfEmpty(r.ReplicaConfidence), types.NullIfEmpty(r.ReplicaExplanation),
					r.EstimatedSavingsCents,
					r.EstimatedCPUSavingsCents, r.EstimatedMemSavingsCents,
					types.IdleStateForWrite(r.IdleState), r.IdleSince, r.IdleDurationDays,
					r.EstimatedWasteCents, r.PeakCPUMC, r.PeakMemoryBytes,
					r.MonitoringStartTime, r.MonitoringEndTime,
					types.NullIfEmpty(r.Category), types.NullIfEmpty(r.CategoryCPU), types.NullIfEmpty(r.CategoryMemory),
				}, r.Expl)...,
			)
		}
		if err := flushBatch(ctx, tx, batch); err != nil {
			return fmt.Errorf("batch exec: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit recommendations tx: %w", err)
	}
	return nil
}

// WriteRecommendationsAndRefreshOrg persists recommendations and refreshes org metadata.
func WriteRecommendationsAndRefreshOrg(ctx context.Context, pool *pgxpool.Pool, recs []types.ContainerRec) error {
	if err := WriteRecommendations(ctx, pool, recs); err != nil {
		return err
	}
	if len(recs) == 0 {
		return nil
	}
	return RefreshOrgMetadata(ctx, pool, recs[0].OrgID)
}

// MarkUnreportedContainersStale marks recommendation_sets rows stale when they
// were not refreshed in this cycle (updated_at older than cycleStart minus grace).
func MarkUnreportedContainersStale(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, cycleStart time.Time) (int64, error) {
	grace := cycleStart.Add(-5 * time.Minute)
	tag, err := pool.Exec(ctx, `
		UPDATE recommendation_sets
		SET stale = true, updated_at = now()
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND stale = false
		  AND updated_at < $3`,
		orgID, clusterUUID, grace,
	)
	if err != nil {
		return 0, fmt.Errorf("mark unreported containers stale: %w", err)
	}
	return tag.RowsAffected(), nil
}
