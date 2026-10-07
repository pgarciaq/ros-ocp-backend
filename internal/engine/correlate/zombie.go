package correlate

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/librobne/hcp"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// W3 unused-hosted-cluster rule (#658 mechanism, #664 redefinition,
// design ADR-0333, research R4 GO).
//
// Per HC with fresh snapshot evidence, over a trailing 14-day window
// (two-weekend rationale — the window is a const, not a knob):
//   - idle leg (PRIMARY): zero active non-system workloads every day.
//     A workload is active iff its daily CPU sums at or above the
//     ceremonial floor (tenant-tunable, default 10m, discouraged).
//     Full 14d digest coverage required — missing days read as unknown
//     (silence), subsuming the brand-new-cluster grace period.
//   - still-on leg: fresh association snapshot AND CP-namespace CPU
//     requests above the absolute floor (a provisioned, running CP).
//     Digests present-but-zero read as known-not-on (silence); digests
//     absent read as unknown (medium confidence, never proof of off).
//
// Why workload+CPU, not API counts (#662): hosted background chatter
// measures 0.9–3.6M requests/day on quiet boxes across labs and months
// (controller leases, list/watch loops, monitoring) — no absolute
// request threshold separates zombie from alive, at any verb scoping.
// Workload CPU has a natural zero (true idle reads exactly 0.0000);
// a Sept specimen (1 workload @ 0 CPU, 888k API req/day) fires here
// and correctly never fires on API counts.
//
// Fire review_unused_hosted_cluster with high confidence on dual
// evidence, medium on snapshot+idle without digest proof. Unknown
// anywhere else is silence. Never recommends pausedUntil (ADR NO-GO);
// copy branching is downstream (rows only, #646 precedent).
//
// Grain note (locked pre-build): evaluation rides the hourly job, but
// evidence is mixed-grain — daily digests, per-upload snapshots.
// Freshness is per-leg following the evalC precedent (date-based for
// daily inputs); the 2h bucket freshness gate never touches this rule.
const (
	zombieVerdict = "review_unused_hosted_cluster"

	// zombieWindowDays is the trailing idle window. Const by owner
	// amendment (#391): two weekends catch weekend-only-idle dev
	// clusters without firing on them.
	zombieWindowDays = 14
	// zombieSnapshotFreshHours bounds "still on" to recently-observed
	// associations. Snapshots ride multi-hour upload cycles, so this is
	// day-scale, not the 2h bucket freshness gate.
	zombieSnapshotFreshHours = 72
)

// zombiePlatformPrefixes marks platform-owned namespaces by prefix.
// Narrow by design: an incomplete list fails silent (addon CPU counted
// as user activity no-fires), while an over-broad list would fire on
// alive clusters. Grounded in hosted namespace evidence (Sept pack +
// live hc01); tenant-curated replacement rides #665 (koku cost-groups
// sync), which also serves as the permanent fallback story in reverse.
var zombiePlatformPrefixes = []string{"kube-", "openshift-"}

// zombiePlatformExact marks platform addon namespaces that live outside
// the platform prefixes (evidence-grounded, same narrow-side rule).
var zombiePlatformExact = map[string]bool{
	"koku-metrics-operator":               true,
	"local-path-storage":                  true,
	"open-cluster-management-agent-addon": true,
	"open-cluster-management-hc01":        true,
}

// isZombieUserNamespace reports whether CPU in ns counts as user
// activity for the idle leg. Platform namespaces (prefix or curated
// exact) never count; everything else (including default/) does.
func isZombieUserNamespace(ns string) bool {
	if zombiePlatformExact[ns] {
		return false
	}
	for _, p := range zombiePlatformPrefixes {
		if len(ns) >= len(p) && ns[:len(p)] == p {
			return false
		}
	}
	return true
}

// zombieCandidate is one HC with a fresh association snapshot.
type zombieCandidate struct {
	orgID       string
	clusterUUID string
	hcID        string
}

