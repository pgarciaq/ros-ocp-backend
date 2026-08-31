package pgdigest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
)

type pvcWrite struct {
	Row pvc.PVCDigestRow
}

// WritePVCDigests upserts already-computed PVC days with the same
// GREATEST/LEAST / weighted-avg / sample_count+= merge as ingest
// (internal/ingestion/pvc.go). Empty grouped is a no-op. Does not rewrite
// org_id on conflict. A second write of the same full day doubles sample_count.
func WritePVCDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, grouped map[pvc.PVCKey][]pvc.PVCDigestRow) error {
	rows := flattenPVCWrites(grouped)
	if len(rows) == 0 {
		return nil
	}
	if err := requireOrgCluster(orgID, clusterUUID); err != nil {
		return err
	}
	months := make([]time.Time, len(rows))
	for i, r := range rows {
		months[i] = r.Row.BucketDate
	}
	if err := ensureRangePartitions(ctx, pool, "daily_pvc_digests", months); err != nil {
		return err
	}
	return withWriteTx(ctx, pool, func(tx pgx.Tx) error {
		if err := flushQueued(ctx, tx, len(rows), func(batch *pgx.Batch, i int) {
			queuePVCInsert(batch, orgID, clusterUUID, rows[i].Row)
		}); err != nil {
			return fmt.Errorf("upsert PVC digest: %w", err)
		}
		return nil
	})
}

func flattenPVCWrites(grouped map[pvc.PVCKey][]pvc.PVCDigestRow) []pvcWrite {
	var out []pvcWrite
	for _, days := range grouped {
		for _, row := range days {
			out = append(out, pvcWrite{Row: row})
		}
	}
	slices.SortFunc(out, func(a, b pvcWrite) int {
		if c := cmp.Compare(a.Row.Namespace, b.Row.Namespace); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Row.PVC, b.Row.PVC); c != 0 {
			return c
		}
		return a.Row.BucketDate.Compare(b.Row.BucketDate)
	})
	return out
}

func queuePVCInsert(batch *pgx.Batch, orgID, clusterUUID string, d pvc.PVCDigestRow) {
	batch.Queue(`
			INSERT INTO daily_pvc_digests (
				bucket_date, org_id, cluster_uuid, namespace,
				persistentvolumeclaim, persistentvolume, storageclass,
				capacity_bytes, request_bytes,
				usage_bytes_min, usage_bytes_max, usage_bytes_avg,
				sample_count, last_seen_pod, vm_name
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			ON CONFLICT (cluster_uuid, namespace, persistentvolumeclaim, bucket_date)
			DO UPDATE SET
				persistentvolume = EXCLUDED.persistentvolume,
				storageclass = EXCLUDED.storageclass,
				last_seen_pod = CASE
					WHEN EXCLUDED.last_seen_pod != '' THEN EXCLUDED.last_seen_pod
					ELSE daily_pvc_digests.last_seen_pod
				END,
				vm_name = CASE
					WHEN EXCLUDED.vm_name != '' THEN EXCLUDED.vm_name
					ELSE daily_pvc_digests.vm_name
				END,
				capacity_bytes = GREATEST(daily_pvc_digests.capacity_bytes, EXCLUDED.capacity_bytes),
				request_bytes = GREATEST(daily_pvc_digests.request_bytes, EXCLUDED.request_bytes),
				usage_bytes_min = LEAST(daily_pvc_digests.usage_bytes_min, EXCLUDED.usage_bytes_min),
				usage_bytes_max = GREATEST(daily_pvc_digests.usage_bytes_max, EXCLUDED.usage_bytes_max),
				usage_bytes_avg = (daily_pvc_digests.usage_bytes_avg * daily_pvc_digests.sample_count + EXCLUDED.usage_bytes_avg * EXCLUDED.sample_count)
					/ NULLIF(daily_pvc_digests.sample_count + EXCLUDED.sample_count, 0),
				sample_count = daily_pvc_digests.sample_count + EXCLUDED.sample_count`,
		d.BucketDate, orgID, clusterUUID, d.Namespace,
		d.PVC, d.PV, d.StorageClass,
		d.CapacityBytes, d.RequestBytes,
		d.UsageBytesMin, d.UsageBytesMax, d.UsageBytesAvg,
		d.SampleCount, d.LastSeenPod, d.VMName,
	)
}
