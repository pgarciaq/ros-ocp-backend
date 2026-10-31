// W3 unused-hosted-cluster rule (#722 robne parity; server rule
// internal/engine/correlate/zombie.go — #658 mechanism, #664 redefinition,
// design ADR-0333, research R4 GO).
//
// Per HC with fresh snapshot evidence, over a trailing window:
//   - idle leg (PRIMARY): zero active non-system workloads every day.
//     A workload is active iff its daily CPU sums at or above the floor.
//     Full window digest coverage required — missing days read as unknown
//     (silence), subsuming the brand-new-cluster grace period.
//   - still-on leg: fresh association snapshot AND CP-namespace CPU
//     requests above the absolute floor (a provisioned, running CP).
//     Digests present-but-zero read as known-not-on (silence); digests
//     absent read as unknown (medium confidence, never proof of off).
//
// Why workload+CPU, not API counts (#662): hosted background chatter has
// no absolute request threshold that separates zombie from alive. API
// counts ride along as context only — they never gate.
//
// Two lanes, evaluated independently (no suppression — each window stands
// alone with its own row): the standard 14d window and the pool-zero short
// lane (#674): every pool at spec==0 AND status==0 sustained over the short
// window, then the same dual legs.
//
// Offline shape (ADR-0350): the hosted digests are the PRIMARY payload's
// (the HC's own cluster ID, N-leg convention); the still-on digests, the
// association namespaces, and the NodePool rows come from the CONTEXT
// payload (the management cluster). Never the reverse.
package advisory

