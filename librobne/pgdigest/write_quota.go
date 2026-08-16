package pgdigest

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
)

// WriteNamespaceQuotaDigests upserts already-computed namespace quota days
// with last-write-wins (not ingest GREATEST). Heap table — no partitions.
// Empty slice is a no-op. report_date is LastObservedAt.
func WriteNamespaceQuotaDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, rows []quota.NamespaceQuotaSnapshot) error {
	if len(rows) == 0 {
		return nil
	}
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return err
	}
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b quota.NamespaceQuotaSnapshot) int {
		if c := cmp.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		if c := cmp.Compare(a.QuotaName, b.QuotaName); c != 0 {
			return c
		}
		return a.LastObservedAt.Compare(b.LastObservedAt)
	})
	return withWriteTx(ctx, pool, func(tx pgx.Tx) error {
		if err := flushQueued(ctx, tx, len(sorted), func(batch *pgx.Batch, i int) {
			queueNamespaceQuotaInsert(batch, orgID, clusterUUID, sorted[i])
		}); err != nil {
			return fmt.Errorf("upsert namespace quota digest: %w", err)
		}
		return nil
	})
}

func queueNamespaceQuotaInsert(batch *pgx.Batch, orgID, clusterUUID string, s quota.NamespaceQuotaSnapshot) {
	batch.Queue(`
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
			cpu_request_hard = EXCLUDED.cpu_request_hard,
			cpu_request_used = EXCLUDED.cpu_request_used,
			cpu_limit_hard = EXCLUDED.cpu_limit_hard,
			cpu_limit_used = EXCLUDED.cpu_limit_used,
			memory_request_hard = EXCLUDED.memory_request_hard,
			memory_request_used = EXCLUDED.memory_request_used,
			memory_limit_hard = EXCLUDED.memory_limit_hard,
			memory_limit_used = EXCLUDED.memory_limit_used,
			storage_request_hard = EXCLUDED.storage_request_hard,
			storage_request_used = EXCLUDED.storage_request_used,
			pods_hard = EXCLUDED.pods_hard,
			pods_used = EXCLUDED.pods_used,
			object_count_hard = EXCLUDED.object_count_hard,
			object_count_used = EXCLUDED.object_count_used`,
		orgID, clusterUUID, s.Namespace, s.QuotaName, s.LastObservedAt,
		s.CPURequestHardMC, s.CPURequestUsedMC,
		s.CPULimitHardMC, s.CPULimitUsedMC,
		s.MemoryRequestHardBytes, s.MemoryRequestUsedBytes,
		s.MemoryLimitHardBytes, s.MemoryLimitUsedBytes,
		s.StorageRequestHardBytes, s.StorageRequestUsedBytes,
		s.PodsHard, s.PodsUsed,
		s.ObjectCountHard, s.ObjectCountUsed,
	)
}

// WriteClusterQuotaDigests upserts already-computed CRQ days with last-write-wins
// (not ingest GREATEST). Heap table — no partitions. Empty slice is a no-op.
func WriteClusterQuotaDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, rows []quota.ClusterQuotaSnapshot) error {
	if len(rows) == 0 {
		return nil
	}
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return err
	}
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b quota.ClusterQuotaSnapshot) int {
		if c := cmp.Compare(a.ClusterQuotaName, b.ClusterQuotaName); c != 0 {
			return c
		}
		return a.LastObservedAt.Compare(b.LastObservedAt)
	})
	return withWriteTx(ctx, pool, func(tx pgx.Tx) error {
		if err := flushQueued(ctx, tx, len(sorted), func(batch *pgx.Batch, i int) {
			queueClusterQuotaInsert(batch, orgID, clusterUUID, sorted[i])
		}); err != nil {
			return fmt.Errorf("upsert cluster quota digest: %w", err)
		}
		return nil
	})
}

func queueClusterQuotaInsert(batch *pgx.Batch, orgID, clusterUUID string, s quota.ClusterQuotaSnapshot) {
	batch.Queue(`
		INSERT INTO daily_cluster_quota_digests (
			org_id, cluster_uuid, cluster_quota_name, report_date,
			cpu_request_hard, cpu_request_used,
			cpu_limit_hard, cpu_limit_used,
			memory_request_hard, memory_request_used,
			memory_limit_hard, memory_limit_used,
			storage_request_hard, storage_request_used,
			pods_hard, pods_used,
			object_count_hard, object_count_used,
			namespaces
		) VALUES (
			$1, $2::uuid, $3, $4,
			$5, $6, $7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17, $18, $19
		)
		ON CONFLICT (org_id, cluster_uuid, cluster_quota_name, report_date)
		DO UPDATE SET
			cpu_request_hard = EXCLUDED.cpu_request_hard,
			cpu_request_used = EXCLUDED.cpu_request_used,
			cpu_limit_hard = EXCLUDED.cpu_limit_hard,
			cpu_limit_used = EXCLUDED.cpu_limit_used,
			memory_request_hard = EXCLUDED.memory_request_hard,
			memory_request_used = EXCLUDED.memory_request_used,
			memory_limit_hard = EXCLUDED.memory_limit_hard,
			memory_limit_used = EXCLUDED.memory_limit_used,
			storage_request_hard = EXCLUDED.storage_request_hard,
			storage_request_used = EXCLUDED.storage_request_used,
			pods_hard = EXCLUDED.pods_hard,
			pods_used = EXCLUDED.pods_used,
			object_count_hard = EXCLUDED.object_count_hard,
			object_count_used = EXCLUDED.object_count_used,
			namespaces = EXCLUDED.namespaces`,
		orgID, clusterUUID, s.ClusterQuotaName, s.LastObservedAt,
		s.CPURequestHardMC, s.CPURequestUsedMC,
		s.CPULimitHardMC, s.CPULimitUsedMC,
		s.MemoryRequestHardBytes, s.MemoryRequestUsedBytes,
		s.MemoryLimitHardBytes, s.MemoryLimitUsedBytes,
		s.StorageRequestHardBytes, s.StorageRequestUsedBytes,
		s.PodsHard, s.PodsUsed,
		s.ObjectCountHard, s.ObjectCountUsed,
		nullableString(s.Namespaces),
	)
}