// enumerateZombieCandidates lists (org, mgmt, hc) triples with a fresh
// complete association snapshot. Digest coverage is checked inside the
// idle leg (per-day unknown, never assumed). No snapshot means no
// evaluation.
func enumerateZombieCandidates(ctx context.Context, pool *pgxpool.Pool, snapshotCutoff time.Time) ([]zombieCandidate, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT org_id, cluster_uuid::text, hosted_cluster_id
		FROM manifest_hcp_snapshots
		WHERE hosted_cluster_id IS NOT NULL AND hosted_cluster_id != ''
		  AND observed_at >= $1 AND complete = TRUE`,
		snapshotCutoff)
	if err != nil {
		return nil, fmt.Errorf("enumerate zombie candidates: %w", err)
	}
	defer rows.Close()
	var out []zombieCandidate
	for rows.Next() {
		var c zombieCandidate
		if err := rows.Scan(&c.orgID, &c.clusterUUID, &c.hcID); err != nil {
			return nil, fmt.Errorf("scan zombie candidate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// zombieDayCPU is one workload-day of hosted burn.
type zombieDayCPU struct {
	day       string
	namespace string
	workload  string
	totalMC   int64
	measured  bool
}

// loadZombieDayCPU reads per-workload daily CPU sums for a hosted
// cluster over the window (cluster_uuid is the HC's own hosted ID, per
// the N-leg convention — never the management cluster). All namespaces
// load; platform split happens in Go so platform rows prove pipeline
// liveness. SUM skips NULLs (unmeasurable samples contribute nothing);
// measured flags rows with at least one real sample so all-NULL days
// read as unmeasurable, never zero.
func loadZombieDayCPU(ctx context.Context, pool *pgxpool.Pool, orgID, hostedCluster string, startDate, endDate time.Time) ([]zombieDayCPU, error) {
	rows, err := pool.Query(ctx, `
		SELECT bucket_date, namespace, workload,
			SUM(cpu_usage_p50_mc) AS wcpu,
			COUNT(cpu_usage_p50_mc) AS nmeas
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND bucket_date >= $3 AND bucket_date < $4
		GROUP BY 1, 2, 3`,
		orgID, hostedCluster, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("load zombie day cpu: %w", err)
	}
	defer rows.Close()
	var out []zombieDayCPU
	for rows.Next() {
		var d zombieDayCPU
		var date time.Time
		var total *int64
		var nmeas int64
		if err := rows.Scan(&date, &d.namespace, &d.workload, &total, &nmeas); err != nil {
			return nil, fmt.Errorf("scan zombie day cpu: %w", err)
		}
		d.day = date.UTC().Format("2006-01-02")
		if total != nil {
			d.totalMC = *total
		}
		d.measured = nmeas > 0
		out = append(out, d)
	}
	return out, rows.Err()
}

// evalZombieIdle applies the idle leg: every window day covered by
// measurable digests, and zero active (at-or-above-floor) non-system
// workloads on every covered day. A day with digests but no user
// workloads is idle evidence (pipeline ran, user absent); a day with
// no measurable digests is unknown. Returns peak daily user CPU for
// evidence.
func evalZombieIdle(days []zombieDayCPU, windowStart, windowEnd time.Time, floorMC int64) (peakUserMC int64, idle, known bool) {
	byDay := map[string][]zombieDayCPU{}
	for _, d := range days {
		byDay[d.day] = append(byDay[d.day], d)
	}
	peakUserMC = 0
	for d := windowStart; d.Before(windowEnd); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format("2006-01-02")
		rows, ok := byDay[key]
		if !ok {
			return 0, false, false
		}
		covered := false
		dayPeak := int64(0)
		for _, r := range rows {
			if !r.measured {
				continue
			}
			covered = true
			if !isZombieUserNamespace(r.namespace) {
				continue
			}
			if r.totalMC > dayPeak {
				dayPeak = r.totalMC
			}
			if r.totalMC >= floorMC {
				return 0, false, true
			}
		}
		if !covered {
			return 0, false, false
		}
		if dayPeak > peakUserMC {
			peakUserMC = dayPeak
		}
	}
	return peakUserMC, true, true
}

// zombieDailyTotals returns per-day hosted request totals (summed over
// verb groups from +Inf-bucket deltas). Corroborating context only:
// API counts never gate the zombie rule (#662). Counter resets resolve
// to the post-reset value (apitax precedent).
func zombieDailyTotals(ctx context.Context, pool *pgxpool.Pool, orgID, hcID string, windowStart, windowEnd time.Time, minSpan time.Duration) (map[string]float64, error) {
	rows, err := pool.Query(ctx, `
		SELECT verb_group, bucket_count, collected_at
		FROM hosted_api_bucket_rollups
		WHERE org_id = $1 AND hc_cluster_id = $2 AND le = '+Infinity'
		  AND collected_at >= $3 AND collected_at < $4
		ORDER BY verb_group, collected_at ASC`,
		orgID, hcID, windowStart, windowEnd)
	if err != nil {
		return nil, fmt.Errorf("load zombie buckets: %w", err)
	}
	defer rows.Close()
	type point struct {
		count float64
		at    time.Time
	}
	byVerbDay := map[string]map[string][]point{}
	for rows.Next() {
		var verb string
		var count float64
		var at time.Time
		if err := rows.Scan(&verb, &count, &at); err != nil {
			return nil, fmt.Errorf("scan zombie bucket: %w", err)
		}
		day := at.UTC().Format("2006-01-02")
		if byVerbDay[verb] == nil {
			byVerbDay[verb] = map[string][]point{}
		}
		byVerbDay[verb][day] = append(byVerbDay[verb][day], point{count, at})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	totals := map[string]float64{}
	for _, byDay := range byVerbDay {
		for day, pts := range byDay {
			if len(pts) < 2 || pts[len(pts)-1].at.Sub(pts[0].at) < minSpan {
				continue
			}
			first, last := pts[0].count, pts[len(pts)-1].count
			delta := last - first
			if delta < 0 {
				delta = last
			}
			totals[day] += delta
		}
	}
	return totals, nil
}

// evalZombieStillOn applies the still-on leg over HCP namespaces: fresh
// snapshot (established by enumeration) plus CP CPU requests above the
// absolute floor. Digests absent is unknown (medium); digests
// present-but-zero is known-not-on (silence).
func evalZombieStillOn(ctx context.Context, pool *pgxpool.Pool, orgID, mgmtCluster, hcID string, namespaces []string) (cpReqMC int64, high, known bool) {
	if len(namespaces) == 0 {
		return 0, false, false
	}
	var usage, requests *int64
	err := pool.QueryRow(ctx, `
		SELECT SUM(cpu_usage_p95_mc), SUM(cpu_request_p50_mc)
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = ANY($3)
		  AND bucket_date >= CURRENT_DATE - INTERVAL '1 day'`,
		orgID, mgmtCluster, namespaces).Scan(&usage, &requests)
	if err != nil {
		return 0, false, false
	}
	if requests == nil {
		return 0, false, false
	}
	if *requests < hcp.ControlPlaneCPUFloorMC {
		return *requests, false, true
	}
	return *requests, true, true
}

// zombieAPIMaxDaily is a best-effort corroborating read of trailing
// bucket deltas for signals context. Errors and absence yield unknown;
// it never gates evaluation (API counts are non-discriminating, #662).
func zombieAPIMaxDaily(ctx context.Context, pool *pgxpool.Pool, orgID, hcID string, windowStart, windowEnd time.Time, minSpan time.Duration) (float64, bool) {
	totals, err := zombieDailyTotals(ctx, pool, orgID, hcID, windowStart.AddDate(0, 0, -1), windowEnd, minSpan)
	if err != nil || len(totals) == 0 {
		return 0, false
	}
	maxV := 0.0
	for _, v := range totals {
		if v > maxV {
			maxV = v
		}
	}
	return maxV, true
}

// writeZombieAdvisory upserts one fired zombie advisory. Daily window
// rows refresh in place on hourly reruns; the verdict distinguishes
// the row. Per-verdict column semantics (apitax precedent): h_p99_s
// carries peak daily user CPU (mC), h_threshold_s carries the floor;
// c_ratio stays NULL; HC identity and CP/API evidence ride n_signals.
func writeZombieAdvisory(ctx context.Context, pool *pgxpool.Pool, orgID, hcID, mgmtCluster string, peakUserMC, floorMC, cpReqMC int64, apiMax float64, apiKnown bool, confidence string, windowStart, windowEnd time.Time, p Policy, lane ...string) error {
	apiCtx := "api_max_daily:unknown (context, no gate)"
	if apiKnown {
		apiCtx = fmt.Sprintf("api_max_daily:%.0f (context, no gate)", apiMax)
	}
	signals := []string{
		"hc:" + hcID,
		fmt.Sprintf("peak_user_mc:%d", peakUserMC),
		fmt.Sprintf("idle_floor_mc:%d", floorMC),
		fmt.Sprintf("cp_req_mc:%d", cpReqMC),
		apiCtx,
	}
	signals = append(signals, lane...)
	_, err := pool.Exec(ctx, `
		INSERT INTO hcp_correlation_advisories (
			org_id, hc_cluster_id, management_cluster_uuid,
			window_start, window_end, verdict, confidence,
			h_p99_s, h_threshold_s, c_ratio, n_signals,
			expires_at
		) VALUES ($1,$2,$3,$4,$5,'review_unused_hosted_cluster',$6,$7,$8,NULL,$9,$10)
		ON CONFLICT (org_id, hc_cluster_id, window_start)
		DO UPDATE SET verdict = EXCLUDED.verdict, confidence = EXCLUDED.confidence,
			h_p99_s = EXCLUDED.h_p99_s, h_threshold_s = EXCLUDED.h_threshold_s,
			c_ratio = EXCLUDED.c_ratio, n_signals = EXCLUDED.n_signals,
			expires_at = EXCLUDED.expires_at`,
		orgID, hcID, mgmtCluster,
		windowStart.UTC(), windowEnd.UTC(), confidence,
		float64(peakUserMC), float64(floorMC), signals,
		windowEnd.UTC().Add(time.Duration(p.AdvisoryExpiryHours)*time.Hour))
	if err != nil {
		return fmt.Errorf("write zombie advisory: %w", err)
	}
	metrics.HCPAdvisoriesTotal.WithLabelValues(zombieVerdict).Inc()
	return nil
}

// evaluateZombieForHC fires at most one advisory per HC per day.
// Cluster routing (locked): idle leg reads HOSTED digests keyed by the
// HC's own cluster ID (N-leg convention); still-on reads MANAGEMENT
// digests over association namespaces. Never the reverse.
func evaluateZombieForHC(ctx context.Context, pool *pgxpool.Pool, c zombieCandidate, windowStart, windowEnd time.Time, p Policy) (fired bool, err error) {
	days, err := loadZombieDayCPU(ctx, pool, c.orgID, c.hcID, windowStart, windowEnd)
	if err != nil {
		return false, err
	}
	peak, idle, known := evalZombieIdle(days, windowStart, windowEnd, int64(p.ZombieIdleCPUFloorMC))
	if !known || !idle {
		return false, nil
	}
	hcMap, err := engine.LoadHCAssociationForRun(ctx, pool, c.orgID, c.clusterUUID)
	if err != nil || len(hcMap) == 0 {
		return false, err
	}
	var namespaces []string
	for ns, hc := range hcMap {
		if hc == c.hcID {
			namespaces = append(namespaces, ns)
		}
	}
	cpReq, stillOn, stillKnown := evalZombieStillOn(ctx, pool, c.orgID, c.clusterUUID, c.hcID, namespaces)
	if !stillKnown {
		// Digests absent: snapshot + idle without CP proof — medium.
		apiMax, apiKnown := zombieAPIMaxDaily(ctx, pool, c.orgID, c.hcID, windowStart, windowEnd, time.Duration(p.HMinDataMinutes)*time.Minute)
		if err := writeZombieAdvisory(ctx, pool, c.orgID, c.hcID, c.clusterUUID, peak, int64(p.ZombieIdleCPUFloorMC), 0, apiMax, apiKnown, "medium", windowStart, windowEnd, p); err != nil {
			return false, err
		}
		return true, nil
	}
	if !stillOn {
		return false, nil
	}
	apiMax, apiKnown := zombieAPIMaxDaily(ctx, pool, c.orgID, c.hcID, windowStart, windowEnd, time.Duration(p.HMinDataMinutes)*time.Minute)
	if err := writeZombieAdvisory(ctx, pool, c.orgID, c.hcID, c.clusterUUID, peak, int64(p.ZombieIdleCPUFloorMC), cpReq, apiMax, apiKnown, "high", windowStart, windowEnd, p); err != nil {
		return false, err
	}
	return true, nil
}

// RunZombieCycle evaluates idle evidence for every HC with daily
// windows and writes review_unused_hosted_cluster advisories. Daily
// window rows (midnight UTC boundaries) refresh in place on hourly
// reruns. Shares the sweep, metrics, and never-fail-on-evidence
// contract. Never fails on evidence problems — those are silence.
func RunZombieCycle(ctx context.Context, pool *pgxpool.Pool) (fired int, err error) {
	windowEnd := time.Now().UTC().Truncate(24 * time.Hour)
	snapshotCutoff := time.Now().UTC().Add(-time.Duration(zombieSnapshotFreshHours) * time.Hour)
	cands, err := enumerateZombieCandidates(ctx, pool, snapshotCutoff)
	if err != nil {
		if pgrec.IsUndefinedTable(err) {
			logging.GetLogger().Warnf("correlator: zombie tables unavailable (pre-migration, skipping): %v", err)
			return 0, nil
		}
		return 0, err
	}
	for _, c := range cands {
		if err := ctx.Err(); err != nil {
			return fired, err
		}
		p := policyForOrg(ctx, pool, c.orgID)
		// Idle window is tenant-tunable (const-lock overturn, #674);
		// clamp to sane bounds (validation guarantees 1-90, belt and
		// suspenders against direct Policy construction).
		idleWindow := p.ZombieIdleWindowDays
		if idleWindow < 1 || idleWindow > 90 {
			idleWindow = zombieWindowDays
		}
		windowStart := windowEnd.AddDate(0, 0, -idleWindow)
		ok, err := evaluateZombieForHC(ctx, pool, c, windowStart, windowEnd, p)
		if err != nil {
			logging.ForOrg(c.orgID, c.hcID).Warnf("correlator: zombie evaluation failed, silent: %v", err)
			continue
		}
		if ok {
			fired++
		}
	}
	return fired, nil
}
