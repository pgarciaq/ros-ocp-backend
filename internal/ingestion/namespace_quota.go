package ingestion

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type namespaceQuotaDigestKey struct {
	orgID       string
	clusterUUID string
	namespace   string
	quotaName   string
	reportDate  time.Time
}

type namespaceQuotaDigestAgg struct {
	key                namespaceQuotaDigestKey
	cpuRequestHard     int64
	cpuRequestUsed     int64
	cpuLimitHard       int64
	cpuLimitUsed       int64
	memoryRequestHard  int64
	memoryRequestUsed  int64
	memoryLimitHard    int64
	memoryLimitUsed    int64
	storageRequestHard int64
	storageRequestUsed int64
	podsHard           int64
	podsUsed           int64
	objectCountHard    int64
	objectCountUsed    int64
}

func groupNamespaceQuotaRows(rows []NamespaceMetricRow, orgID, clusterUUID string) map[namespaceQuotaDigestKey]*namespaceQuotaDigestAgg {
	out := make(map[namespaceQuotaDigestKey]*namespaceQuotaDigestAgg)
	for _, row := range rows {
		if !namespaceRowHasQuotaHardOrUsed(row) {
			continue
		}
		reportDate := time.Date(
			row.IntervalEnd.Year(), row.IntervalEnd.Month(), row.IntervalEnd.Day(),
			0, 0, 0, 0, time.UTC,
		)
		key := namespaceQuotaDigestKey{
			orgID:       orgID,
			clusterUUID: clusterUUID,
			namespace:   row.Namespace,
			quotaName:   row.QuotaName,
			reportDate:  reportDate,
		}
		agg, ok := out[key]
		if !ok {
			agg = &namespaceQuotaDigestAgg{key: key}
			out[key] = agg
		}
		agg.cpuRequestHard = maxInt64NS(agg.cpuRequestHard, row.CPURequestHardMC)
		agg.cpuRequestUsed = maxInt64NS(agg.cpuRequestUsed, row.CPURequestUsedMC)
		agg.cpuLimitHard = maxInt64NS(agg.cpuLimitHard, row.CPULimitHardMC)
		agg.cpuLimitUsed = maxInt64NS(agg.cpuLimitUsed, row.CPULimitUsedMC)
		agg.memoryRequestHard = maxInt64NS(agg.memoryRequestHard, row.MemoryRequestHardBytes)
		agg.memoryRequestUsed = maxInt64NS(agg.memoryRequestUsed, row.MemoryRequestUsedBytes)
		agg.memoryLimitHard = maxInt64NS(agg.memoryLimitHard, row.MemoryLimitHardBytes)
		agg.memoryLimitUsed = maxInt64NS(agg.memoryLimitUsed, row.MemoryLimitUsedBytes)
		agg.storageRequestHard = maxInt64NS(agg.storageRequestHard, row.StorageRequestHardBytes)
		agg.storageRequestUsed = maxInt64NS(agg.storageRequestUsed, row.StorageRequestUsedBytes)
		agg.podsHard = maxInt64NS(agg.podsHard, row.PodsHard)
		agg.podsUsed = maxInt64NS(agg.podsUsed, row.PodsUsed)
		agg.objectCountHard = maxInt64NS(agg.objectCountHard, row.ObjectCountHard)
		agg.objectCountUsed = maxInt64NS(agg.objectCountUsed, row.ObjectCountUsed)
	}
	return out
}

func namespaceRowHasQuotaHardOrUsed(r NamespaceMetricRow) bool {
	return r.CPURequestHardMC > 0 || r.CPULimitHardMC > 0 ||
		r.MemoryRequestHardBytes > 0 || r.MemoryLimitHardBytes > 0 ||
		r.StorageRequestHardBytes > 0 || r.PodsHard > 0 || r.ObjectCountHard > 0 ||
		r.CPURequestUsedMC > 0 || r.CPULimitUsedMC > 0 ||
		r.MemoryRequestUsedBytes > 0 || r.MemoryLimitUsedBytes > 0 ||
		r.StorageRequestUsedBytes > 0 || r.PodsUsed > 0 || r.ObjectCountUsed > 0
}

// UpsertNamespaceQuotaDigestsFromRows persists per-ResourceQuota daily snapshots.
func UpsertNamespaceQuotaDigestsFromRows(ctx context.Context, pool *pgxpool.Pool, rows []NamespaceMetricRow, orgID, clusterUUID string) error {
	groups := groupNamespaceQuotaRows(rows, orgID, clusterUUID)
	for _, agg := range groups {
		if err := upsertNamespaceQuotaDigest(ctx, pool, agg); err != nil {
			return err
		}
	}
	return nil
}

