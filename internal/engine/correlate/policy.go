// Package correlate implements the thin cross-plane correlator (#646,
// implements #625): precision-first H && C && !N evaluation producing
// batch-written advisories. Binary confidence (emit high-confidence or
// silence); advisory-only (never suppresses existing recommendations);
// single full-language audience (customer-safe copy lives in #620).
package correlate

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// Policy carries the numeric gate. Compiled defaults mirror the #628 table;
// policyForOrg overlays tenant/admin values via the #645 settings domain,
// falling back here on any resolution failure.
type Policy struct {
	HAbsoluteThresholdS  float64
	HBaselineMultiple    float64
	HWindowHours         int
	HMinDataMinutes      int
	CPUThresholdPct      float64
	SkewToleranceMinutes int
	FreshnessHours       int
	AdvisoryExpiryHours  int
	// ZombieIdleReqPerDay caps hosted requests/day counting as idle (W3,
	// #658). Tenant-tunable like other thresholds; the 14d window is a
	// const, not a knob.
	ZombieIdleReqPerDay float64
	// ZombieIdleCPUFloorMC is the daily per-workload CPU at or above
	// which a non-system workload counts as active (W3, #664).
	// Discouraged to tune; see the configurability guide.
	ZombieIdleCPUFloorMC int
	// ZombieShortWindowDays bounds the pool-zero fast lane (Child B,
	// #674): trailing days evaluated when pools read parked.
	ZombieShortWindowDays int
	// ZombieShortRequiredDays floors covered pool-evidence days.
	ZombieShortRequiredDays int
	// ZombieIdleWindowDays bounds the standard idle evaluation (#664
	// 14d, tunable since the const-lock overturn).
	ZombieIdleWindowDays int
	// NodeFreshnessHours calibrates N to the daily node pipeline: 2h would
	// permanently silence N (and thus the correlator) on daily uploads, so N
	// freshness follows ingest cadence. Lab controls (#646) own recalibration.
	NodeFreshnessHours int
}

// DefaultPolicy returns the locked compiled defaults (#625 close draft).
func DefaultPolicy() Policy {
	return Policy{
		HAbsoluteThresholdS:  0.30,
		HBaselineMultiple:    3,
		HWindowHours:         1,
		HMinDataMinutes:      30,
		CPUThresholdPct:      80,
		SkewToleranceMinutes: 5,
		FreshnessHours:       2,
		AdvisoryExpiryHours:  24,
		ZombieIdleReqPerDay:  100,
		ZombieIdleCPUFloorMC: 10,
		ZombieShortWindowDays:  3,
		ZombieShortRequiredDays: 3,
		ZombieIdleWindowDays:    14,
		NodeFreshnessHours:   36,
	}
}

// PolicyFromSettings converts tenant/admin-resolved settings into the
// evaluator policy. Field-for-field by construction; drift between the two
// shapes fails the TestPolicyFromSettings_MirrorsDefaults test, not review.
func PolicyFromSettings(s engine.HCPCorrelationSettings) Policy {
	return Policy{
		HAbsoluteThresholdS:  s.HP99ThresholdS,
		HBaselineMultiple:    s.HBaselineMultiple,
		HWindowHours:         s.WindowHours,
		// HMinDataMinutes and NodeFreshnessHours are methodological floors,
		// not tuning knobs: no tenant field exists for them by design (#628).
		HMinDataMinutes:      DefaultPolicy().HMinDataMinutes,
		CPUThresholdPct:      s.CCPUPct,
		SkewToleranceMinutes: s.SkewMinutes,
		FreshnessHours:       s.FreshnessHours,
		AdvisoryExpiryHours:  s.ExpiryHours,
		ZombieIdleReqPerDay:  s.ZombieIdleReqPerDay,
		ZombieIdleCPUFloorMC: s.ZombieIdleCPUFloorMC,
		ZombieShortWindowDays:  s.ZombieShortWindowDays,
		ZombieShortRequiredDays: s.ZombieShortRequiredDays,
		ZombieIdleWindowDays:    s.ZombieIdleWindowDays,
		NodeFreshnessHours:   DefaultPolicy().NodeFreshnessHours,
	}
}

// policyForOrg resolves tenant/admin policy, falling back to compiled
// defaults on any resolution failure (never fail the run on settings I/O).
func policyForOrg(ctx context.Context, pool *pgxpool.Pool, orgID string) Policy {
	s, err := engine.ResolveHCPCorrelationSettings(ctx, pool, orgID)
	if err != nil {
		logging.ForOrg(orgID, "").Warnf("correlator: policy resolve failed, compiled defaults: %v", err)
		return DefaultPolicy()
	}
	return PolicyFromSettings(s)
}

// Advice is one evaluated HC window. Fire is true only for high-confidence
// H && C && !N with all three inputs known; every other combination is
// silence (no row written).
type Advice struct {
	Fire         bool
	HCP99        float64
	HThreshold   float64
	CRatio       float64
	NSignals     []string
	ManagementID string
}

// Evaluate applies the precision-first rule over known/unknown evidence.
// Unknown anywhere means silence — never proof of health, never blame.
func Evaluate(hHigh, hKnown, cHigh, cKnown, nPressured, nKnown bool, hP99, hThreshold, cRatio float64, nSignals []string, mgmtID string) Advice {
	if !(hKnown && cKnown && nKnown) {
		return Advice{}
	}
	if !(hHigh && cHigh && !nPressured) {
		return Advice{}
	}
	return Advice{Fire: true, HCP99: hP99, HThreshold: hThreshold, CRatio: cRatio, NSignals: nSignals, ManagementID: mgmtID}
}
