package correlate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// W8 non-prod sleep-candidate advisory (#716, ADR-0348). Advisory only —
// ROS never sleeps anything. The declaration (tier label) classifies; the
// business-hours signature gates: a labeled non-prod HC running overnight
// workloads gets no advisory, because sleeping it would break them.

const (
	// sleepVerdict is the W8 sleep-candidate advisory verdict.
	sleepVerdict = "schedule_nonprod_sleep"
	// hcTierNonProd is the insights.cost-management/hc-tier label value
	// that declares an HC non-prod (ADR-0348 §2).
	hcTierNonProd = "non-prod"
	// sleepScheduleSentinel mirrors business_hours_schedules' all-zero
	// cluster_uuid (org-wide default rows).
	sleepScheduleSentinel = "00000000-0000-0000-0000-000000000000"
)

// sleepSchedule is the resolved org/cluster-level business-hours window.
// Namespace-scoped rows never define an HC's hours (ADR-0348 §4): with
// per-namespace windows, "the HC's business hours" is ill-defined.
type sleepSchedule struct {
	Timezone string
	Days     []string
	StartMin int // minutes from midnight
	EndMin   int
}

// weeklyOutsideHours is the non-business hours per week for the reclaim
// estimate. Windows crossing midnight (end <= start) wrap.
func (s sleepSchedule) weeklyOutsideHours() float64 {
	window := s.EndMin - s.StartMin
	if window <= 0 {
		window += 24 * 60
	}
	return float64(len(s.Days)) * float64(24*60-window) / 60.0
}

func (s sleepSchedule) describe() string {
	return fmt.Sprintf("%s %02d:%02d-%02d:%02d %s",
		strings.Join(s.Days, ","), s.StartMin/60, s.StartMin%60, s.EndMin/60, s.EndMin%60, s.Timezone)
}

// loadLatestHCTier reads the tier label value on the most-recent complete
// snapshot row for the HC — LATEST-WINS, including empty (ADR-0348 §3).
// Snapshot rows are repeated observations of one admin-controlled label,
// not independent votes: majority-wins would keep recommending sleep for a
// cluster just re-classified non-prod→prod (the old value holds the
// majority for up to the window), and latest-non-empty would mask label
// removal. Recency is the correct resolver for intent; #621's
// all-rows-agree strictness governs identity, not mutable labels.
func loadLatestHCTier(ctx context.Context, pool *pgxpool.Pool, orgID, hcID string) (string, error) {
	var tier string
	err := pool.QueryRow(ctx, `
		SELECT hc_tier FROM manifest_hcp_snapshots
		WHERE org_id = $1 AND hosted_cluster_id = $2 AND complete
		ORDER BY observed_at DESC LIMIT 1`,
		orgID, hcID).Scan(&tier)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", nil
		}
		return "", fmt.Errorf("load latest hc tier: %w", err)
	}
	return tier, nil
}

// resolveSleepSchedule returns the deepest non-namespace business-hours
// schedule for the hosted cluster (cluster-scoped row over the org default),
// keyed to the HC's own cluster UUID — the same key the digest pipeline
// resolved when producing its BH streams. Nil when none exists: no schedule
// is invented (owner decision 2026-10-30).
func resolveSleepSchedule(ctx context.Context, pool *pgxpool.Pool, orgID, hcID string) (*sleepSchedule, error) {
	var s sleepSchedule
	var startMin, endMin int
	err := pool.QueryRow(ctx, `
		SELECT timezone, days, EXTRACT(EPOCH FROM start_time)/60::int, EXTRACT(EPOCH FROM end_time)/60::int
		FROM business_hours_schedules
		WHERE org_id = $1 AND enabled AND namespace = ''
		  AND (cluster_uuid = $2::uuid OR cluster_uuid = '`+sleepScheduleSentinel+`'::uuid)
		ORDER BY (cluster_uuid = $2::uuid) DESC, updated_at DESC
		LIMIT 1`,
		orgID, hcID).Scan(&s.Timezone, &s.Days, &startMin, &endMin)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve sleep schedule: %w", err)
	}
	s.StartMin, s.EndMin = startMin, endMin
	return &s, nil
}

// sleepKey identifies one (namespace, workload) pair.
type sleepKey struct {
	ns string
	wl string
}

// sleepWorkloadDay is one (day, namespace, workload) pair across the two
// digest streams.
type sleepWorkloadDay struct {
	bhCPU       int64
	allCPU      int64
	bhMeasured  bool
	allMeasured bool
}

