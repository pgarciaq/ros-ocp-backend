package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
