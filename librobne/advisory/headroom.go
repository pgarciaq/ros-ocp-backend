// W2 headroom rule (#723 robne parity; server rule
// internal/engine/correlate/{evaluate,job}.go — the precision-first H &&
// C && !N correlator).
//
// Per hosted cluster with bucket evidence plus a window-proven
// association, over the trailing hourly window:
//   - H: hosted API latency — max verb-group p99 vs
//     max(absolute threshold, baseline multiple × the 7-day median of
//     daily p99s), computed with the shared quantile path (no second
//     math).
//   - C: management control-plane stress for the HC's association
//     namespaces — trailing-day usage/requests ratio vs the threshold.
//     Attribution-grade evidence only (the strict association map);
//     a guardrail union would mix sibling HCs' CPUs into this HC's
//     verdict.
//   - !N: hosted worker calm — the computed node recommendations carry
//     no overcommitted (12) or scheduling-limited (74) codes.
//
// Fire do_not_add_workers_first with high confidence only when all three
// legs are known and point the same way. Unknown anywhere is silence —
// never proof of health, never blame. The window proof requires a
// complete association snapshot observed no earlier than the window
// (minus skew) with one incarnation (a recreated HC fails there).
//
// Offline shape (ADR-0350): the SLO rows are identity-joined on
// hc_cluster_id == the primary cluster; the C leg reads the context
// payload's digests over the association namespaces; the N leg reads the
// node recommendations robne itself computes from the primary payload.
package advisory

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// HeadroomVerdict is the W2 headroom advisory verdict (#717's serving
// contract) — the server's only v1 verdict, kept verbatim.
const HeadroomVerdict = "do_not_add_workers_first"

// HeadroomSkewToleranceMinutes is the association-proof skew (the
// server's SkewToleranceMinutes, a methodological const offline).
const HeadroomSkewToleranceMinutes = 5

// HeadroomMinDataMinutes is the min-data methodological floor (no tenant
// knob exists for it by design, #628).
const HeadroomMinDataMinutes = 30

// HeadroomPolicy carries the W2 evaluation knobs (the server's per-org
// correlator settings, offline as CLI config).
type HeadroomPolicy struct {
	HAbsoluteThresholdS float64
	HBaselineMultiple   float64
	HWindowHours        int
	CPUThresholdPct     float64
}

// DefaultHeadroomPolicy mirrors the server's compiled defaults
// (DefaultPolicy in internal/engine/correlate).
func DefaultHeadroomPolicy() HeadroomPolicy {
	return HeadroomPolicy{HAbsoluteThresholdS: 0.30, HBaselineMultiple: 3, HWindowHours: 1, CPUThresholdPct: 80}
}

// HeadroomInput is one HC's offline evaluation facts.
type HeadroomInput struct {
	HcID string
	Now  time.Time
	// SLORows: the HC's API bucket snapshots (any payload;
	// identity-joined on HCClusterID).
	SLORows []csv.SLORow
	// MgmtDigests: the management cluster's digests (context payload).
	MgmtDigests []types.KeyedDigest
	// AssocNamespaces: the association map's namespaces for this HC.
	AssocNamespaces []string
	// NodeNotifications: the notification codes of the computed node
	// recommendations for the primary payload (the N leg reads codes
	// 12 overcommitted / 74 scheduling-limited).
	NodeNotifications []int16
	// NodeRecsExist: node recommendations were computable at all —
	// absent recs read unknown, never calm.
	NodeRecsExist bool
	// SnapshotObserved: the latest complete association snapshot's
	// observation time for this HC (the window proof).
	SnapshotObserved time.Time
	// SnapshotConsistent: every complete snapshot for this HC names one
	// incarnation (the strict #621 rule).
	SnapshotConsistent bool
	Policy             HeadroomPolicy
}

// evalHeadroom reports hosted pain: max group p99 vs max(absolute,
// multiple of the 7-day baseline median). Baseline = median of daily
// p99s (day-aggregated deltas), computed with the same quantile path.
func evalHeadroom(rows []csv.SLORow, hcID string, windowEnd time.Time, p HeadroomPolicy) (p99, threshold float64, high, known bool) {
	windowStart := windowEnd.Add(-time.Duration(p.HWindowHours) * time.Hour)
	minSpan := time.Duration(HeadroomMinDataMinutes) * time.Minute
	// One span covers the fire window plus the 7-day baseline (previous
	// day-aggregated snapshots double as delta baselines).
	var all []Sample
	for _, r := range rows {
		if r.HCClusterID != hcID {
			continue
		}
		all = append(all, Sample{Group: r.VerbGroup, LE: r.Le, Count: float64(r.BucketCount), Collected: r.CollectedAt})
	}
	if len(all) == 0 {
		return 0, 0, false, false
	}
	groups, ok := WindowP99(all, windowStart, windowEnd, minSpan)
	if !ok {
		return 0, 0, false, false
	}
	p99 = 0
	for _, v := range groups {
		if v > p99 {
			p99 = v
		}
	}
	// Baseline: one p99 per trailing day from day-aggregated deltas.
	var daily []float64
	for d := 1; d <= 7; d++ {
		dayEnd := windowStart.AddDate(0, 0, -(d - 1))
		dayStart := dayEnd.AddDate(0, 0, -1)
		dayGroups, ok := WindowP99(all, dayStart, dayEnd, 0)
		if !ok {
			continue
		}
		dayMax := 0.0
		for _, v := range dayGroups {
			if v > dayMax {
				dayMax = v
			}
		}
		daily = append(daily, dayMax)
	}
	median, ok := MedianFloat64(daily)
	if !ok {
		median = 0
	}
	threshold = p.HAbsoluteThresholdS
	if rel := p.HBaselineMultiple * median; rel > threshold {
		threshold = rel
	}
	return p99, threshold, p99 > threshold, true
}

