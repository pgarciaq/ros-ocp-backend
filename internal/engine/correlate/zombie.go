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

// W3 unused-hosted-cluster rule (#658, design ADR-0333, research R4 GO).
//
// Per HC with fresh snapshot evidence, over a trailing 14-day window
// (two-weekend rationale: a dev cluster idle only on weekends must not
// fire — the window is a const, not a knob):
//   - idle leg: MAX daily hosted requests (all verb groups, +Inf-bucket
//     deltas) below T. Max, not mean: one busy day vetoes. All 14 days
//     must carry valid deltas — missing days read as unknown (silence),
//     which subsumes the brand-new-cluster grace period by construction.
//   - still-on leg: fresh association snapshot AND CP-namespace CPU
//     requests above the absolute floor (a provisioned, running CP).
//     Digests present-but-zero read as known-not-on (silence); digests
//     absent read as unknown (medium confidence, never proof of off).
//
// Fire review_unused_hosted_cluster with high confidence on dual
// evidence, medium on snapshot+idle without digest proof. Unknown
// anywhere else is silence. Never recommends pausedUntil (ADR NO-GO);
// copy branching is downstream (rows only, #646 precedent).
//
// Grain note (locked pre-build): evaluation rides the hourly job, but
// evidence is mixed-grain — hourly bucket snapshots, per-upload
// association snapshots, daily digests. Freshness is per-leg following
// the evalC precedent (date-based for daily inputs); the 2h bucket
// freshness gate never touches daily evidence.
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

// zombieCandidate is one HC with recent bucket evidence plus the
// management cluster serving it (from snapshots).
type zombieCandidate struct {
	orgID       string
	clusterUUID string
	hcID        string
}

// enumerateZombieCandidates lists (org, mgmt, hc) triples with bucket
// evidence in the trailing window and a fresh association snapshot.
// No evidence means no evaluation.
func enumerateZombieCandidates(ctx context.Context, pool *pgxpool.Pool, windowStart time.Time, snapshotCutoff time.Time) ([]zombieCandidate, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT b.org_id, s.cluster_uuid::text, b.hc_cluster_id
		FROM hosted_api_bucket_rollups b
		JOIN manifest_hcp_snapshots s
		  ON s.org_id = b.org_id AND s.hosted_cluster_id = b.hc_cluster_id
		WHERE b.window_end >= $1 AND s.observed_at >= $2 AND s.complete = TRUE`,
		windowStart, snapshotCutoff)
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

// zombieDailyTotals returns per-day hosted request totals (summed over
// verb groups from +Inf-bucket deltas) for the 14-day window, plus the
// count of days with valid deltas. A day is valid with >=2 snapshots
// spanning the methodological min-data floor; counter resets resolve
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

// evalZombieIdle applies the idle leg: all 14 window days valid and the
// max daily total below T. Missing days are unknown (silence), never
// proof of idleness — this is also the brand-new-cluster grace period.
func evalZombieIdle(totals map[string]float64, windowStart, windowEnd time.Time, threshold float64) (maxDaily float64, idle, known bool) {
	if threshold <= 0 {
		return 0, false, false
	}
	maxDaily = 0
	for d := windowStart; d.Before(windowEnd); d = d.AddDate(0, 0, 1) {
		v, ok := totals[d.UTC().Format("2006-01-02")]
		if !ok {
			return 0, false, false
		}
		if v > maxDaily {
			maxDaily = v
		}
	}
	return maxDaily, maxDaily < threshold, true
}

// evalZombieStillOn applies the still-on leg over HCP namespaces: fresh
// snapshot (established by enumeration) plus CP CPU requests above the
// absolute floor. Digests absent is unknown (medium); digests
// present-but-zero is known-not-on (silence).
func evalZombieStillOn(ctx context.Context, pool *pgxpool.Pool, orgID, mgmtCluster, hcID string) (cpReqMC int64, high, known bool) {
	hcMap, err := engine.LoadHCAssociationForRun(ctx, pool, orgID, mgmtCluster)
	if err != nil || len(hcMap) == 0 {
		return 0, false, false
	}
	var namespaces []string
	for ns, hc := range hcMap {
		if hc == hcID {
			namespaces = append(namespaces, ns)
		}
	}
	if len(namespaces) == 0 {
		return 0, false, false
	}
	var usage, requests *int64
	err = pool.QueryRow(ctx, `
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

// writeZombieAdvisory upserts one fired zombie advisory. Daily window
// rows refresh in place on hourly reruns; the verdict distinguishes
// the row. Per-verdict column semantics (apitax precedent): h_p99_s
// carries max daily requests, h_threshold_s carries T; c_ratio stays
// NULL; HC identity and CP evidence ride n_signals.
func writeZombieAdvisory(ctx context.Context, pool *pgxpool.Pool, orgID, hcID, mgmtCluster string, maxDaily, threshold float64, cpReqMC int64, confidence string, windowStart, windowEnd time.Time, p Policy) error {
	signals := []string{
		"hc:" + hcID,
		fmt.Sprintf("max_daily_req:%.0f", maxDaily),
		fmt.Sprintf("idle_threshold:%.0f", threshold),
		fmt.Sprintf("cp_req_mc:%d", cpReqMC),
	}
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
		maxDaily, threshold, signals,
		windowEnd.UTC().Add(time.Duration(p.AdvisoryExpiryHours)*time.Hour))
	if err != nil {
		return fmt.Errorf("write zombie advisory: %w", err)
	}
	metrics.HCPAdvisoriesTotal.WithLabelValues(zombieVerdict).Inc()
	return nil
}

// evaluateZombieForHC fires at most one advisory per HC per day.
func evaluateZombieForHC(ctx context.Context, pool *pgxpool.Pool, c zombieCandidate, windowStart, windowEnd time.Time, p Policy) (fired bool, err error) {
	minSpan := time.Duration(p.HMinDataMinutes) * time.Minute
	totals, err := zombieDailyTotals(ctx, pool, c.orgID, c.hcID, windowStart, windowEnd, minSpan)
	if err != nil {
		return false, err
	}
	maxDaily, idle, known := evalZombieIdle(totals, windowStart, windowEnd, p.ZombieIdleReqPerDay)
	if !known || !idle {
		return false, nil
	}
	cpReq, stillOn, stillKnown := evalZombieStillOn(ctx, pool, c.orgID, c.clusterUUID, c.hcID)
	if !stillKnown {
		// Digests absent: snapshot + idle without CP proof — medium.
		if err := writeZombieAdvisory(ctx, pool, c.orgID, c.hcID, c.clusterUUID, maxDaily, p.ZombieIdleReqPerDay, 0, "medium", windowStart, windowEnd, p); err != nil {
			return false, err
		}
		return true, nil
	}
	if !stillOn {
		return false, nil
	}
	if err := writeZombieAdvisory(ctx, pool, c.orgID, c.hcID, c.clusterUUID, maxDaily, p.ZombieIdleReqPerDay, cpReq, "high", windowStart, windowEnd, p); err != nil {
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
	windowStart := windowEnd.AddDate(0, 0, -zombieWindowDays)
	snapshotCutoff := time.Now().UTC().Add(-time.Duration(zombieSnapshotFreshHours) * time.Hour)
	cands, err := enumerateZombieCandidates(ctx, pool, windowStart, snapshotCutoff)
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
