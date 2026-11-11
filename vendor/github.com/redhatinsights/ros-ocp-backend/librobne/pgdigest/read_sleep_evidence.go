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
// EvidenceHours is one container-day's per-leg observed-hour counts (#827),
// read alongside the value legs for the CLI's coverage input, and written
// by the CLI's DB mode alongside the legs (the hours must ride the same
// upsert as the values — a write without hours would leave NULL coverage
// that the read treats as unobserved).
type EvidenceHours struct {
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
	Day           time.Time
	BHHours       int16
	OffHours      int16
}

// ReadSleepEvidence loads the sleep evidence legs for the CLI's DB mode.
// scheduleFingerprint (#831) is the CURRENTLY resolved schedule's identity;
// the returned attested flag is true only when EVERY row in the window
// carries it (NULL pre-#831 rows read unattested) — the caller suppresses
// the advisory on mismatch, mirroring the server's gate.
func ReadSleepEvidence(ctx context.Context, q Querier, orgID, clusterUUID string, start, end time.Time, scheduleFingerprint string) (bh, off, offLegacy []types.KeyedDigest, hours []EvidenceHours, attested bool, err error) {
	rows, err := q.Query(ctx, `
		SELECT namespace, workload, workload_type, container_name, bucket_date,
			bh_cpu_p50_mc, off_cpu_p50_mc, off_cpu_p95_mc,
			bh_observed_hours, off_observed_hours, schedule_fingerprint
		FROM hcp_sleep_evidence
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date < $4`,
		orgID, clusterUUID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, nil, nil, nil, false, err
	}
	defer rows.Close()
	attested = true
	rowCount := 0
	for rows.Next() {
		var ns, wl, wt, cn string
		var day time.Time
		var bhP50, offP50, offP95 *int64
		var bhH, offH *int16
		var fp *string
		if err := rows.Scan(&ns, &wl, &wt, &cn, &day, &bhP50, &offP50, &offP95, &bhH, &offH, &fp); err != nil {
			return nil, nil, nil, nil, false, err
		}
		rowCount++
		// #831: any row not matching the current fingerprint (including
		// NULL pre-#831 rows) unattests the whole window — a re-ingested
		// sibling cannot mask a stale row.
		if fp == nil || *fp != scheduleFingerprint {
			attested = false
		}
		// #827: observed hours for EVERY row, COALESCEd to 0 when the
		// columns are NULL (pre-#827 rows) — a missing measurement is 0
		// observed hours, never "no information" that skips the floor:
		// the server's loader reads the same rows the same way
		// (MIN(COALESCE(·,0))). A row with hours present carries its
		// real counts; the workload-grain MIN happens in the CLI's
		// coverage assembly, the same collapse as SleepSums.
		var b, o int16
		if bhH != nil {
			b = *bhH
		}
		if offH != nil {
			o = *offH
		}
		hours = append(hours, EvidenceHours{Namespace: ns, Workload: wl, WorkloadType: wt, ContainerName: cn, Day: day, BHHours: b, OffHours: o})
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
	if rowCount == 0 {
		attested = false // no evidence at all — nothing attests the window
	}
	return bh, off, offLegacy, hours, attested, rows.Err()
}
