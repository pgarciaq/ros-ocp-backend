package pgdigest

import (
	"context"
	"fmt"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
)

// ReadNodeDigests loads node daily rows in [start, end]. Empty result is not an error.
func ReadNodeDigests(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time) ([]node.DigestRow, error) {
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return nil, err
	}
	if err := requireQuerier(q); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT bucket_date, node,
			COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p95_mc, 0), COALESCE(cpu_usage_max_mc, 0),
			COALESCE(mem_usage_p50_kib, 0), COALESCE(mem_usage_p95_kib, 0), COALESCE(mem_usage_max_kib, 0),
			max_cpu_allocatable_mc, max_mem_allocatable_kib,
			COALESCE(max_cpu_requests_mc, 0), COALESCE(max_mem_requests_kib, 0),
			COALESCE(max_pod_count, 0), COALESCE(pod_capacity, 0),
			instance_type, machineset_name,
			COALESCE(sample_count, 0), node_gpu_count
		FROM daily_node_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		ORDER BY node, bucket_date`,
		orgID, clusterUUID, start.Format(dateLayout), end.Format(dateLayout))
	if err != nil {
		return nil, fmt.Errorf("pgdigest: query node digests: %w", err)
	}
	defer rows.Close()
	var out []node.DigestRow
	for rows.Next() {
		var d node.DigestRow
		var inst, ms *string
		if err := rows.Scan(
			&d.BucketDate, &d.Node,
			&d.CPUUsageP50MC, &d.CPUUsageP95MC, &d.CPUUsageMaxMC,
			&d.MemUsageP50KiB, &d.MemUsageP95KiB, &d.MemUsageMaxKiB,
			&d.MaxCPUAllocMC, &d.MaxMemAllocKiB,
			&d.MaxCPURequestsMC, &d.MaxMemRequestsKiB,
			&d.MaxPodCount, &d.PodCapacity,
			&inst, &ms,
			&d.SampleCount, &d.NodeGPUCount,
		); err != nil {
			return nil, fmt.Errorf("pgdigest: scan node digest: %w", err)
		}
		d.InstanceType = derefString(inst)
		d.MachineSetName = derefString(ms)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgdigest: iterate node digests: %w", err)
	}
	return out, nil
}
