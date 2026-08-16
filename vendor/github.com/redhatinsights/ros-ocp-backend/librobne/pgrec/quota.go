package pgrec

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// WriteQuotaRecommendations upserts quota recommendations for a cluster.
func WriteQuotaRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []quota.QuotaRec) error {
	for _, r := range recs {
		s := r.Snapshot
		_, err := pool.Exec(ctx, `
			INSERT INTO quota_recommendation_sets (
				org_id, cluster_uuid, namespace, quota_name,
				cpu_request_hard_millicores, cpu_limit_hard_millicores,
				memory_request_hard_bytes, memory_limit_hard_bytes,
				cpu_request_used_millicores, cpu_limit_used_millicores,
				memory_request_used_bytes, memory_limit_used_bytes,
				storage_request_hard_bytes, storage_request_used_bytes,
				storage_request_recommended_bytes,
				pods_hard, pods_used, pods_recommended,
				cpu_request_recommended_millicores, cpu_limit_recommended_millicores,
				memory_request_recommended_bytes, memory_limit_recommended_bytes,
				headroom_basis_points,
				cpu_request_utilization_bp, cpu_limit_utilization_bp,
				memory_request_utilization_bp, memory_limit_utilization_bp,
				utilization_storage_request_bp, utilization_pods_bp,
				cpu_freed_millicores, memory_freed_bytes,
				storage_freed_bytes, pods_freed,
				estimated_savings_cents, currency,
				recommendation_type, risk_level, notification_codes,
				quota_id, last_observed_at, updated_at,`+types.QuotaExplSQLColumns+`
			) VALUES (
				$1, $2::uuid, $3, $4,
				$5, $6, $7, $8,
				$9, $10, $11, $12,
				$13, $14, $15,
				$16, $17, $18,
				$19, $20, $21, $22,
				$23,
				$24, $25, $26, $27,
				$28, $29,
				$30, $31, $32, $33,
				$34, $35, $36,
				$37, $38, $39, $40, NOW(), $41, $42, $43, $44, $45, $46, $47
			)
			ON CONFLICT (org_id, cluster_uuid, namespace, quota_name)
			DO UPDATE SET
				cpu_request_hard_millicores = EXCLUDED.cpu_request_hard_millicores,
				cpu_limit_hard_millicores = EXCLUDED.cpu_limit_hard_millicores,
				memory_request_hard_bytes = EXCLUDED.memory_request_hard_bytes,
				memory_limit_hard_bytes = EXCLUDED.memory_limit_hard_bytes,
				cpu_request_used_millicores = EXCLUDED.cpu_request_used_millicores,
				cpu_limit_used_millicores = EXCLUDED.cpu_limit_used_millicores,
				memory_request_used_bytes = EXCLUDED.memory_request_used_bytes,
				memory_limit_used_bytes = EXCLUDED.memory_limit_used_bytes,
				storage_request_hard_bytes = EXCLUDED.storage_request_hard_bytes,
				storage_request_used_bytes = EXCLUDED.storage_request_used_bytes,
				storage_request_recommended_bytes = EXCLUDED.storage_request_recommended_bytes,
				pods_hard = EXCLUDED.pods_hard,
				pods_used = EXCLUDED.pods_used,
				pods_recommended = EXCLUDED.pods_recommended,
				cpu_request_recommended_millicores = EXCLUDED.cpu_request_recommended_millicores,
				cpu_limit_recommended_millicores = EXCLUDED.cpu_limit_recommended_millicores,
				memory_request_recommended_bytes = EXCLUDED.memory_request_recommended_bytes,
				memory_limit_recommended_bytes = EXCLUDED.memory_limit_recommended_bytes,
				headroom_basis_points = EXCLUDED.headroom_basis_points,
				cpu_request_utilization_bp = EXCLUDED.cpu_request_utilization_bp,
				cpu_limit_utilization_bp = EXCLUDED.cpu_limit_utilization_bp,
				memory_request_utilization_bp = EXCLUDED.memory_request_utilization_bp,
				memory_limit_utilization_bp = EXCLUDED.memory_limit_utilization_bp,
				utilization_storage_request_bp = EXCLUDED.utilization_storage_request_bp,
				utilization_pods_bp = EXCLUDED.utilization_pods_bp,
				cpu_freed_millicores = EXCLUDED.cpu_freed_millicores,
				memory_freed_bytes = EXCLUDED.memory_freed_bytes,
				storage_freed_bytes = EXCLUDED.storage_freed_bytes,
				pods_freed = EXCLUDED.pods_freed,
				estimated_savings_cents = EXCLUDED.estimated_savings_cents,
				currency = EXCLUDED.currency,
				recommendation_type = EXCLUDED.recommendation_type,
				risk_level = EXCLUDED.risk_level,
				notification_codes = EXCLUDED.notification_codes,
				quota_id = EXCLUDED.quota_id,
				last_observed_at = EXCLUDED.last_observed_at,
				updated_at = NOW(),`+types.QuotaExplUpdateSet,
			append([]any{
				r.OrgID, r.ClusterUUID, r.Namespace, r.QuotaName,
				nullableInt64(s.CPURequestHardMC), nullableInt64(s.CPULimitHardMC),
				nullableInt64(s.MemoryRequestHardBytes), nullableInt64(s.MemoryLimitHardBytes),
				nullableInt64(s.CPURequestUsedMC), nullableInt64(s.CPULimitUsedMC),
				nullableInt64(s.MemoryRequestUsedBytes), nullableInt64(s.MemoryLimitUsedBytes),
				nullableInt64(s.StorageRequestHardBytes), nullableInt64(s.StorageRequestUsedBytes),
				nullableInt64(r.Recommended.StorageRequestBytes),
				nullableInt64(s.PodsHard), nullableInt64(s.PodsUsed), nullableInt64(r.Recommended.Pods),
				nullableInt64(r.Recommended.CPURequestMillicores), nullableInt64(r.Recommended.CPULimitMillicores),
				nullableInt64(r.Recommended.MemoryRequestBytes), nullableInt64(r.Recommended.MemoryLimitBytes),
				r.HeadroomBP,
				r.Utilization.CPURequestBP, r.Utilization.CPULimitBP,
				r.Utilization.MemoryRequestBP, r.Utilization.MemoryLimitBP,
				quota.UtilizationBP(s.StorageRequestUsedBytes, s.StorageRequestHardBytes),
				quota.UtilizationBP(s.PodsUsed, s.PodsHard),
				nullableInt64(r.CapacityFreed.CPUMillicores), nullableInt64(r.CapacityFreed.MemoryBytes),
				nullableInt64(r.CapacityFreed.StorageBytes), nullableInt64(r.CapacityFreed.PodsFreed),
				nullableInt64(r.EstimatedSavingsCents), r.Currency,
				r.RecommendationType, r.RiskLevel, r.NotificationCodes,
				NativeQuotaID(r.ClusterUUID, r.Namespace, r.QuotaName),
				s.LastObservedAt,
			}, types.AppendQuotaExplArgs(nil, r.Expl)...)...,
		)
		if err != nil {
			return fmt.Errorf("upsert quota recommendation %s/%s: %w", r.Namespace, r.QuotaName, err)
		}
	}
	return nil
}

