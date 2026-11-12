package correlate

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
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
// evidence legs (#760): the in-window p50 sum and the off-window p50 sum —
// direct measures, never a subtraction.
type sleepWorkloadDay struct {
	bhCPU       int64
	offCPU      int64
	bhMeasured  bool
	offMeasured bool
	// Per-leg observed-hour coverage (#827): MIN over the workload's
	// container rows, a missing leg on any container reading as 0 —
	// a fully-observed sibling cannot mask an under-observed one.
	bhObservedHours  int16
	offObservedHours int16
	// scheduleAttested (#831): every container row of the workload-day
	// carries the currently resolved schedule's fingerprint.
	scheduleAttested bool
}

// loadSleepEvidence loads per (day, namespace, workload) CPU for both
// evidence legs from hcp_sleep_evidence (#760, #826): the in-window p50 sum
// and the off-window p95 sum — direct measures of each side of the
// schedule, never a subtraction. The off leg's statistic is p95 (#826): a
// median hides minority-of-window activity, and the house convention for
// CPU-usage activity evidence is p95 (#747's zombie-rule precedent); the
// BH-activity leg keeps p50 — its failure direction (underestimating
// activity) suppresses advice, which is safe. The workload-grain SUM of
// per-container p95s is a conservative upper bound for the veto direction.
// NULL legs (a workload with no samples in one window, or pre-#826 rows
// carrying no p95) leave the flag false — the #761 coverage shape; the
// cold start re-reads unknown until re-ingestion.
// N-leg convention: cluster_uuid is the HC's own ID. All namespaces load;
// the platform split happens in Go, shared with the zombie rule (including
// the synced cost-groups set).
func loadSleepEvidence(ctx context.Context, pool *pgxpool.Pool, orgID, hcID string, start, end time.Time, scheduleFingerprint string) (map[string]map[sleepKey]*sleepWorkloadDay, error) {
	rows, err := pool.Query(ctx, `
		SELECT bucket_date::text, namespace, workload,
			SUM(bh_cpu_p50_mc), COUNT(bh_cpu_p50_mc),
			SUM(off_cpu_p95_mc), COUNT(off_cpu_p95_mc), COUNT(*),
			MIN(COALESCE(bh_observed_hours, 0)), MIN(COALESCE(off_observed_hours, 0)),
			NOT bool_or(schedule_fingerprint IS DISTINCT FROM $5)
		FROM hcp_sleep_evidence
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date < $4
		GROUP BY 1, 2, 3`,
		orgID, hcID, start.Format("2006-01-02"), end.Format("2006-01-02"), scheduleFingerprint)
	if err != nil {
		return nil, fmt.Errorf("load sleep evidence: %w", err)
	}
	defer rows.Close()
	out := map[string]map[sleepKey]*sleepWorkloadDay{}
	for rows.Next() {
		var day, ns, wl string
		var bhTotal, offTotal *int64
		var bhN, offN, totalN int64
		var bhHours, offHours *int16
		var attested bool
		if err := rows.Scan(&day, &ns, &wl, &bhTotal, &bhN, &offTotal, &offN, &totalN, &bhHours, &offHours, &attested); err != nil {
			return nil, fmt.Errorf("scan sleep evidence: %w", err)
		}
		if out[day] == nil {
			out[day] = map[sleepKey]*sleepWorkloadDay{}
		}
		k := sleepKey{ns: ns, wl: wl}
		d := out[day][k]
		if d == nil {
			d = &sleepWorkloadDay{}
			out[day][k] = d
		}
		if bhTotal != nil {
			d.bhCPU = *bhTotal
			if bhN > 0 {
				d.bhMeasured = true
			}
		}
		if offTotal != nil {
			d.offCPU = *offTotal
			// #826 review (partial migration): the leg is measured only
			// when EVERY container row of the workload-day carries a p95 —
			// a migrated quiet sibling must not mask a legacy row's
			// hidden activity (COUNT(p95) < COUNT(*) reads unmeasured →
			// the #761 gate → unknown day).
			if offN > 0 && offN == totalN {
				d.offMeasured = true
			}
		}
		// #827: observed-hour coverage — MIN over the workload's container
		// rows, with a missing leg on any container reading as 0 observed
		// hours (COALESCE in the SQL): a fully-observed sibling must not
		// mask an under-observed one (#826's F1 lesson, one level down).
		// The scan is defensive about NULL only; GROUP BY rows always
		// carry a MIN over COALESCEd values.
		d.bhObservedHours, d.offObservedHours = 0, 0
		if bhHours != nil {
			d.bhObservedHours = *bhHours
		}
		if offHours != nil {
			d.offObservedHours = *offHours
		}
		// #831: split-schedule attestation — every row of the workload-day
		// must carry the CURRENTLY resolved schedule's fingerprint. Any
		// mismatch (an old-window row, a mixed-version window, or NULL
		// pre-#831 rows) reads unattested → unknown; a re-ingested sibling
		// cannot mask a stale row (the #826 completeness rule, one level
		// up). Self-healing on re-ingestion.
		d.scheduleAttested = attested
	}
	return out, rows.Err()
}

