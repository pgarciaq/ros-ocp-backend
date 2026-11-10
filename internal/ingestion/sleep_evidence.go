package ingestion

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
)

// maxSleepEvidenceBatch mirrors pgdigest's batch cap.
const maxSleepEvidenceBatch = 2000

// sleepEvidenceRow is one container-day's in-window and off-window CPU p50
// (#760): the direct off-hours evidence for the W8 sleep predicate. A NULL
// leg means the container had no samples in that window that day — the
// #761 coverage shape, carried per container.
type sleepEvidenceRow struct {
	OrgID         string
	ClusterUUID   string
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
	BucketDate    time.Time
	BHCPUP50MC    *int64
	OffCPUP50MC   *int64
	// OffCPUP95MC is the off-window p95 (#826): the veto statistic. A
	// median hides minority-of-window activity (6-of-16-hours nightly
	// work reads p50=0); the house convention for CPU-usage activity
	// evidence is p95 (#747's identical zombie-rule migration).
	OffCPUP95MC *int64
	// Observed-hour coverage per leg (#827): the count of distinct UTC
	// hour buckets with at least one sample. Two observed hours per day
	// must not read as complete evidence over an unobserved window.
	BHObservedHours  *int16
	OffObservedHours *int16
}

// buildSleepEvidenceRows computes per-container-day in-window and off-window
// p50s from the all-hours groups' complete samples (#760). Only the all-hours
// groups carry every sample (the business_hours groups are pre-filtered and
// fractionally weighted when off_hours_weight > 0); splitting them by the
// schedule window yields exact complement legs through the canonical digest
// machinery. Rows are written only when a schedule is enabled for the
// namespace — the same condition as the business_hours digest stream.
func buildSleepEvidenceRows(grouped map[DigestKey][]metricSample, cache *bhschedule.Cache) []sleepEvidenceRow {
	if cache == nil {
		return nil
	}
	var out []sleepEvidenceRow
	for key, group := range grouped {
		if key.ScheduleType != ScheduleTypeAllHours {
			continue
		}
		// #760: split at the CLUSTER-scope schedule — the sleep predicate
		// resolves the HC's cluster/org schedule (resolveSleepSchedule) and
		// its question is "is anything active outside THAT window?". A
		// wider namespace override would split the off leg at the wider
		// window and under-veto hours the advice would sleep away; the
		// cluster split is the safety-correct grain (a narrower namespace
		// override only makes the off leg MORE inclusive — conservative).
		sched := cache.ResolveCluster()
		if !sched.Enabled {
			continue
		}
		var inWindow, offWindow []metricSample
		for _, s := range group {
			if bhschedule.InBusinessHours(s.IntervalStart, sched) {
				inWindow = append(inWindow, s)
			} else {
				offWindow = append(offWindow, s)
			}
		}
		row := sleepEvidenceRow{
			OrgID:         key.OrgID,
			ClusterUUID:   key.ClusterUUID,
			Namespace:     key.Namespace,
			Workload:      key.Workload,
			WorkloadType:  key.WorkloadType,
			ContainerName: key.ContainerName,
			BucketDate:    key.BucketDate,
		}
		if len(inWindow) > 0 {
			v := cpuUsageP50MC(inWindow)
			row.BHCPUP50MC = &v
			bh := int16(distinctHours(inWindow)) //nolint:gosec // G115: distinct UTC hour buckets, ≤24 by construction
			row.BHObservedHours = &bh
		}
		if len(offWindow) > 0 {
			v := cpuUsageP50MC(offWindow)
			row.OffCPUP50MC = &v
			v95 := cpuUsageP95MC(offWindow)
			row.OffCPUP95MC = &v95
			oh := int16(distinctHours(offWindow)) //nolint:gosec // G115: distinct UTC hour buckets, ≤24 by construction
			row.OffObservedHours = &oh
		}
		if row.BHCPUP50MC == nil && row.OffCPUP50MC == nil {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.OrgID != b.OrgID {
			return a.OrgID < b.OrgID
		}
		if a.ClusterUUID != b.ClusterUUID {
			return a.ClusterUUID < b.ClusterUUID
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Workload != b.Workload {
			return a.Workload < b.Workload
		}
		if a.WorkloadType != b.WorkloadType {
			return a.WorkloadType < b.WorkloadType
		}
		if a.ContainerName != b.ContainerName {
			return a.ContainerName < b.ContainerName
		}
		return a.BucketDate.Before(b.BucketDate)
	})
	return out
}

// distinctHours counts the distinct UTC hour buckets represented in the
// sample set (#827): the observation-coverage measure. Hour-of-day suffices
// — the group is one container-day.
func distinctHours(samples []metricSample) int {
	seen := make(map[int]struct{}, len(samples))
	for _, s := range samples {
		seen[s.IntervalStart.UTC().Hour()] = struct{}{}
	}
	return len(seen)
}

// cpuUsageP95MC is the CPU-usage p95 through the same percentile core —
// the #826 off-leg statistic (the veto gate; see sleepEvidenceRow).
func cpuUsageP95MC(samples []metricSample) int64 {
	values := make([]int64, len(samples))
	for i, s := range samples {
		values[i] = s.CPUUsageMC
	}
	return ComputeDigest(values).P95
}

// cpuUsageP50MC is the CPU-usage p50 through the digest machinery's own
// percentile core (ComputeDigest) — the same math the full digest uses for
// that exact column, one field instead of six (the evidence needs no other
// column; the full compute would double scheduled-scope digest CPU).
func cpuUsageP50MC(samples []metricSample) int64 {
	values := make([]int64, len(samples))
	for i, s := range samples {
		values[i] = s.CPUUsageMC
	}
	return ComputeDigest(values).P50
}

// upsertSleepEvidenceOnSender writes the #760 evidence rows on the same
// batch sender as the digests — every ingest path funnels through the
// digest writer (pipeline, single-tx, staged-chunk), so this hook covers
// them all. Replacement upserts at container-day grain keep replayed
// manifests idempotent (a replay converges to the same row).
func upsertSleepEvidenceOnSender(ctx context.Context, sender pgxBatchSender, grouped map[DigestKey][]metricSample, cache *bhschedule.Cache) error {
	rows := buildSleepEvidenceRows(grouped, cache)
	if len(rows) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	queued := 0
	for _, r := range rows {
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
			r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType,
			r.ContainerName, r.BucketDate, r.BHCPUP50MC, r.OffCPUP50MC, r.OffCPUP95MC,
			r.BHObservedHours, r.OffObservedHours)
		queued++
		if queued >= maxSleepEvidenceBatch {
			if err := flushQueuedBatch(ctx, sender, batch, queued); err != nil {
				return err
			}
			batch = &pgx.Batch{}
			queued = 0
		}
	}
	return flushQueuedBatch(ctx, sender, batch, queued)
}
