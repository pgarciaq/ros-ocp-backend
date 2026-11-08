package pgdigest

import (
	"context"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// ReadSleepEvidence reads the #760 sleep evidence legs for a cluster
// window: the per-container-day in-window and off-window p50s from
// hcp_sleep_evidence. Each leg becomes its own KeyedDigest set (NULL legs
// are absent from their set) so SleepSums(bh, off) receives exactly the
// server predicate's evidence shape.
func ReadSleepEvidence(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time) (bh, off []types.KeyedDigest, err error) {
	rows, err := q.Query(ctx, `
		SELECT namespace, workload, workload_type, container_name, bucket_date,
			bh_cpu_p50_mc, off_cpu_p50_mc
		FROM hcp_sleep_evidence
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date < $4`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ns, wl, wt, cn string
		var day time.Time
		var bhP50, offP50 *int64
		if err := rows.Scan(&ns, &wl, &wt, &cn, &day, &bhP50, &offP50); err != nil {
			return nil, nil, err
		}
		key := types.ContainerKey{
			Namespace: ns, Workload: wl, WorkloadType: wt, ContainerName: cn,
		}
		if bhP50 != nil {
			bh = append(bh, types.KeyedDigest{
				Key: key,
				Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: *bhP50},
			})
		}
		if offP50 != nil {
			off = append(off, types.KeyedDigest{
				Key: key,
				Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: *offP50},
			})
		}
	}
	return bh, off, rows.Err()
}
