// Package correlate implements the thin cross-plane correlator (#646,
// implements #625): precision-first H && C && !N evaluation producing
// batch-written advisories. Binary confidence (emit high-confidence or
// silence); advisory-only (never suppresses existing recommendations);
// single full-language audience (customer-safe copy lives in #620).
package correlate

// Policy carries the numeric gate. Compiled defaults mirror the #628 table;
// ResolvePolicy overlays tenant/admin values when the #645 settings domain
// lands (today it returns defaults and says so).
type Policy struct {
	HAbsoluteThresholdS  float64
	HBaselineMultiple    float64
	HWindowHours         int
	HMinDataMinutes      int
	CPUThresholdPct      float64
	SkewToleranceMinutes int
	FreshnessHours       int
	AdvisoryExpiryHours  int
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
		NodeFreshnessHours:   36,
	}
}

// ResolvePolicy returns the effective policy for an org. Today: compiled
// defaults (#645 wires ROS_HCP_* locks + tenant overrides into this seam).
func ResolvePolicy(_ string) Policy {
	return DefaultPolicy()
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
