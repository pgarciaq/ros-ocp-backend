package correlate

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// W3 short lane (Child B, #674): pool-zero fast path over the standard
// 14d rule (#664, unchanged default lane).
//
// Lane arbitration per HC, evaluated independently (no suppression —
// each window stands alone with its own PK row):
//  1. Short lane — NodePool evidence exists for the HC: every pool at
//     spec==0 AND status==0 sustained (mid-transition reads unknown),
//     then the dual legs over the trailing short window. Same verdict,
//     window + lane signals distinguish the row.
//  2. Standard lane — everything else: the #664 14d evaluation,
//     byte-identical behavior.
//
// Absolute zero is exact (declared + observed integer counts — no
// epsilon; see #674 amendment). Coverage: at least required_days of
// the short window must carry snapshots; every covered day must read
// all-pools-zero (a non-zero covered day vetoes, max-like semantics).
// Legs (workload idle + still-on) require full short-window coverage
// — cheap daily data, no reason to loosen.

// poolDayState is one day's parked reading across an HC's pools.
type poolDayState struct {
	day       string
	snapshots int
	allZero   bool
}

// loadPoolDayStates reads per-day pool snapshots for one HC in range.
// A day with no rows is absent (unknown); autoscaling strings ride
// along as context but never gate (zero means zero, whatever the mode).
func loadPoolDayStates(ctx context.Context, pool *pgxpool.Pool, orgID, hcID string, start, end time.Time) (map[string]*poolDayState, error) {
	rows, err := pool.Query(ctx, `
		SELECT window_start, pool_name, spec_replicas, status_replicas
		FROM hosted_nodepool_rollups
		WHERE org_id = $1 AND hc_cluster_id = $2
		  AND window_start >= $3 AND window_end <= $4
		ORDER BY window_start ASC`,
		orgID, hcID, start, end)
	if err != nil {
		return nil, fmt.Errorf("load pool snapshots: %w", err)
	}
	defer rows.Close()
	out := map[string]*poolDayState{}
	for rows.Next() {
		var ws time.Time
		var poolName string
		var spec, status int64
		if err := rows.Scan(&ws, &poolName, &spec, &status); err != nil {
			return nil, fmt.Errorf("scan pool snapshot: %w", err)
		}
		day := ws.UTC().Format("2006-01-02")
		st, ok := out[day]
		if !ok {
			st = &poolDayState{day: day, allZero: true}
			out[day] = st
		}
		st.snapshots++
		if spec != 0 || status != 0 {
			st.allZero = false
		}
		_ = poolName
	}
	return out, rows.Err()
}

// evalPoolParked applies the parked gate: at least required covered days
// in the window, every covered day all-pools-zero. Returns covered count.
func evalPoolParked(states map[string]*poolDayState, windowStart time.Time, windowDays, requiredDays int) (covered int, parked bool) {
	for i := 0; i < windowDays; i++ {
		day := windowStart.AddDate(0, 0, i).UTC().Format("2006-01-02")
		st, ok := states[day]
		if !ok {
			continue
		}
		covered++
		if !st.allZero {
			return covered, false
		}
	}
	if covered < requiredDays {
		return covered, false
	}
	return covered, true
}

