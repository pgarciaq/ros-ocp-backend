package pgdigest

import (
	"context"
	"fmt"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
)

// ReadPVCDigests loads PVC daily rows in [start, end] for this org+cluster.
// Unique key has no org_id; org_id is still filtered. Empty result is not an error.
func ReadPVCDigests(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time) (map[pvc.PVCKey][]pvc.PVCDigestRow, error) {
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return nil, err
	}
	if err := requireQuerier(q); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT bucket_date, namespace, persistentvolumeclaim,
			COALESCE(persistentvolume, ''), COALESCE(storageclass, ''),
			COALESCE(capacity_bytes, 0), COALESCE(request_bytes, 0),
			COALESCE(usage_bytes_min, 0), COALESCE(usage_bytes_max, 0), COALESCE(usage_bytes_avg, 0),
			COALESCE(sample_count, 0), COALESCE(last_seen_pod, ''), COALESCE(vm_name, '')
		FROM daily_pvc_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date <= $4
		ORDER BY namespace, persistentvolumeclaim, bucket_date`,
		orgID, clusterUUID, start.Format(dateLayout), end.Format(dateLayout))
	if err != nil {
		return nil, fmt.Errorf("pgdigest: query PVC digests: %w", err)
	}
	defer rows.Close()
	out := make(map[pvc.PVCKey][]pvc.PVCDigestRow)
	for rows.Next() {
		var d pvc.PVCDigestRow
		if err := rows.Scan(
			&d.BucketDate, &d.Namespace, &d.PVC,
			&d.PV, &d.StorageClass,
			&d.CapacityBytes, &d.RequestBytes,
			&d.UsageBytesMin, &d.UsageBytesMax, &d.UsageBytesAvg,
			&d.SampleCount, &d.LastSeenPod, &d.VMName,
		); err != nil {
			return nil, fmt.Errorf("pgdigest: scan PVC digest: %w", err)
		}
		key := pvc.PVCKey{Namespace: d.Namespace, PVC: d.PVC}
		out[key] = append(out[key], d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgdigest: iterate PVC digests: %w", err)
	}
	return out, nil
}
