package pgdigest

import (
	"context"
	"fmt"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
)

// ReadNamespaceQuotaDigests loads namespace quota days in [start, end]
// (report_date window). Empty result is not an error. Caller reduces to latest
// day per namespace×quota_name.
func ReadNamespaceQuotaDigests(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time) ([]quota.NamespaceQuotaSnapshot, error) {
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return nil, err
	}
	if err := requireQuerier(q); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT namespace, quota_name, report_date,
			COALESCE(cpu_request_hard, 0), COALESCE(cpu_request_used, 0),
			COALESCE(cpu_limit_hard, 0), COALESCE(cpu_limit_used, 0),
			COALESCE(memory_request_hard, 0), COALESCE(memory_request_used, 0),
			COALESCE(memory_limit_hard, 0), COALESCE(memory_limit_used, 0),
			COALESCE(storage_request_hard, 0), COALESCE(storage_request_used, 0),
			COALESCE(pods_hard, 0), COALESCE(pods_used, 0),
			COALESCE(object_count_hard, 0), COALESCE(object_count_used, 0)
		FROM daily_namespace_quota_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND report_date >= $3 AND report_date <= $4
		ORDER BY namespace, quota_name, report_date`,
		orgID, clusterUUID, start.Format(dateLayout), end.Format(dateLayout))
	if err != nil {
		return nil, fmt.Errorf("pgdigest: query namespace quota digests: %w", err)
	}
	defer rows.Close()
	var out []quota.NamespaceQuotaSnapshot
	for rows.Next() {
		var s quota.NamespaceQuotaSnapshot
		if err := rows.Scan(
			&s.Namespace, &s.QuotaName, &s.LastObservedAt,
			&s.CPURequestHardMC, &s.CPURequestUsedMC,
			&s.CPULimitHardMC, &s.CPULimitUsedMC,
			&s.MemoryRequestHardBytes, &s.MemoryRequestUsedBytes,
			&s.MemoryLimitHardBytes, &s.MemoryLimitUsedBytes,
			&s.StorageRequestHardBytes, &s.StorageRequestUsedBytes,
			&s.PodsHard, &s.PodsUsed,
			&s.ObjectCountHard, &s.ObjectCountUsed,
		); err != nil {
			return nil, fmt.Errorf("pgdigest: scan namespace quota digest: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgdigest: iterate namespace quota digests: %w", err)
	}
	return out, nil
}

// ReadClusterQuotaDigests loads CRQ days in [start, end]. Empty namespaces
// stored as NULL reconstruct as "". Empty result is not an error. Caller
// reduces to latest day with hard limits.
func ReadClusterQuotaDigests(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time) ([]quota.ClusterQuotaSnapshot, error) {
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return nil, err
	}
	if err := requireQuerier(q); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT cluster_quota_name, report_date, namespaces,
			COALESCE(cpu_request_hard, 0), COALESCE(cpu_request_used, 0),
			COALESCE(cpu_limit_hard, 0), COALESCE(cpu_limit_used, 0),
			COALESCE(memory_request_hard, 0), COALESCE(memory_request_used, 0),
			COALESCE(memory_limit_hard, 0), COALESCE(memory_limit_used, 0),
			COALESCE(storage_request_hard, 0), COALESCE(storage_request_used, 0),
			COALESCE(pods_hard, 0), COALESCE(pods_used, 0),
			COALESCE(object_count_hard, 0), COALESCE(object_count_used, 0)
		FROM daily_cluster_quota_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND report_date >= $3 AND report_date <= $4
		ORDER BY cluster_quota_name, report_date`,
		orgID, clusterUUID, start.Format(dateLayout), end.Format(dateLayout))
	if err != nil {
		return nil, fmt.Errorf("pgdigest: query cluster quota digests: %w", err)
	}
	defer rows.Close()
	var out []quota.ClusterQuotaSnapshot
	for rows.Next() {
		var s quota.ClusterQuotaSnapshot
		var ns *string
		if err := rows.Scan(
			&s.ClusterQuotaName, &s.LastObservedAt, &ns,
			&s.CPURequestHardMC, &s.CPURequestUsedMC,
			&s.CPULimitHardMC, &s.CPULimitUsedMC,
			&s.MemoryRequestHardBytes, &s.MemoryRequestUsedBytes,
			&s.MemoryLimitHardBytes, &s.MemoryLimitUsedBytes,
			&s.StorageRequestHardBytes, &s.StorageRequestUsedBytes,
			&s.PodsHard, &s.PodsUsed,
			&s.ObjectCountHard, &s.ObjectCountUsed,
		); err != nil {
			return nil, fmt.Errorf("pgdigest: scan cluster quota digest: %w", err)
		}
		s.Namespaces = derefString(ns)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgdigest: iterate cluster quota digests: %w", err)
	}
	return out, nil
}