// evalCapacity reports management control-plane stress for the HC's
// namespaces: trailing-day usage/requests ratio vs threshold. Rows
// absent, or requests zero, read unknown — never calm-by-absence.
func evalCapacity(mgmtDigests []types.KeyedDigest, namespaces []string, now time.Time, p HeadroomPolicy) (ratio float64, high, known bool) {
	if len(namespaces) == 0 {
		return 0, false, false
	}
	nsSet := make(map[string]bool, len(namespaces))
	for _, ns := range namespaces {
		nsSet[ns] = true
	}
	cutoff := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	var usage, requests int64
	seen := false
	for _, kd := range mgmtDigests {
		if !nsSet[kd.Key.Namespace] || kd.Row.BucketDate.Before(cutoff) {
			continue
		}
		usage += kd.Row.CPUUsageP95MC
		requests += kd.Row.CPURequestP50MC
		seen = true
	}
	if !seen || requests <= 0 {
		return 0, false, false
	}
	ratio = float64(usage) / float64(requests)
	return ratio, ratio*100 > p.CPUThresholdPct, true
}

// evalNodesPressure reports hosted worker pressure from the computed node
// recommendations: pressured iff any carries code 12 (overcommitted) or
// 74 (scheduling-limited). Absent recs read unknown — never as calm.
func evalNodesPressure(notifications []int16, recsExist bool) (pressured, known bool, signals []string) {
	if !recsExist {
		return false, false, nil
	}
	hotCodes := map[string]bool{}
	for _, c := range notifications {
		if c == 12 || c == 74 {
			hotCodes[codeString(c)] = true
		}
	}
	for s := range hotCodes {
		signals = append(signals, s)
	}
	sortStrings(signals)
	return len(hotCodes) > 0, true, signals
}

func codeString(c int16) string {
	if c == 0 {
		return "0"
	}
	neg := c < 0
	if neg {
		c = -c
	}
	var buf [8]byte
	i := len(buf)
	for c > 0 {
		i--
		buf[i] = byte('0' + c%10)
		c /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// windowProof requires the association to be proven FOR this window, not
// merely sometime in the lookback: a complete snapshot observed no
// earlier than windowStart (minus skew), with one incarnation (a
// recreated HC fails here, not downstream).
func windowProof(observed time.Time, consistent bool, windowStart time.Time) bool {
	if !consistent {
		return false
	}
	floor := windowStart.Add(-time.Duration(HeadroomSkewToleranceMinutes) * time.Minute)
	return !observed.Before(floor)
}

// EvaluateHeadroom applies the precision-first rule over known/unknown
// evidence. Returns nil on silence (the server's never-fail-on-evidence
// posture): unknown anywhere, calm legs, pressured workers, or an
// unproven window all produce nothing.
func EvaluateHeadroom(in HeadroomInput) *Advisory {
	windowEnd := in.Now.UTC().Truncate(time.Hour)
	windowStart := windowEnd.Add(-time.Duration(in.Policy.HWindowHours) * time.Hour)

	hP99, hThreshold, hHigh, hKnown := evalHeadroom(in.SLORows, in.HcID, windowEnd, in.Policy)
	if !hKnown {
		return nil
	}
	cRatio, cHigh, cKnown := evalCapacity(in.MgmtDigests, in.AssocNamespaces, in.Now, in.Policy)
	if !cKnown {
		return nil
	}
	nPressured, nKnown, nSignals := evalNodesPressure(in.NodeNotifications, in.NodeRecsExist)
	if !nKnown {
		return nil
	}
	if !windowProof(in.SnapshotObserved, in.SnapshotConsistent, windowStart) {
		return nil
	}
	if !hHigh || !cHigh || nPressured {
		return nil
	}
	// The fired row carries the N signals verbatim (the server's
	// writeAdvisory; on a fire they are the calm set — empty).
	signals := nSignals
	if signals == nil {
		signals = []string{}
	}
	return &Advisory{
		HostedClusterID: in.HcID,
		Verdict:         HeadroomVerdict,
		Confidence:      "high",
		WindowStart:     windowStart,
		WindowEnd:       windowEnd,
		Details: map[string]any{
			"hosted_api_p99_seconds":       hP99,
			"hosted_api_threshold_seconds": hThreshold,
			"control_plane_capacity_ratio": cRatio,
		},
		Evidence: signals,
	}
}