// evaluateZombieShortForHC fires at most one short-lane advisory per HC
// per day. No nodepool evidence means no short evaluation (the standard
// lane still runs) — never an error.
func evaluateZombieShortForHC(ctx context.Context, pool *pgxpool.Pool, c zombieCandidate, windowStart, windowEnd time.Time, p Policy) (fired bool, err error) {
	if p.ZombieShortWindowDays <= 0 || p.ZombieShortRequiredDays <= 0 {
		return false, nil
	}
	windowDays := p.ZombieShortWindowDays
	requiredDays := p.ZombieShortRequiredDays
	if requiredDays > windowDays {
		requiredDays = windowDays
	}
	states, err := loadPoolDayStates(ctx, pool, c.orgID, c.hcID, windowStart, windowEnd)
	if err != nil {
		return false, err
	}
	if len(states) == 0 {
		return false, nil
	}
	covered, parked := evalPoolParked(states, windowStart, windowDays, requiredDays)
	if !parked {
		return false, nil
	}
	days, err := loadZombieDayCPU(ctx, pool, c.orgID, c.hcID, windowStart, windowEnd)
	if err != nil {
		return false, err
	}
	syncedExact, syncedPrefixes := loadSyncedPlatformSet(ctx, pool, c.orgID)
	peak, idle, known := evalZombieIdle(days, windowStart, windowEnd, int64(p.ZombieIdleCPUFloorMC), syncedExact, syncedPrefixes)
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
	_ = cpReq
	apiMax, apiKnown := zombieAPIMaxDaily(ctx, pool, c.orgID, c.hcID, windowStart, windowEnd, time.Duration(p.HMinDataMinutes)*time.Minute)
	if !stillKnown {
		if err := writeZombieShortAdvisory(ctx, pool, c, peak, covered, apiMax, apiKnown, "medium", windowStart, windowEnd, p); err != nil {
			return false, err
		}
		return true, nil
	}
	if !stillOn {
		return false, nil
	}
	if err := writeZombieShortAdvisory(ctx, pool, c, peak, covered, apiMax, apiKnown, "high", windowStart, windowEnd, p); err != nil {
		return false, err
	}
	return true, nil
}

// writeZombieShortAdvisory upserts one short-lane advisory. Same table,
// verdict, and PK shape as the standard lane; the lane marker in signals
// (short-window days + covered count) distinguishes how it fired.
func writeZombieShortAdvisory(ctx context.Context, pool *pgxpool.Pool, c zombieCandidate, peakUserMC int64, covered int, apiMax float64, apiKnown bool, confidence string, windowStart, windowEnd time.Time, p Policy) error {
	return writeZombieAdvisory(ctx, pool, c.orgID, c.hcID, c.clusterUUID, peakUserMC, int64(p.ZombieIdleCPUFloorMC), 0, apiMax, apiKnown, confidence, windowStart, windowEnd, p,
		fmt.Sprintf("lane:short-%dd", int(windowEnd.Sub(windowStart).Hours()/24)),
		fmt.Sprintf("pool_zero_days:%d", covered))
}

// RunZombieShortCycle evaluates parked evidence for every HC with daily
// windows. Shares the sweep, metrics, and never-fail-on-evidence
// contract with the standard pass. Never fails on evidence problems.
func RunZombieShortCycle(ctx context.Context, pool *pgxpool.Pool) (fired int, err error) {
	windowEnd := time.Now().UTC().Truncate(24 * time.Hour)
	// Candidate enumeration is window-free (fresh snapshots only); per-org
	// windows resolve below.
	snapshotCutoff := time.Now().UTC().Add(-time.Duration(zombieSnapshotFreshHours) * time.Hour)
	cands, err := enumerateZombieCandidates(ctx, pool, snapshotCutoff)
	if err != nil {
		if pgrec.IsUndefinedTable(err) {
			logging.GetLogger().Warnf("correlator: zombie short tables unavailable (pre-migration, skipping): %v", err)
			return 0, nil
		}
		return 0, err
	}
	for _, c := range cands {
		if err := ctx.Err(); err != nil {
			return fired, err
		}
		p := policyForOrg(ctx, pool, c.orgID)
		if p.ZombieShortWindowDays <= 0 {
			continue
		}
		ws := windowEnd.AddDate(0, 0, -p.ZombieShortWindowDays)
		ok, err := evaluateZombieShortForHC(ctx, pool, c, ws, windowEnd, p)
		if err != nil {
			logging.ForOrg(c.orgID, c.hcID).Warnf("correlator: zombie short evaluation failed, silent: %v", err)
			continue
		}
		if ok {
			fired++
		}
	}
	return fired, nil
}
