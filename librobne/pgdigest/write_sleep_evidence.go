package pgdigest

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// WriteSleepEvidence persists the #760 sleep evidence legs for a cluster —
// the CLI's DB-output mode uses it so its read-back path exercises the same
// rows the server's ingestion writes. The two leg sets merge per
// (namespace, workload, workload_type, container, day): a key present in
// both sets gets both columns; a key in one set gets that column and NULL
// for the other. Replacement upserts (idempotent replay). hours (#827)
// carries the per-container-day observed-hour counts and rides the same
// upsert — a write without hours would leave NULL coverage that reads as
// unobserved and bypass the offline gate.
func WriteSleepEvidence(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, bh, off []types.KeyedDigest, hours []EvidenceHours) error {
	type row struct {
		ns, wl, wt, cn string
		day            time.Time
		bhP50, offP50  *int64
		offP95         *int64
		bhH, offH      *int16
	}
	rows := map[[5]string]*row{}
	add := func(legs []types.KeyedDigest, isBH bool) {
		for _, kd := range legs {
			k := [5]string{kd.Key.Namespace, kd.Key.Workload, kd.Key.WorkloadType, kd.Key.ContainerName, kd.Row.BucketDate.Format("2006-01-02")}
			r := rows[k]
			if r == nil {
				r = &row{ns: kd.Key.Namespace, wl: kd.Key.Workload, wt: kd.Key.WorkloadType, cn: kd.Key.ContainerName, day: kd.Row.BucketDate}
				rows[k] = r
			}
			v := kd.Row.CPUUsageP50MC
			if isBH {
				r.bhP50 = &v
			} else {
				r.offP50 = &v
				// #826: the off leg's p95 — the veto statistic.
				v95 := kd.Row.CPUUsageP95MC
				r.offP95 = &v95
			}
		}
	}
	add(bh, true)
	add(off, false)
	for _, h := range hours {
		k := [5]string{h.Namespace, h.Workload, h.WorkloadType, h.ContainerName, h.Day.Format("2006-01-02")}
		r := rows[k]
		if r == nil {
			r = &row{ns: h.Namespace, wl: h.Workload, wt: h.WorkloadType, cn: h.ContainerName, day: h.Day}
			rows[k] = r
		}
		bh, off := h.BHHours, h.OffHours
		r.bhH, r.offH = &bh, &off
	}
	if len(rows) == 0 {
		return nil
	}
	keys := make([]*row, 0, len(rows))
	for _, r := range rows {
		keys = append(keys, r)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.ns != b.ns {
			return a.ns < b.ns
		}
		if a.wl != b.wl {
			return a.wl < b.wl
		}
		if a.wt != b.wt {
			return a.wt < b.wt
		}
		if a.cn != b.cn {
			return a.cn < b.cn
		}
		return a.day.Before(b.day)
	})
	batch := &pgx.Batch{}
	queued := 0
	for _, r := range keys {
		batch.Queue(`
			INSERT INTO hcp_sleep_evidence (
				org_id, cluster_uuid, namespace, workload, workload_type,
				container_name, bucket_date, bh_cpu_p50_mc, off_cpu_p50_mc, off_cpu_p95_mc,
				bh_observed_hours, off_observed_hours
			) VALUES ($1,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, bucket_date)
			DO UPDATE SET bh_cpu_p50_mc = EXCLUDED.bh_cpu_p50_mc,
				off_cpu_p50_mc = EXCLUDED.off_cpu_p50_mc,
				off_cpu_p95_mc = EXCLUDED.off_cpu_p95_mc,
				bh_observed_hours = EXCLUDED.bh_observed_hours,
				off_observed_hours = EXCLUDED.off_observed_hours`,
			orgID, clusterUUID, r.ns, r.wl, r.wt, r.cn, r.day, r.bhP50, r.offP50, r.offP95, r.bhH, r.offH)
		queued++
		if queued >= 2000 {
			if err := flushEvidenceBatch(ctx, pool, batch, queued); err != nil {
				return err
			}
			batch = &pgx.Batch{}
			queued = 0
		}
	}
	return flushEvidenceBatch(ctx, pool, batch, queued)
}

func flushEvidenceBatch(ctx context.Context, pool *pgxpool.Pool, batch *pgx.Batch, queued int) error {
	if queued == 0 {
		return nil
	}
	br := pool.SendBatch(ctx, batch)
	defer func() { _ = br.Close() }()
	for range queued {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}
