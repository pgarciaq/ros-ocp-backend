package engine

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// computeAdaptiveMarginFloat is the pre-M5 float reference for equivalence checks.
func computeAdaptiveMarginFloat(p95, p50, mean int64, minMargin, maxMargin float64) float64 {
	if mean <= 0 {
		return minMargin
	}
	cv := float64(p95-p50) / float64(mean)
	margin := 1.0 + cv
	return math.Min(maxMargin, math.Max(minMargin, margin))
}

func TestComputeAdaptiveMarginScaledDirect_EquivalenceWithFloat(t *testing.T) {
	cases := []struct {
		name      string
		p95, p50  int64
		mean      int64
		minMargin float64
		maxMargin float64
	}{
		{"stable", 105, 100, 100, 1.15, 1.50},
		{"variable", 300, 100, 120, 1.15, 1.50},
		{"medium", 200, 100, 150, 1.15, 1.50},
		{"low_var", 120, 100, 110, 1.15, 1.50},
		{"zero_mean", 0, 0, 0, 1.15, 1.50},
		{"negative_spread", 80, 100, 100, 1.15, 1.50},
		{"rounding_edge", 121, 100, 111, 1.15, 1.50},
		{"large_values", 500000, 100000, 200000, 1.10, 2.00},
		{"tiny_mean", 10, 5, 1, 1.15, 1.50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			floatScaled := ScaleMargin(computeAdaptiveMarginFloat(tc.p95, tc.p50, tc.mean, tc.minMargin, tc.maxMargin))
			direct := ComputeAdaptiveMarginScaledDirect(tc.p95, tc.p50, tc.mean, tc.minMargin, tc.maxMargin)
			diff := direct - floatScaled
			if diff < 0 {
				diff = -diff
			}
			assert.LessOrEqual(t, diff, int64(1), "direct=%d floatScaled=%d", direct, floatScaled)
		})
	}
}

func TestComputeAdaptiveMargin_StableWorkload_15Percent(t *testing.T) {
	// p95 ≈ p50 ≈ mean → CV near zero → clamps to min margin
	margin := ComputeAdaptiveMargin(105, 100, 100, 1.15, 1.50)
	assert.InDelta(t, 1.15, margin, 0.01)
}

func TestComputeAdaptiveMargin_VariableWorkload_50Percent(t *testing.T) {
	// p95 = 3x p50 → CV = (300-100)/120 = 1.67 → clamps to max
	margin := ComputeAdaptiveMargin(300, 100, 120, 1.15, 1.50)
	assert.InDelta(t, 1.50, margin, 0.01)
}

func TestComputeAdaptiveMargin_MediumVariability(t *testing.T) {
	// p95=200, p50=100, mean=150 → CV = (200-100)/150 = 0.667 → 1 + 0.667 = 1.667 → clamp to 1.50
	margin := ComputeAdaptiveMargin(200, 100, 150, 1.15, 1.50)
	assert.InDelta(t, 1.50, margin, 0.01)
}

func TestComputeAdaptiveMargin_LowVariability(t *testing.T) {
	// p95=120, p50=100, mean=110 → CV = 20/110 = 0.18 → 1.18 (between min and max)
	margin := ComputeAdaptiveMargin(120, 100, 110, 1.15, 1.50)
	assert.InDelta(t, 1.18, margin, 0.01)
}

func TestComputeAdaptiveMargin_ZeroMean_UsesMinMargin(t *testing.T) {
	margin := ComputeAdaptiveMargin(0, 0, 0, 1.15, 1.50)
	assert.InDelta(t, 1.15, margin, 0.01)
}

func TestComputeAdaptiveMarginScaled_MatchesDirect(t *testing.T) {
	assert.Equal(t,
		ComputeAdaptiveMarginScaledDirect(120, 100, 110, 1.15, 1.50),
		ComputeAdaptiveMarginScaled(120, 100, 110, 1.15, 1.50),
	)
}