import (
	"fmt"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/hcp"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// ZombieVerdict is the W3 unused-hosted-cluster advisory verdict
// (#717's serving contract).
const ZombieVerdict = "review_unused_hosted_cluster"

// ZombieSnapshotFreshHours bounds "still on" to recently-observed
// associations (the server's const, not a knob).
const ZombieSnapshotFreshHours = 72

// ZombiePolicy carries the W3 evaluation knobs (the server's per-org
// correlator settings, offline as CLI config).
type ZombiePolicy struct {
	IdleWindowDays    int
	IdleCPUFloorMC    int64
	ShortWindowDays   int
	ShortRequiredDays int
	MinDataMinutes    int
}

// DefaultZombiePolicy mirrors the server's compiled defaults
// (DefaultPolicy in internal/engine/correlate).
func DefaultZombiePolicy() ZombiePolicy {
	return ZombiePolicy{IdleWindowDays: 14, IdleCPUFloorMC: 10, ShortWindowDays: 3, ShortRequiredDays: 3, MinDataMinutes: 30}
}

// ZombieInput is one HC's offline evaluation facts. The hosted digests are
// the primary payload's; the management facts are the context payload's.
type ZombieInput struct {
	HcID string
	Now  time.Time
	// HostedDigests: the HC's own cluster digests (primary payload).
	HostedDigests []types.KeyedDigest
	// MgmtDigests: the management cluster's digests (context payload).
	MgmtDigests []types.KeyedDigest
	// AssocNamespaces: the association map's namespaces for this HC.
	AssocNamespaces []string
	// SnapshotFresh: a complete association snapshot observed within
	// ZombieSnapshotFreshHours (the enumeration gate, resolved by the
	// caller from the context topology).
	SnapshotFresh bool
	// NodepoolRows: the HC's pool snapshots (context payload) — the
	// short lane's parked gate.
	NodepoolRows []csv.NodepoolRow
	// SLORows: the HC's API bucket snapshots (any payload) — the
	// api_max_daily context (never a gate).
	SLORows []csv.SLORow
	Policy  ZombiePolicy
	// SyncedExact/SyncedPrefixes: the optional synced platform
	// exclusions (koku cost-groups push stand-in).
	SyncedExact    map[string]bool
	SyncedPrefixes []string
}

// ZombieDaySums groups hosted digests into per-(day, ns, workload) CPU p50
// sums (millicores) — the idle leg's input. A present row is measured;
// the offline digests carry values, never NULLs.
func ZombieDaySums(digests []types.KeyedDigest) map[string]map[DayKey]int64 {
	out := map[string]map[DayKey]int64{}
	for _, kd := range digests {
		day := kd.Row.BucketDate.UTC().Format("2006-01-02")
		if out[day] == nil {
			out[day] = map[DayKey]int64{}
		}
		k := DayKey{NS: kd.Key.Namespace, WL: kd.Key.Workload}
		out[day][k] += kd.Row.CPUUsageP50MC
	}
	return out
}

// zombieIsUserNamespace applies the platform exclusion: compiled
// prefixes/exact plus the optional synced set (the offline stand-in for
// the koku cost-groups push — absent input means the compiled defaults,
// which is itself a parity case).
func zombieIsUserNamespace(ns string, exact map[string]bool, prefixes []string) bool {
	if CompiledPlatformExact[ns] || exact[ns] {
		return false
	}
	for _, p := range CompiledPlatformPrefixes {
		if p != "" && len(ns) >= len(p) && ns[:len(p)] == p {
			return false
		}
	}
	for _, p := range prefixes {
		if p != "" && len(ns) >= len(p) && ns[:len(p)] == p {
			return false
		}
	}
	return true
}

// evalZombieIdle: every covered day must show zero active user workloads.
// A day with digests but no user workloads is idle evidence (pipeline
// ran, user absent); a day with no digests is unknown. Returns peak daily
// user CPU for evidence.
func evalZombieIdle(sums map[string]map[DayKey]int64, windowStart, windowEnd time.Time, floorMC int64, syncedExact map[string]bool, syncedPrefixes []string) (peakUserMC int64, idle, known bool) {
	peakUserMC = 0
	for d := windowStart; d.Before(windowEnd); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format("2006-01-02")
		rows, ok := sums[key]
		if !ok {
			return 0, false, false
		}
		covered := false
		dayPeak := int64(0)
		for k, totalMC := range rows {
			covered = true
			if !zombieIsUserNamespace(k.NS, syncedExact, syncedPrefixes) {
				continue
			}
			if totalMC > dayPeak {
				dayPeak = totalMC
			}
			if totalMC >= floorMC {
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

// evalZombieStillOn applies the still-on leg over the management
// digests on the association namespaces: CP CPU requests above the
// absolute floor. Rows absent in the trailing day read unknown (medium);
// present-but-zero reads known-not-on (silence).
func evalZombieStillOn(mgmtDigests []types.KeyedDigest, namespaces []string, now time.Time) (cpReqMC int64, high, known bool) {
	if len(namespaces) == 0 {
		return 0, false, false
	}
	nsSet := make(map[string]bool, len(namespaces))
	for _, ns := range namespaces {
		nsSet[ns] = true
	}
	cutoff := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	sum := int64(0)
	seen := false
	for _, kd := range mgmtDigests {
		if !nsSet[kd.Key.Namespace] || kd.Row.BucketDate.Before(cutoff) {
			continue
		}
		sum += kd.Row.CPURequestP50MC
		seen = true
	}
	if !seen {
		return 0, false, false
	}
	if sum < hcp.ControlPlaneCPUFloorMC {
		return sum, false, true
	}
	return sum, true, true
}

// zombieAPIMaxDaily is a best-effort corroborating read of trailing
// +Inf bucket deltas for signals context. Errors and absence yield
// unknown; it never gates evaluation (API counts are non-discriminating,
// #662). Counter resets resolve to the post-reset value.
func zombieAPIMaxDaily(sloRows []csv.SLORow, hcID string, windowStart, windowEnd time.Time, minSpan time.Duration) (float64, bool) {
	type point struct {
		count float64
		at    time.Time
	}
	byVerbDay := map[string]map[string][]point{}
	for _, r := range sloRows {
		if r.HCClusterID != hcID || !isInf(r.Le) {
			continue
		}
		if r.CollectedAt.Before(windowStart) || !r.CollectedAt.Before(windowEnd) {
			continue
		}
		day := r.CollectedAt.UTC().Format("2006-01-02")
		if byVerbDay[r.VerbGroup] == nil {
			byVerbDay[r.VerbGroup] = map[string][]point{}
		}
		byVerbDay[r.VerbGroup][day] = append(byVerbDay[r.VerbGroup][day], point{count: float64(r.BucketCount), at: r.CollectedAt})
	}
	totals := map[string]float64{}
	for _, byDay := range byVerbDay {
		for day, pts := range byDay {
			if len(pts) < 2 || pts[len(pts)-1].at.Sub(pts[0].at) < minSpan {
				continue
			}
			delta := pts[len(pts)-1].count - pts[0].count
			if delta < 0 {
				delta = pts[len(pts)-1].count
			}
			totals[day] += delta
		}
	}
	if len(totals) == 0 {
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

func isInf(v float64) bool { return v > 1e308 }

// poolDayState is one day's parked reading across an HC's pools.
type poolDayState struct {
	day       string
	snapshots int
	allZero   bool
}

// zombiePoolDayStates reads per-day pool snapshots for one HC in range.
// A day with no rows is absent (unknown); autoscaling strings never gate
// (zero means zero, whatever the mode).
func zombiePoolDayStates(rows []csv.NodepoolRow, hcID string, start, end time.Time) map[string]*poolDayState {
	out := map[string]*poolDayState{}
	for _, r := range rows {
		if r.HCClusterID != hcID {
			continue
		}
		if r.WindowStart.Before(start) || r.WindowEnd.After(end) {
			continue
		}
		day := r.WindowStart.UTC().Format("2006-01-02")
		st, ok := out[day]
		if !ok {
			st = &poolDayState{day: day, allZero: true}
			out[day] = st
		}
		st.snapshots++
		if r.SpecReplicas != 0 || r.StatusReplicas != 0 {
			st.allZero = false
		}
	}
	return out
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

// zombieClampedWindow resolves the tenant-tunable idle window with the
// server's sane-bounds clamp (1-90, belt and suspenders against direct
// Policy construction).
func zombieClampedWindow(p ZombiePolicy) int {
	if p.IdleWindowDays < 1 || p.IdleWindowDays > 90 {
		return 14
	}
	return p.IdleWindowDays
}

// zombieAdvisory assembles the fired row. Per-verdict column semantics
// (apitax precedent): the peak/floor ride the details; HC identity and
// CP/API evidence ride the signals — byte-parity with the server's
// writeZombieAdvisory.
func zombieAdvisory(hcID string, peakUserMC, floorMC, cpReqMC int64, apiMax float64, apiKnown bool, confidence string, windowStart, windowEnd time.Time, lane ...string) *Advisory {
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
	details := map[string]any{
		"peak_user_cpu_mc":             peakUserMC,
		"idle_floor_cpu_mc":            floorMC,
		"control_plane_cpu_request_mc": cpReqMC,
	}
	if apiKnown {
		details["api_max_daily_requests"] = apiMax
	}
	return &Advisory{
		HostedClusterID: hcID,
		Verdict:         ZombieVerdict,
		Confidence:      confidence,
		WindowStart:     windowStart,
		WindowEnd:       windowEnd,
		Details:         details,
		Evidence:        signals,
	}
}

// EvaluateZombieStandard fires at most one standard-lane advisory per HC.
// Cluster routing (locked): the idle leg reads HOSTED digests keyed by the
// HC's own cluster ID (N-leg convention); still-on reads MANAGEMENT
// digests over the association namespaces. Never the reverse. Returns nil
// on silence (the server's never-fail-on-evidence posture).
func EvaluateZombieStandard(in ZombieInput) *Advisory {
	if !in.SnapshotFresh {
		return nil
	}
	windowEnd := in.Now.UTC().Truncate(24 * time.Hour)
	windowStart := windowEnd.AddDate(0, 0, -zombieClampedWindow(in.Policy))
	return evaluateZombieLanes(in, windowStart, windowEnd)
}

// EvaluateZombieShort fires at most one short-lane advisory per HC: the
// pool-zero fast path (#674). No nodepool evidence means no short
// evaluation — the standard lane owns those HCs.
func EvaluateZombieShort(in ZombieInput) *Advisory {
	if !in.SnapshotFresh || in.Policy.ShortWindowDays <= 0 || in.Policy.ShortRequiredDays <= 0 {
		return nil
	}
	windowDays := in.Policy.ShortWindowDays
	requiredDays := in.Policy.ShortRequiredDays
	if requiredDays > windowDays {
		requiredDays = windowDays
	}
	windowEnd := in.Now.UTC().Truncate(24 * time.Hour)
	windowStart := windowEnd.AddDate(0, 0, -windowDays)
	states := zombiePoolDayStates(in.NodepoolRows, in.HcID, windowStart, windowEnd)
	if len(states) == 0 {
		return nil
	}
	covered, parked := evalPoolParked(states, windowStart, windowDays, requiredDays)
	if !parked {
		return nil
	}
	lane := []string{
		fmt.Sprintf("lane:short-%dd", int(windowEnd.Sub(windowStart).Hours()/24)),
		fmt.Sprintf("pool_zero_days:%d", covered),
	}
	got := evaluateZombieLanes(in, windowStart, windowEnd, lane...)
	// The short lane's idle legs evaluate the SHORT window; a covered-day
	// requirement already passed above. evaluateZombieLanes handles the
	// rest; on still-on unknown it still fires medium (server parity).
	return got
}

// evaluateZombieLanes runs the shared dual legs over one window.
func evaluateZombieLanes(in ZombieInput, windowStart, windowEnd time.Time, lane ...string) *Advisory {
	sums := ZombieDaySums(in.HostedDigests)
	peak, idle, known := evalZombieIdle(sums, windowStart, windowEnd, in.Policy.IdleCPUFloorMC, in.SyncedExact, in.SyncedPrefixes)
	if !known || !idle {
		return nil
	}
	if len(in.AssocNamespaces) == 0 {
		return nil
	}
	cpReq, stillOn, stillKnown := evalZombieStillOn(in.MgmtDigests, in.AssocNamespaces, in.Now)
	apiMax, apiKnown := zombieAPIMaxDaily(in.SLORows, in.HcID, windowStart.AddDate(0, 0, -1), windowEnd, time.Duration(in.Policy.MinDataMinutes)*time.Minute)
	if !stillKnown {
		// Digests absent: snapshot + idle without CP proof — medium.
		return zombieAdvisory(in.HcID, peak, in.Policy.IdleCPUFloorMC, 0, apiMax, apiKnown, "medium", windowStart, windowEnd, lane...)
	}
	if !stillOn {
		return nil
	}
	return zombieAdvisory(in.HcID, peak, in.Policy.IdleCPUFloorMC, cpReq, apiMax, apiKnown, "high", windowStart, windowEnd, lane...)
}
