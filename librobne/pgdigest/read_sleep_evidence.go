package pgdigest

import (
	"context"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// ReadSleepEvidence reads the #760/#826 sleep evidence legs for a cluster
// window: the per-container-day in-window p50s and off-window p95s from
// hcp_sleep_evidence. Each measured leg becomes its own KeyedDigest set
// (NULL legs are absent from their set). offLegacy carries rows whose off
// leg predates #826 (off-p50 set, p95 NULL): they mark the workload's off
// leg PRESENT-but-UNMEASURED — SleepSumsAll routes them through the #761
// unknown gate, so a legacy night-only workload cannot vanish behind a
// re-ingested quiet sibling (#826 review finding).
func ReadSleepEvidence(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time) (bh, off, offLegacy []types.KeyedDigest, err error) {
	rows, err := q.Query(ctx, `
		SELECT namespace, workload, workload_type, container_name, bucket_date,
			bh_cpu_p50_mc, off_cpu_p50_mc, off_cpu_p95_mc
		FROM hcp_sleep_evidence
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date < $4`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ns, wl, wt, cn string
		var day time.Time
		var bhP50, offP50, offP95 *int64
		if err := rows.Scan(&ns, &wl, &wt, &cn, &day, &bhP50, &offP50, &offP95); err != nil {
			return nil, nil, nil, err
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
		if offP95 != nil {
			row := types.DigestRow{BucketDate: day, CPUUsageP95MC: *offP95}
			if offP50 != nil {
				row.CPUUsageP50MC = *offP50
			}
			off = append(off, types.KeyedDigest{Key: key, Row: row})
		} else if offP50 != nil {
			// Legacy off leg (pre-#826): present, unmeasured.
			offLegacy = append(offLegacy, types.KeyedDigest{
				Key: key,
				Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: *offP50},
			})
		}
	}
	return bh, off, offLegacy, rows.Err()
}