func upsertNamespaceQuotaDigest(ctx context.Context, pool *pgxpool.Pool, agg *namespaceQuotaDigestAgg) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO daily_namespace_quota_digests (
			org_id, cluster_uuid, namespace, quota_name, report_date,
			cpu_request_hard, cpu_request_used,
			cpu_limit_hard, cpu_limit_used,
			memory_request_hard, memory_request_used,
			memory_limit_hard, memory_limit_used,
			storage_request_hard, storage_request_used,
			pods_hard, pods_used,
			object_count_hard, object_count_used
		) VALUES (
			$1, $2::uuid, $3, $4, $5,
			$6, $7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19
		)
		ON CONFLICT (org_id, cluster_uuid, namespace, quota_name, report_date)
		DO UPDATE SET
			cpu_request_hard = GREATEST(COALESCE(daily_namespace_quota_digests.cpu_request_hard, 0), COALESCE(EXCLUDED.cpu_request_hard, 0)),
			cpu_request_used = GREATEST(COALESCE(daily_namespace_quota_digests.cpu_request_used, 0), COALESCE(EXCLUDED.cpu_request_used, 0)),
			cpu_limit_hard = GREATEST(COALESCE(daily_namespace_quota_digests.cpu_limit_hard, 0), COALESCE(EXCLUDED.cpu_limit_hard, 0)),
			cpu_limit_used = GREATEST(COALESCE(daily_namespace_quota_digests.cpu_limit_used, 0), COALESCE(EXCLUDED.cpu_limit_used, 0)),
			memory_request_hard = GREATEST(COALESCE(daily_namespace_quota_digests.memory_request_hard, 0), COALESCE(EXCLUDED.memory_request_hard, 0)),
			memory_request_used = GREATEST(COALESCE(daily_namespace_quota_digests.memory_request_used, 0), COALESCE(EXCLUDED.memory_request_used, 0)),
			memory_limit_hard = GREATEST(COALESCE(daily_namespace_quota_digests.memory_limit_hard, 0), COALESCE(EXCLUDED.memory_limit_hard, 0)),
			memory_limit_used = GREATEST(COALESCE(daily_namespace_quota_digests.memory_limit_used, 0), COALESCE(EXCLUDED.memory_limit_used, 0)),
			storage_request_hard = GREATEST(COALESCE(daily_namespace_quota_digests.storage_request_hard, 0), COALESCE(EXCLUDED.storage_request_hard, 0)),
			storage_request_used = GREATEST(COALESCE(daily_namespace_quota_digests.storage_request_used, 0), COALESCE(EXCLUDED.storage_request_used, 0)),
			pods_hard = GREATEST(COALESCE(daily_namespace_quota_digests.pods_hard, 0), COALESCE(EXCLUDED.pods_hard, 0)),
			pods_used = GREATEST(COALESCE(daily_namespace_quota_digests.pods_used, 0), COALESCE(EXCLUDED.pods_used, 0)),
			object_count_hard = GREATEST(COALESCE(daily_namespace_quota_digests.object_count_hard, 0), COALESCE(EXCLUDED.object_count_hard, 0)),
			object_count_used = GREATEST(COALESCE(daily_namespace_quota_digests.object_count_used, 0), COALESCE(EXCLUDED.object_count_used, 0))`,
		agg.key.orgID, agg.key.clusterUUID, agg.key.namespace, agg.key.quotaName, agg.key.reportDate,
		nullableInt64Digest(agg.cpuRequestHard), nullableInt64Digest(agg.cpuRequestUsed),
		nullableInt64Digest(agg.cpuLimitHard), nullableInt64Digest(agg.cpuLimitUsed),
		nullableInt64Digest(agg.memoryRequestHard), nullableInt64Digest(agg.memoryRequestUsed),
		nullableInt64Digest(agg.memoryLimitHard), nullableInt64Digest(agg.memoryLimitUsed),
		nullableInt64Digest(agg.storageRequestHard), nullableInt64Digest(agg.storageRequestUsed),
		nullableInt64Digest(agg.podsHard), nullableInt64Digest(agg.podsUsed),
		nullableInt64Digest(agg.objectCountHard), nullableInt64Digest(agg.objectCountUsed),
	)
	if err != nil {
		return fmt.Errorf("upsert namespace quota digest %s/%s: %w", agg.key.namespace, agg.key.quotaName, err)
	}
	return nil
}
