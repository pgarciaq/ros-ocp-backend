package quota

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	libquota "github.com/redhatinsights/ros-ocp-backend/librobne/quota"
)

// RecommendClusterQuotas reads cluster-quota digests and produces per-CRQ recommendations.
func RecommendClusterQuotas(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, cfg QuotaRecConfig) ([]ClusterQuotaRec, error) {
	snapshots, err := QueryLatestClusterQuotaSnapshots(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return nil, nil
	}

	nsAggs := make(map[string]NamespaceQuotaClusterAggregate)
	for _, snap := range snapshots {
		if !snap.HasHardLimits() {
			continue
		}
		nsAgg, err := QueryNamespaceQuotaAggregateForNamespaces(ctx, pool, orgID, clusterUUID, parseClusterQuotaNamespaces(snap.Namespaces))
		if err != nil {
			return nil, err
		}
		nsAggs[snap.ClusterQuotaName] = nsAgg
	}

	return libquota.RecommendClusterQuotas(snapshots, nsAggs, orgID, clusterUUID, cfg), nil
}

func parseClusterQuotaNamespaces(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ApplyClusterQuotaSavings computes estimated monthly savings in cents.
// CPU and memory use hourly rates; storage uses storage_gb_request_per_month (or usage fallback).
// Pods have no cost-model metric — capacity_freed.pods is reported but not monetized.
// hoursPerMonth should be HoursInMonth(year, month) for the target calendar month.
func ApplyClusterQuotaSavings(recs []ClusterQuotaRec, costData *costdata.ClusterCostData, hoursPerMonth int64) {
	if costData == nil {
		return
	}
	cpuRate := core.RateMicroCentsPerMCHour(costdata.CPUCoreHourlyRate(costData))
	memRate := core.RateMicroCentsPerGiBHour(costdata.MemoryGBHourlyRate(costData))
	storageRate := core.RateMicroCentsPerGiBMonth(costdata.StorageRequestPerMonth(costData))

	for i := range recs {
		if recs[i].RecommendationType != QuotaRecTypeTighten {
			continue
		}
		cpuDeltaMC := recs[i].CapacityFreed.CPUMillicores
		memDeltaBytes := recs[i].CapacityFreed.MemoryBytes
		storageDeltaBytes := recs[i].CapacityFreed.StorageBytes

		savingsMicroCents := core.QuotaTightenSavingsMicroCents(
			cpuDeltaMC, memDeltaBytes, storageDeltaBytes, cpuRate, memRate, storageRate, hoursPerMonth,
		)
		recs[i].EstimatedSavingsCents = core.MicroCentsToCents(savingsMicroCents)
	}
}

// QueryLatestClusterQuotaSnapshots returns the newest digest row per CRQ name.
func QueryLatestClusterQuotaSnapshots(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]ClusterQuotaSnapshot, error) {
	query := `
		SELECT DISTINCT ON (cluster_quota_name)
			cluster_quota_name, COALESCE(namespaces, ''),
			cpu_request_hard, cpu_limit_hard,
			memory_request_hard, memory_limit_hard,
			cpu_request_used, cpu_limit_used,
			memory_request_used, memory_limit_used,
			storage_request_hard, storage_request_used,
			pods_hard, pods_used,
			object_count_hard, object_count_used,
			report_date
		FROM daily_cluster_quota_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid
			AND (
				cpu_request_hard IS NOT NULL OR cpu_limit_hard IS NOT NULL OR
				memory_request_hard IS NOT NULL OR memory_limit_hard IS NOT NULL OR
				storage_request_hard IS NOT NULL OR pods_hard IS NOT NULL OR
				object_count_hard IS NOT NULL
			)
		ORDER BY cluster_quota_name, report_date DESC`

	rows, err := pool.Query(ctx, query, orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("query cluster quota snapshots: %w", err)
	}
	defer rows.Close()

	var out []ClusterQuotaSnapshot
	for rows.Next() {
		var s ClusterQuotaSnapshot
		var reportDate time.Time
		var cpuReqHard, cpuLimHard, memReqHard, memLimHard *int64
		var cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed *int64
		var storageHard, storageUsed, podsHard, podsUsed, objHard, objUsed *int64
		if err := rows.Scan(
			&s.ClusterQuotaName, &s.Namespaces,
			&cpuReqHard, &cpuLimHard, &memReqHard, &memLimHard,
			&cpuReqUsed, &cpuLimUsed, &memReqUsed, &memLimUsed,
			&storageHard, &storageUsed, &podsHard, &podsUsed, &objHard, &objUsed,
			&reportDate,
		); err != nil {
			return nil, fmt.Errorf("scan cluster quota snapshot: %w", err)
		}
		s.CPURequestHardMC = derefInt64(cpuReqHard)
		s.CPULimitHardMC = derefInt64(cpuLimHard)
		s.MemoryRequestHardBytes = derefInt64(memReqHard)
		s.MemoryLimitHardBytes = derefInt64(memLimHard)
		s.CPURequestUsedMC = derefInt64(cpuReqUsed)
		s.CPULimitUsedMC = derefInt64(cpuLimUsed)
		s.MemoryRequestUsedBytes = derefInt64(memReqUsed)
		s.MemoryLimitUsedBytes = derefInt64(memLimUsed)
		s.StorageRequestHardBytes = derefInt64(storageHard)
		s.StorageRequestUsedBytes = derefInt64(storageUsed)
		s.PodsHard = derefInt64(podsHard)
		s.PodsUsed = derefInt64(podsUsed)
		s.ObjectCountHard = derefInt64(objHard)
		s.ObjectCountUsed = derefInt64(objUsed)
		s.LastObservedAt = reportDate
		out = append(out, s)
	}
	return out, rows.Err()
}