// loadSleepSignature loads per (day, namespace, workload) CPU for both
// streams on the hosted cluster (N-leg convention: cluster_uuid is the HC's
// own ID). All namespaces load; the platform split happens in Go, shared
// with the zombie rule (including the synced cost-groups set).
func loadSleepSignature(ctx context.Context, pool *pgxpool.Pool, orgID, hcID string, start, end time.Time) (map[string]map[sleepKey]*sleepWorkloadDay, error) {
	rows, err := pool.Query(ctx, `
		SELECT bucket_date::text, namespace, workload, schedule_type,
			SUM(cpu_usage_p50_mc), COUNT(cpu_usage_p50_mc)
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date < $4
		GROUP BY 1, 2, 3, 4`,
		orgID, hcID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("load sleep signature: %w", err)
	}
	defer rows.Close()
	out := map[string]map[sleepKey]*sleepWorkloadDay{}
	for rows.Next() {
		var day, ns, wl, sched string
		var total *int64
		var nmeas int64
		if err := rows.Scan(&day, &ns, &wl, &sched, &total, &nmeas); err != nil {
			return nil, fmt.Errorf("scan sleep signature: %w", err)
		}
		if out[day] == nil {
			out[day] = map[sleepKey]*sleepWorkloadDay{}
		}
		k := sleepKey{ns: ns, wl: wl}
		if out[day][k] == nil {
			out[day][k] = &sleepWorkloadDay{}
		}
		d := out[day][k]
		if total != nil {
			if sched == "business_hours" {
				d.bhCPU = *total
				if nmeas > 0 {
					d.bhMeasured = true
				}
			} else {
				d.allCPU = *total
				if nmeas > 0 {
					d.allMeasured = true
				}
			}
		}
	}
	return out, rows.Err()
}

// evalSleepSignature applies the ADR-0348 §5 predicate: every covered day
// (BOTH streams measured — partial BH coverage reads unknown, never
// zero-activity) has at least one user workload at/above the BH activity
// floor (active in hours) and no user workload whose outside-BH CPU is
// at/above the outside floor (idle outside). Partition with the zombie rule
// holds by construction: idle-always matches zombie, never this.
func evalSleepSignature(days map[string]map[sleepKey]*sleepWorkloadDay, windowStart, windowEnd time.Time, bhFloor, outsideFloor int64, syncedExact map[string]bool, syncedPrefixes []string) (bhPeak, outsidePeak int64, match, known bool) {
	for d := windowStart; d.Before(windowEnd); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format("2006-01-02")
		rows, ok := days[key]
		if !ok {
			return 0, 0, false, false
		}
		covered := false
		dayBHActive := false
		for k, r := range rows {
			if !r.bhMeasured || !r.allMeasured {
				continue
			}
			covered = true
			if !isZombieUserNamespace(k.ns, syncedExact, syncedPrefixes) {
				continue
			}
			outside := r.allCPU - r.bhCPU
			if r.bhCPU > bhPeak {
				bhPeak = r.bhCPU
			}
			if outside > outsidePeak {
				outsidePeak = outside
			}
			if r.bhCPU >= bhFloor {
				dayBHActive = true
			}
			if outside >= outsideFloor {
				return 0, 0, false, true
			}
		}
		if !covered {
			return 0, 0, false, false
		}
		if !dayBHActive {
			return 0, 0, false, true
		}
	}
	return bhPeak, outsidePeak, true, true
}

// loadHCWorkerNodes sums the latest spec_replicas per pool for the HC from
// the NodePool rollups — authoritative pool counts for the reclaim estimate.
func loadHCWorkerNodes(ctx context.Context, pool *pgxpool.Pool, orgID, hcID string) (int, error) {
	var nodes int64
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(spec_replicas), 0) FROM (
			SELECT DISTINCT ON (pool_name) pool_name, spec_replicas
			FROM hosted_nodepool_rollups
			WHERE org_id = $1 AND hc_cluster_id = $2
			ORDER BY pool_name, collected_at DESC
		) latest`,
		orgID, hcID).Scan(&nodes)
	if err != nil {
		return 0, fmt.Errorf("load hc worker nodes: %w", err)
	}
	return int(nodes), nil
}

// writeSleepAdvisory upserts one sleep-candidate advisory, mirroring the
// zombie write semantics (in-place daily refresh, expiry sweep shared).
func writeSleepAdvisory(ctx context.Context, pool *pgxpool.Pool, orgID, hcID, mgmtCluster string, bhPeak, outsidePeak int64, sched *sleepSchedule, workerNodes int, windowStart, windowEnd time.Time, p Policy) error {
	reclaim := float64(workerNodes) * sched.weeklyOutsideHours()
	signals := []string{
		"hc:" + hcID,
		"declared_tier:" + hcTierNonProd,
		fmt.Sprintf("bh_active_peak_mc:%d", bhPeak),
		fmt.Sprintf("outside_bh_peak_mc:%d", outsidePeak),
		"bh_schedule:" + sched.describe(),
		fmt.Sprintf("worker_nodes:%d", workerNodes),
		fmt.Sprintf("reclaim_node_hours_per_week:%.0f", reclaim),
		"action:nodepool-scale-to-zero + pausedUntil (runbook); CP keeps running; wake ~= 20 min reprovision",
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO hcp_correlation_advisories (
			org_id, hc_cluster_id, management_cluster_uuid,
			window_start, window_end, verdict, confidence,
			h_p99_s, h_threshold_s, c_ratio, n_signals, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,'high',$7,$8,NULL,$9,$10)
		ON CONFLICT (org_id, hc_cluster_id, window_start)
		DO UPDATE SET verdict = EXCLUDED.verdict, confidence = EXCLUDED.confidence,
			h_p99_s = EXCLUDED.h_p99_s, h_threshold_s = EXCLUDED.h_threshold_s,
			c_ratio = EXCLUDED.c_ratio, n_signals = EXCLUDED.n_signals,
			expires_at = EXCLUDED.expires_at`,
		orgID, hcID, mgmtCluster,
		windowStart.UTC(), windowEnd.UTC(), sleepVerdict,
		float64(bhPeak), float64(outsidePeak), signals,
		windowEnd.UTC().Add(time.Duration(p.AdvisoryExpiryHours)*time.Hour))
	if err != nil {
		return fmt.Errorf("write sleep advisory: %w", err)
	}
	metrics.HCPAdvisoriesTotal.WithLabelValues(sleepVerdict).Inc()
	return nil
}

