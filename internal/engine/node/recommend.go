package node

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	libnode "github.com/redhatinsights/ros-ocp-backend/librobne/node"
)

// NodeRecsAdvisoryLock is the pg_advisory_xact_lock key shared between
// PersistRecommendations and migration 000058 (PK rebuild) to prevent
// deadlocks without requiring manual worker shutdown during migrations.
const NodeRecsAdvisoryLock = 7358001

// defaultNodeDigestCapacity is the initial slice capacity for QueryNodeDigests results.
const defaultNodeDigestCapacity = 512

// RecommendNodes evaluates node-level utilization signals from daily digest data.
func RecommendNodes(digests []DigestRow, cfg RecConfig, nodeSettings ThresholdSettings, terms []core.TermConfig) []Rec {
	return libnode.RecommendNodes(digests, cfg, nodeSettings, terms)
}

// ResolveAllocatable returns the effective allocatable CPU in millicores.
func ResolveAllocatable(storedAlloc *int64, maxRequests int64, factor float64) int64 {
	return libnode.ResolveAllocatable(storedAlloc, maxRequests, factor)
}

// ResolveAllocatableMem returns the effective allocatable memory in KiB.
func ResolveAllocatableMem(storedAlloc *int64, maxRequests int64, factor float64) int64 {
	return libnode.ResolveAllocatableMem(storedAlloc, maxRequests, factor)
}

// LinearRegressionSlope computes the slope of a simple OLS linear regression.
func LinearRegressionSlope(ys []float64) float64 {
	return libnode.LinearRegressionSlope(ys)
}

// QueryNodeDigests reads daily_node_digests for a cluster within a time range.
func QueryNodeDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, start, end time.Time) ([]DigestRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT bucket_date, node,
			COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p95_mc, 0),
			COALESCE(mem_usage_p50_kib, 0), COALESCE(mem_usage_p95_kib, 0),
			max_cpu_allocatable_mc, max_mem_allocatable_kib,
			COALESCE(max_cpu_requests_mc, 0), COALESCE(max_mem_requests_kib, 0),
			COALESCE(max_pod_count, 0), COALESCE(pod_capacity, 0),
			COALESCE(instance_type, ''), COALESCE(machineset_name, ''),
			COALESCE(sample_count, 0), node_gpu_count
		FROM daily_node_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		ORDER BY node, bucket_date`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	// N.B. filterNodeByWindow uses binary search and relies on bucket_date sort order above.
	if err != nil {
		return nil, fmt.Errorf("query node digests: %w", err)
	}
	defer rows.Close()

	result := make([]DigestRow, 0, defaultNodeDigestCapacity)
	for rows.Next() {
		var d DigestRow
		err := rows.Scan(
			&d.BucketDate, &d.Node,
			&d.CPUUsageP50MC, &d.CPUUsageP95MC,
			&d.MemUsageP50KiB, &d.MemUsageP95KiB,
			&d.MaxCPUAllocMC, &d.MaxMemAllocKiB,
			&d.MaxCPURequestsMC, &d.MaxMemRequestsKiB,
			&d.MaxPodCount, &d.PodCapacity, &d.InstanceType, &d.MachineSetName, &d.SampleCount,
			&d.NodeGPUCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan node digest row: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node digest rows: %w", err)
	}
	return result, nil
}

// PersistRecommendations upserts computed node recommendations into the database.
func PersistRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, recs []Rec, validTerms []string) error {
	if len(recs) == 0 {
		return nil
	}

	t0 := time.Now()
	defer func() { metrics.ObserveDB("persist_node_recommendations", t0) }()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Advisory lock serializes with migration 000058 (PK rebuild).
	// If the migration is running, this blocks until it completes rather than deadlocking.
	if _, err := tx.Exec(ctx, fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", NodeRecsAdvisoryLock)); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}

	for chunkStart := 0; chunkStart < len(recs); chunkStart += db.MaxPgxBatchQueue {
		chunkEnd := min(chunkStart+db.MaxPgxBatchQueue, len(recs))
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
				r.CPUOvercommitRatio, r.Category, core.IdleStateForWrite(r.IdleState),
				r.StrandedResource, r.PodCount, nullInt64PodCapacity(r.PodCapacity), nullStringMachineSet(r.MachineSetName), r.TrendSlope, r.NotificationCodes,
				recommendedCPUCores, recommendedMemGiB, r.NodeCountReduction,
				r.EstimatedMonthlySavingsCents, r.InstanceType,
				nullStringOptional(r.SuggestedInstanceType), nullStringOptional(r.InstanceTypeReason),
				r.ConfidenceLevel, r.DataDays,
				core.NullIntExpl(r.Expl.DataDays),
				core.NullInt32Expl(r.Expl.TargetUtilizationBP),
				core.NullInt64Expl(r.Expl.CurrentCPUMC),
				core.NullInt64Expl(r.Expl.CurrentMemKiB),
				core.NullInt64Expl(r.Expl.MaxCPUUsageP95MC),
				core.NullInt64Expl(r.Expl.MaxMemUsageP95KiB),
				core.NullInt32Expl(r.Expl.PodSchedulingHeadroomBP),
				core.NullInt32Expl(r.Expl.EMAImbalanceBP),
				r.Expl.ConsolidationApplied,
				core.NullStringExpl(r.Expl.SizingFormula),
			)
		}
		if err := db.FlushRecommendationBatch(ctx, tx, batch); err != nil {
			return fmt.Errorf("batch node recs chunk %d: %w", chunkStart, err)
		}
	}

	// Remove rows for terms no longer in the active config (stale term cleanup).
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

	logging.ForOrg(orgID, clusterUUID).Infof("PersistRecommendations: upserted %d recs", len(recs))
	return nil
}

func nullInt64PodCapacity(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}

func nullStringMachineSet(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullStringOptional(s string) any {
	if s == "" {
		return nil
	}
	return s
}
