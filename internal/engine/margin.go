package engine

import "math"

// ComputeAdaptiveMargin calculates a safety margin based on workload variability.
// Formula: min(maxMargin, max(minMargin, 1.0 + (p95 - p50) / mean))
// Returns minMargin when mean is zero (idle/empty workload guard).
func ComputeAdaptiveMargin(p95, p50, mean int64, minMargin, maxMargin float64) float64 {
	if mean <= 0 {
		return minMargin
	}
	cv := float64(p95-p50) / float64(mean)
	margin := 1.0 + cv
	return math.Min(maxMargin, math.Max(minMargin, margin))
}