// evaluateSleepForHC fires at most one sleep-candidate advisory per HC per
// day. Gates: declared tier (latest-wins), org/cluster-level BH schedule,
// signature (every covered day BH-active and outside-idle).
func evaluateSleepForHC(ctx context.Context, pool *pgxpool.Pool, c zombieCandidate, windowStart, windowEnd time.Time, p Policy) (fired bool, err error) {
	tier, err := loadLatestHCTier(ctx, pool, c.orgID, c.hcID)
	if err != nil || tier != hcTierNonProd {
		return false, err
	}
	sched, err := resolveSleepSchedule(ctx, pool, c.orgID, c.hcID)
	if err != nil || sched == nil {
		return false, err
	}
	signature, err := loadSleepSignature(ctx, pool, c.orgID, c.hcID, windowStart, windowEnd)
	if err != nil {
		return false, err
	}
	syncedExact, syncedPrefixes := loadSyncedPlatformSet(ctx, pool, c.orgID)
	bhPeak, outsidePeak, match, known := evalSleepSignature(signature, windowStart, windowEnd,
		int64(p.SleepBHActivityFloorMC), int64(p.SleepOutsideBHIdleFloorMC), syncedExact, syncedPrefixes)
	if !known || !match {
		return false, nil
	}
	workerNodes, err := loadHCWorkerNodes(ctx, pool, c.orgID, c.hcID)
	if err != nil {
		return false, err
	}
	if err := writeSleepAdvisory(ctx, pool, c.orgID, c.hcID, c.clusterUUID, bhPeak, outsidePeak, sched, workerNodes, windowStart, windowEnd, p); err != nil {
		return false, err
	}
	return true, nil
}

// RunSleepCandidateCycle evaluates the W8 non-prod sleep signature for every
// HC with a fresh association snapshot (shared enumeration with the zombie
// rule — same never-fail-on-evidence contract; evidence problems are
// silence).
func RunSleepCandidateCycle(ctx context.Context, pool *pgxpool.Pool) (fired int, err error) {
	windowEnd := time.Now().UTC().Truncate(24 * time.Hour)
	snapshotCutoff := time.Now().UTC().Add(-time.Duration(zombieSnapshotFreshHours) * time.Hour)
	cands, err := enumerateZombieCandidates(ctx, pool, snapshotCutoff)
	if err != nil {
		if pgrec.IsUndefinedTable(err) {
			logging.GetLogger().Warnf("correlator: sleep candidate tables unavailable (pre-migration, skipping): %v", err)
			return 0, nil
		}
		return 0, err
	}
	for _, c := range cands {
		if err := ctx.Err(); err != nil {
			return fired, err
		}
		p := policyForOrg(ctx, pool, c.orgID)
		sleepWindow := p.SleepWindowDays
		if sleepWindow < 1 || sleepWindow > 90 {
			sleepWindow = 14
		}
		windowStart := windowEnd.AddDate(0, 0, -sleepWindow)
		ok, err := evaluateSleepForHC(ctx, pool, c, windowStart, windowEnd, p)
		if err != nil {
			logging.ForOrg(c.orgID, c.hcID).Warnf("correlator: sleep candidate evaluation failed, silent: %v", err)
			continue
		}
		if ok {
			fired++
		}
	}
	return fired, nil
}