// evalSleepSignature applies the ADR-0348 §5 predicate (amended #760,
// #761): every covered day has at least one user workload at/above the BH
// activity floor (active in hours) and no user workload whose off-window
// CPU is at/above the outside floor (idle outside). A day is covered when
// its evidence legs are measured — partial coverage reads unknown, never
// zero-activity (#761, workload grain). Days the schedule does not apply to
// (a weekday schedule's weekends: no in-window hours exist, so the producer
// emits no BH evidence — the whole day is outside the window the advice
// would sleep) gate on the off leg alone: the BH-activity requirement is
// vacuous there, and off-window activity still vetoes. Partition with the
// zombie rule holds by construction: idle-always matches zombie, never
// this.
func evalSleepSignature(days map[string]map[sleepKey]*sleepWorkloadDay, windowStart, windowEnd time.Time, bhFloor, outsideFloor int64, minCoverageFrac float64, sched *sleepSchedule, syncedExact map[string]bool, syncedPrefixes []string) (bhPeak, outsidePeak int64, match, known bool) {
	// The bhschedule form of the resolved schedule: the identical rule
	// the ingestion writer's leg split applies (#827 review — the
	// possible-hours derivation must classify with the writer, never a
	// re-derivation that can disagree). Nil schedule → disabled
	// placeholder → zero BH hours (floor vacuous, off = 24h).
	bs := bhschedule.Schedule{}
	if sched != nil {
		bs = bhschedule.Schedule{
			Timezone:  sched.Timezone,
			Days:      sched.Days,
			Enabled:   true,
			StartTime: fmt.Sprintf("%02d:%02d", sched.StartMin/60, sched.StartMin%60),
			EndTime:   fmt.Sprintf("%02d:%02d", sched.EndMin/60, sched.EndMin%60),
		}
	}
	for d := windowStart; d.Before(windowEnd); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format("2006-01-02")
		rows, ok := days[key]
		if !ok {
			return 0, 0, false, false
		}
		// #845: applicability derives from the classified possible hours
		// — the same classification the writer's leg split applies — not
		// a separate UTC-weekday check. The producer classifies in the
		// schedule's LOCAL timezone, so a timezone-shifted schedule's BH
		// hours can land on UTC days whose weekday the day list does not
		// name, and leave named weekdays BH-free: a UTC-weekday check
		// both demands BH evidence where no BH hours exist (the window
		// reads unknown forever) and skips the gates where BH hours do
		// exist. A day is applicable exactly when it carries BH hours;
		// #760's weekends are the zero-BH-hours case — the off leg still
		// gates safety (the advice sleeps those hours). Same-day UTC
		// schedules agree with the weekday rule bucket-for-bucket;
		// overnight schedules legitimately change on the post-midnight
		// UTC day (a Mon–Fri 22:00–06:00 UTC schedule gives Saturday six
		// BH buckets from Friday's shift while DayAllowed(Saturday) is
		// false — the same lenient-direction repair, pinned by test).
		// Sub-hour windows contained in one clock hour classify zero
		// bucket starts, so the minute-granular intersection check keeps
		// them applicable (#845 review P1). Nil schedule keeps the BH
		// gates on (defense in depth under the cycle's nil early-return):
		// the writer emits no rows for a disabled/absent schedule, so
		// such days read unknown — never advice-without-schedule.
		bhPossible := 0
		for h := 0; h < 24; h++ {
			if bhschedule.InBusinessHours(d.UTC().Add(time.Duration(h)*time.Hour), bs) {
				bhPossible++
			}
		}
		dayApplicable := sched == nil || bhPossible > 0 ||
			bhschedule.DayIntersectsBusinessHours(d.UTC(), bs)

		// #827: the coverage floor — each leg's observed hours must meet
		// SleepMinCoverageFrac of the leg's possible hours for that day,
		// rounded up (the H-leg minimum-observation precedent). Unknown
		// on shortfall, never zero-activity. Possible hours derive by
		// classifying each UTC hour bucket of the day against the
		// schedule — the same classification the writer's leg split
		// applies — so overnight windows (split across UTC day buckets:
		// a Mon–Fri 22:00–06:00 schedule gives Monday only its 22:00 and
		// 23:00 buckets) and non-applicable days (zero BH hours, floor
		// vacuous) are exact, never window-length arithmetic that
		// demands more than a day can produce. Fractional boundaries err
		// lenient by construction: a bucket counts as possible only when
		// its start is in-window, and observed buckets are never fewer
		// than the demand. Pre-#827 rows carry NULL hours, which the
		// loader's COALESCE reads as 0 — pre-#826 rows are then unknown
		// via the p95 rule, #826-window rows (p95 present, hours absent)
		// unknown via this floor; no separate legacy skip exists.
		offPossible := 24 - bhPossible
		minBHHours := int(math.Ceil(minCoverageFrac * float64(bhPossible)))
		minOffHours := int(math.Ceil(minCoverageFrac * float64(offPossible)))
		covered := false
		dayBHActive := false
		for k, r := range rows {
			missingBH := !r.bhMeasured
			if !dayApplicable {
				missingBH = false // no in-window hours exist that day
			}
			if missingBH || !r.offMeasured {
				// #761 (V15-11): a USER workload with an unmeasured leg
				// makes the day unknown — never silently skipped while a
				// sibling covers the day. Absence of a BH leg does not
				// prove zero off-hours usage: the producer omits
				// all-zero-BH groups by design (the normal night-only
				// shape), so the off-hours split is unknowable — unknown,
				// never zero-activity (ADR-0348 §5, workload grain).
				if isZombieUserNamespace(k.ns, syncedExact, syncedPrefixes) {
					return 0, 0, false, false
				}
				continue // system workload: leg shape irrelevant
			}
			covered = true
			if !isZombieUserNamespace(k.ns, syncedExact, syncedPrefixes) {
				continue
			}
			// #827: the coverage floor gate — a USER workload observed for
			// fewer than the floor's hours on either applicable leg makes
			// the day unknown (the sparse-evidence defect: two observed
			// hours must not read as complete coverage). System
			// workloads don't gate: they carry no user-activity evidence.
			if dayApplicable && int(r.bhObservedHours) < minBHHours {
				return 0, 0, false, false
			}
			if int(r.offObservedHours) < minOffHours {
				return 0, 0, false, false
			}
			// #831: the split-schedule attestation gate — a USER workload
			// whose evidence was split under a different window than the
			// one THIS evaluation resolves makes the day unknown. The
			// old-window activity sits in the wrong leg, invisible to the
			// activity and coverage gates; attestation is the only sound
			// answer (the owner-approved silence-over-wrong trade: no new
			// advice until re-ingestion rewrites the evidence).
			if !r.scheduleAttested {
				return 0, 0, false, false
			}
			// #760 (V15-10) + #826 (V16-02): the off-window p95 sum is the
			// direct outside-BH measure — never all-minus-bh percentile
			// subtraction (a constant workload reads outside=0 that way
			// while running all night), and never a median (a p50 hides
			// minority-of-window nightly activity — the #747 lesson).
			outside := r.offCPU
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
		if dayApplicable && !dayBHActive {
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
		ON CONFLICT (org_id, hc_cluster_id, window_start, management_cluster_uuid)
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
	// #831: the fingerprint of the schedule the evidence must be attested
	// to — the one THIS evaluation resolves. Any row split under a
	// different window reads unknown.
	fingerprint := bhschedule.FingerprintBounds(sched.Timezone, sched.Days, sched.StartMin, sched.EndMin)
	signature, err := loadSleepEvidence(ctx, pool, c.orgID, c.hcID, windowStart, windowEnd, fingerprint)
	if err != nil {
		return false, err
	}
	syncedExact, syncedPrefixes := loadSyncedPlatformSet(ctx, pool, c.orgID)
	bhPeak, outsidePeak, match, known := evalSleepSignature(signature, windowStart, windowEnd,
		int64(p.SleepBHActivityFloorMC), int64(p.SleepOutsideBHIdleFloorMC), p.SleepMinCoverageFrac, sched, syncedExact, syncedPrefixes)
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
