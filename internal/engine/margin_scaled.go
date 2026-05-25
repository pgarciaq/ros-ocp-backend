package engine

import "math"

// MarginScale is the fixed-point scale for adaptive margin (4 decimal places).
const MarginScale int64 = 10000

// ScaleMargin converts a float margin (e.g. 1.15) to scaled int64 (11500).
func ScaleMargin(m float64) int64 {
	return int64(math.Round(m * float64(MarginScale)))
}

// ComputeAdaptiveMarginScaled returns margin scaled by MarginScale.
func ComputeAdaptiveMarginScaled(p95, p50, mean int64, minMargin, maxMargin float64) int64 {
	return ScaleMargin(ComputeAdaptiveMargin(p95, p50, mean, minMargin, maxMargin))
}

// ApplyScaledMargin multiplies value by scaled margin with rounding: (value*marginScaled + MarginScale/2) / MarginScale.
func ApplyScaledMargin(value, marginScaled int64) int64 {
	if value == 0 || marginScaled == 0 {
		return 0
	}
	return (value*marginScaled + MarginScale/2) / MarginScale
}

// ScaleLimitMultiplier converts a limit multiplier float to scaled int64.
func ScaleLimitMultiplier(m float64) int64 {
	return int64(math.Round(m * float64(MarginScale)))
}

// ApplyOOMBumpScaled applies an OOM bump using scaled integer arithmetic.
// bumpScaled = MarginScale + round(OOMBaseBump * log2(1+count) * MarginScale), capped at maxBumpScaled.
func ApplyOOMBumpScaled(value int64, oomCount int64, oomBaseBump, oomMaxBump float64) int64 {
	if value == 0 || oomCount <= 0 {
		return value
	}
	bumpAdd := int64(math.Round(oomBaseBump * math.Log2(1+float64(oomCount)) * float64(MarginScale)))
	bumpScaled := MarginScale + bumpAdd
	maxBumpScaled := ScaleMargin(oomMaxBump)
	if bumpScaled > maxBumpScaled {
		bumpScaled = maxBumpScaled
	}
	return ApplyScaledMargin(value, bumpScaled)
}