// WriteClusterQuotaRecommendations upserts cluster-quota recommendations.
func WriteClusterQuotaRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []quota.ClusterQuotaRec) error {
	for _, r := range recs {
		s := r.Snapshot
		cpuCoresFreed := r.CapacityFreed.CPUMillicores / 1000
		_, err := pool.Exec(ctx, `
			INSERT INTO cluster_quota_recommendation_sets (
				org_id, cluster_uuid, cluster_quota_name, namespaces,
				recommendation_type, risk_level,
				cpu_request_hard, cpu_request_used, cpu_request_recommended,
				cpu_limit_hard, cpu_limit_used, cpu_limit_recommended,
				memory_request_hard, memory_request_used, memory_request_recommended,
				memory_limit_hard, memory_limit_used, memory_limit_recommended,
				storage_request_hard, storage_request_used, storage_request_recommended,
				pods_hard, pods_used, pods_recommended,
				utilization_cpu_request_percent, utilization_memory_request_percent,
				utilization_storage_request_percent, utilization_pods_percent,
				savings_cpu_cores_freed, savings_memory_bytes_freed,
				savings_storage_bytes_freed, savings_pods_freed,
				estimated_savings_cents,
				notification_codes, updated_at,`+types.ClusterQuotaExplSQLColumns+`
			) VALUES (
				$1, $2::uuid, $3, $4,
				$5, $6,
				$7, $8, $9,
				$10, $11, $12,
				$13, $14, $15,
				$16, $17, $18,
				$19, $20, $21,
				$22, $23, $24,
				$25, $26, $27, $28,
				$29, $30, $31, $32, $33,
				$34,
				NOW(), $35, $36, $37, $38, $39, $40
			)
			ON CONFLICT (org_id, cluster_uuid, cluster_quota_name)
			DO UPDATE SET
				namespaces = EXCLUDED.namespaces,
				recommendation_type = EXCLUDED.recommendation_type,
				risk_level = EXCLUDED.risk_level,
				cpu_request_hard = EXCLUDED.cpu_request_hard,
				cpu_request_used = EXCLUDED.cpu_request_used,
				cpu_request_recommended = EXCLUDED.cpu_request_recommended,
				cpu_limit_hard = EXCLUDED.cpu_limit_hard,
				cpu_limit_used = EXCLUDED.cpu_limit_used,
				cpu_limit_recommended = EXCLUDED.cpu_limit_recommended,
				memory_request_hard = EXCLUDED.memory_request_hard,
				memory_request_used = EXCLUDED.memory_request_used,
				memory_request_recommended = EXCLUDED.memory_request_recommended,
				memory_limit_hard = EXCLUDED.memory_limit_hard,
				memory_limit_used = EXCLUDED.memory_limit_used,
				memory_limit_recommended = EXCLUDED.memory_limit_recommended,
				storage_request_hard = EXCLUDED.storage_request_hard,
				storage_request_used = EXCLUDED.storage_request_used,
				storage_request_recommended = EXCLUDED.storage_request_recommended,
				pods_hard = EXCLUDED.pods_hard,
				pods_used = EXCLUDED.pods_used,
				pods_recommended = EXCLUDED.pods_recommended,
				utilization_cpu_request_percent = EXCLUDED.utilization_cpu_request_percent,
				utilization_memory_request_percent = EXCLUDED.utilization_memory_request_percent,
				utilization_storage_request_percent = EXCLUDED.utilization_storage_request_percent,
				utilization_pods_percent = EXCLUDED.utilization_pods_percent,
				savings_cpu_cores_freed = EXCLUDED.savings_cpu_cores_freed,
				savings_memory_bytes_freed = EXCLUDED.savings_memory_bytes_freed,
				savings_storage_bytes_freed = EXCLUDED.savings_storage_bytes_freed,
				savings_pods_freed = EXCLUDED.savings_pods_freed,
				estimated_savings_cents = EXCLUDED.estimated_savings_cents,
				notification_codes = EXCLUDED.notification_codes,
				updated_at = NOW(),`+types.ClusterQuotaExplUpdateSet,
			append([]any{
				r.OrgID, r.ClusterUUID, r.ClusterQuotaName, nullableString(r.Namespaces),
				r.RecommendationType, r.RiskLevel,
				nullableInt64(s.CPURequestHardMC), nullableInt64(s.CPURequestUsedMC), nullableInt64(r.Recommended.CPURequestMillicores),
				nullableInt64(s.CPULimitHardMC), nullableInt64(s.CPULimitUsedMC), nullableInt64(r.Recommended.CPULimitMillicores),
				nullableInt64(s.MemoryRequestHardBytes), nullableInt64(s.MemoryRequestUsedBytes), nullableInt64(r.Recommended.MemoryRequestBytes),
				nullableInt64(s.MemoryLimitHardBytes), nullableInt64(s.MemoryLimitUsedBytes), nullableInt64(r.Recommended.MemoryLimitBytes),
				nullableInt64(s.StorageRequestHardBytes), nullableInt64(s.StorageRequestUsedBytes), nullableInt64(r.StorageRecommendedBytes),
				nullableInt64(s.PodsHard), nullableInt64(s.PodsUsed), nullableInt64(r.PodsRecommended),
				r.UtilizationCPURequestPercent, r.UtilizationMemoryRequestPercent,
				r.UtilizationStorageRequestPercent, r.UtilizationPodsPercent,
				nullableInt64(cpuCoresFreed), nullableInt64(r.CapacityFreed.MemoryBytes),
				nullableInt64(r.CapacityFreed.StorageBytes), nullableInt64(r.CapacityFreed.PodsFreed),
				nullableInt64(r.EstimatedSavingsCents),
				r.NotificationCodes,
			}, types.AppendClusterQuotaExplArgs(nil, r.Expl)...)...,
		)
		if err != nil {
			return fmt.Errorf("upsert cluster quota recommendation %s: %w", r.ClusterQuotaName, err)
		}
	}
	return nil
}
