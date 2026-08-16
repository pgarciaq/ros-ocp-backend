package pgrec

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// WriteNamespaceRecommendations batch-upserts NamespaceRec results into
// namespace_recommendation_sets using the native relational columns.
func WriteNamespaceRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []namespace.NamespaceRec) error {
	if len(recs) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, r := range recs {
		namespaceID := NativeNamespaceID(r.ClusterUUID, r.Namespace)
		scheduleType := r.ScheduleType
		if scheduleType == "" {
			scheduleType = namespace.ScheduleAllHours
		}
		batch.Queue(`
			INSERT INTO namespace_recommendation_sets (
				org_id, cluster_uuid, namespace_name,
				term, engine, namespace_id, schedule_type,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				current_cpu_request_millicores, current_cpu_limit_millicores,
				current_memory_request_kib, current_memory_limit_kib,
				variation_cpu_request_pct, variation_cpu_limit_pct,
				variation_memory_request_pct, variation_memory_limit_pct,
				notification_codes, confidence_level, stale,
				monitoring_start_time, monitoring_end_time,
				estimated_savings_cents, estimated_cpu_savings_cents, estimated_memory_savings_cents,
				category, category_cpu, category_memory,`+types.ContainerExplSQLColumns+`, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7::digest_schedule_type,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,`+types.ContainerExplValuePlaceholders(31)+`, now())
			ON CONFLICT (org_id, cluster_uuid, namespace_name, term, engine, schedule_type)
			  WHERE term IS NOT NULL
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
				namespace_id = EXCLUDED.namespace_id,
				monitoring_start_time = EXCLUDED.monitoring_start_time,
				monitoring_end_time = EXCLUDED.monitoring_end_time,
				estimated_savings_cents = EXCLUDED.estimated_savings_cents,
				estimated_cpu_savings_cents = EXCLUDED.estimated_cpu_savings_cents,
				estimated_memory_savings_cents = EXCLUDED.estimated_memory_savings_cents,
				category = EXCLUDED.category,
				category_cpu = EXCLUDED.category_cpu,
				category_memory = EXCLUDED.category_memory,`+types.ContainerExplUpdateSet+`,
				updated_at = now()`,
			types.AppendContainerExplArgs([]any{
				r.OrgID, r.ClusterUUID, r.Namespace,
				r.Term, r.Engine, namespaceID, scheduleType,
				r.RecCPURequestMC, r.RecCPULimitMC,
				r.RecMemRequestKiB, r.RecMemLimitKiB,
				r.CurrentCPURequestMC, r.CurrentCPULimitMC,
				r.CurrentMemRequestKiB, r.CurrentMemLimitKiB,
				r.VariationCPURequestPct, r.VariationCPULimitPct,
				r.VariationMemRequestPct, r.VariationMemLimitPct,
				r.NotificationCodes, r.ConfidenceLevel, r.Stale,
				r.MonitoringStartTime, r.MonitoringEndTime,
				r.EstimatedSavingsCents, r.EstimatedCPUSavingsCents, r.EstimatedMemSavingsCents,
				types.NullIfEmpty(r.Category), types.NullIfEmpty(r.CategoryCPU), types.NullIfEmpty(r.CategoryMemory),
			}, r.Expl)...,
		)
	}

	if err := flushBatch(ctx, pool, batch); err != nil {
		return fmt.Errorf("namespace rec batch exec: %w", err)
	}
	return nil
}
