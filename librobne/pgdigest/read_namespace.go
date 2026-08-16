package pgdigest

import (
	"context"
	"fmt"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// ReadNamespaceDigests loads all_hours namespace usage days in [start, end].
// Empty result is not an error. Hard/used quota columns on this table are unused
// (quota recs read daily_namespace_quota_digests).
func ReadNamespaceDigests(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time) (map[namespace.NamespaceKey][]types.DigestRow, error) {
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return nil, err
	}
	if err := requireQuerier(q); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT bucket_date, namespace,
			COALESCE(cpu_request_p50_mc, 0), COALESCE(cpu_request_p60_mc, 0),
			COALESCE(cpu_request_p95_mc, 0), COALESCE(cpu_request_p98_mc, 0), COALESCE(cpu_request_p99_mc, 0),
			COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p60_mc, 0),
			COALESCE(cpu_usage_p95_mc, 0), COALESCE(cpu_usage_p98_mc, 0), COALESCE(cpu_usage_p99_mc, 0),
			COALESCE(cpu_usage_max_mc, 0),
			COALESCE(memory_request_p50_kib, 0), COALESCE(memory_request_p60_kib, 0),
			COALESCE(memory_request_p95_kib, 0), COALESCE(memory_request_p98_kib, 0), COALESCE(memory_request_p99_kib, 0),
			COALESCE(memory_usage_p50_kib, 0), COALESCE(memory_usage_p60_kib, 0),
			COALESCE(memory_usage_p95_kib, 0), COALESCE(memory_usage_p98_kib, 0), COALESCE(memory_usage_p99_kib, 0),
			COALESCE(memory_usage_max_kib, 0),
			COALESCE(cpu_usage_mean_mc, 0), COALESCE(memory_usage_mean_kib, 0), COALESCE(sample_count, 0)
		FROM daily_namespace_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		  AND schedule_type = 'all_hours'
		ORDER BY namespace, bucket_date`,
		orgID, clusterUUID, start.Format(dateLayout), end.Format(dateLayout))
	if err != nil {
		return nil, fmt.Errorf("pgdigest: query namespace digests: %w", err)
	}
	defer rows.Close()
	out := make(map[namespace.NamespaceKey][]types.DigestRow)
	for rows.Next() {
		var ns string
		var d types.DigestRow
		if err := rows.Scan(
			&d.BucketDate, &ns,
			&d.CPURequestP50MC, &d.CPURequestP60MC, &d.CPURequestP95MC, &d.CPURequestP98MC, &d.CPURequestP99MC,
			&d.CPUUsageP50MC, &d.CPUUsageP60MC, &d.CPUUsageP95MC, &d.CPUUsageP98MC, &d.CPUUsageP99MC, &d.CPUUsageMaxMC,
			&d.MemRequestP50KiB, &d.MemRequestP60KiB, &d.MemRequestP95KiB, &d.MemRequestP98KiB, &d.MemRequestP99KiB,
			&d.MemUsageP50KiB, &d.MemUsageP60KiB, &d.MemUsageP95KiB, &d.MemUsageP98KiB, &d.MemUsageP99KiB, &d.MemUsageMaxKiB,
			&d.CPUUsageMeanMC, &d.MemUsageMeanKiB, &d.SampleCount,
		); err != nil {
			return nil, fmt.Errorf("pgdigest: scan namespace digest: %w", err)
		}
		key := namespace.NamespaceKey{Namespace: ns}
		out[key] = append(out[key], d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgdigest: iterate namespace digests: %w", err)
	}
	return out, nil
}
