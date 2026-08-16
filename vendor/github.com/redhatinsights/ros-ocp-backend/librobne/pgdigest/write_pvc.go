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

// WritePVCDigests upserts already-computed PVC days with last-write-wins
// (not ingest GREATEST/LEAST merge). Empty grouped is a no-op.
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
				org_id = EXCLUDED.org_id,
				persistentvolume = EXCLUDED.persistentvolume,
				storageclass = EXCLUDED.storageclass,
				last_seen_pod = EXCLUDED.last_seen_pod,
				vm_name = EXCLUDED.vm_name,
				capacity_bytes = EXCLUDED.capacity_bytes,
				request_bytes = EXCLUDED.request_bytes,
				usage_bytes_min = EXCLUDED.usage_bytes_min,
				usage_bytes_max = EXCLUDED.usage_bytes_max,
				usage_bytes_avg = EXCLUDED.usage_bytes_avg,
				sample_count = EXCLUDED.sample_count`,
		d.BucketDate, orgID, clusterUUID, d.Namespace,
		d.PVC, d.PV, d.StorageClass,
		d.CapacityBytes, d.RequestBytes,
		d.UsageBytesMin, d.UsageBytesMax, d.UsageBytesAvg,
		d.SampleCount, d.LastSeenPod, d.VMName,
	)
}