// QueryNamespaceQuotaAggregateForNamespaces sums namespace quota recommendations.
// When namespaces is empty, aggregates across the whole cluster (legacy behavior).
func QueryNamespaceQuotaAggregateForNamespaces(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	namespaces []string,
) (NamespaceQuotaClusterAggregate, error) {
	var agg NamespaceQuotaClusterAggregate
	var err error
	if len(namespaces) == 0 {
		err = pool.QueryRow(ctx, `
			SELECT
				COALESCE(SUM(cpu_request_recommended_millicores), 0),
				COALESCE(SUM(cpu_limit_recommended_millicores), 0),
				COALESCE(SUM(memory_request_recommended_bytes), 0),
				COALESCE(SUM(memory_limit_recommended_bytes), 0)
			FROM quota_recommendation_sets
			WHERE org_id = $1 AND cluster_uuid = $2::uuid`,
			orgID, clusterUUID,
		).Scan(
			&agg.CPURequestRecommendedMC,
			&agg.CPULimitRecommendedMC,
			&agg.MemoryRequestRecommendedBytes,
			&agg.MemoryLimitRecommendedBytes,
		)
	} else {
		err = pool.QueryRow(ctx, `
			SELECT
				COALESCE(SUM(cpu_request_recommended_millicores), 0),
				COALESCE(SUM(cpu_limit_recommended_millicores), 0),
				COALESCE(SUM(memory_request_recommended_bytes), 0),
				COALESCE(SUM(memory_limit_recommended_bytes), 0)
			FROM quota_recommendation_sets
			WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = ANY($3::text[])`,
			orgID, clusterUUID, namespaces,
		).Scan(
			&agg.CPURequestRecommendedMC,
			&agg.CPULimitRecommendedMC,
			&agg.MemoryRequestRecommendedBytes,
			&agg.MemoryLimitRecommendedBytes,
		)
	}
	if err != nil {
		return agg, fmt.Errorf("query namespace quota aggregate: %w", err)
	}
	return agg, nil
}

// WriteClusterQuotaRecommendations upserts cluster-quota recommendations.
func WriteClusterQuotaRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []ClusterQuotaRec) error {
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
				notification_codes, updated_at,`+core.ClusterQuotaExplSQLColumns+`
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
				updated_at = NOW(),`+core.ClusterQuotaExplUpdateSet,
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
			}, core.AppendClusterQuotaExplArgs(nil, r.Expl)...)...,
		)
		if err != nil {
			return fmt.Errorf("upsert cluster quota recommendation %s: %w", r.ClusterQuotaName, err)
		}
	}
	return nil
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
