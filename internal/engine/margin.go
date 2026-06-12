package engine

// ComputeAdaptiveMargin calculates a safety margin based on workload variability.
// Formula: min(maxMargin, max(minMargin, 1.0 + (p95 - p50) / mean))
// Returns minMargin when mean is zero (idle/empty workload guard).
//
// Deprecated: prefer ComputeAdaptiveMarginScaledDirect for the hot path; this wrapper
// exists for float-based callers and tests.
func ComputeAdaptiveMargin(p95, p50, mean int64, minMargin, maxMargin float64) float64 {
	scaled := ComputeAdaptiveMarginScaledDirect(p95, p50, mean, minMargin, maxMargin)
	return float64(scaled) / float64(MarginScale)
}
