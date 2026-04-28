package ingestion

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMinFloat_Empty(t *testing.T) {
	assert.Equal(t, 0.0, minFloat(nil))
	assert.Equal(t, 0.0, minFloat([]float64{}))
}

func TestMinFloat_SingleValue(t *testing.T) {
	assert.Equal(t, 3.5, minFloat([]float64{3.5}))
}

func TestMinFloat_MultipleValues(t *testing.T) {
	assert.Equal(t, -1.0, minFloat([]float64{2.0, 5.0, -1.0, 0.0}))
}

func TestMaxFloat_Empty(t *testing.T) {
	assert.Equal(t, 0.0, maxFloat(nil))
	assert.Equal(t, 0.0, maxFloat([]float64{}))
}

func TestMaxFloat_SingleValue(t *testing.T) {
	assert.Equal(t, 7.25, maxFloat([]float64{7.25}))
}

func TestMaxFloat_MultipleValues(t *testing.T) {
	assert.Equal(t, 100.0, maxFloat([]float64{1.0, 100.0, 42.0}))
}

func TestMeanFloat_Empty(t *testing.T) {
	assert.Equal(t, 0.0, meanFloat(nil))
	assert.Equal(t, 0.0, meanFloat([]float64{}))
}

func TestMeanFloat_SingleValue(t *testing.T) {
	assert.Equal(t, 12.0, meanFloat([]float64{12.0}))
}

func TestMeanFloat_MultipleValues(t *testing.T) {
	assert.InDelta(t, 2.5, meanFloat([]float64{1.0, 2.0, 3.0, 4.0}), 1e-12)
}

func TestHasGPU_WithModelName(t *testing.T) {
	r := &MetricRow{AcceleratorModelName: "NVIDIA A100-SXM4-40GB"}
	assert.True(t, r.HasGPU())
}

func TestHasGPU_EmptyModel(t *testing.T) {
	r := &MetricRow{AcceleratorModelName: ""}
	assert.False(t, r.HasGPU())
}

func TestHasGPU_OnlyFBUsage(t *testing.T) {
	r := &MetricRow{
		AcceleratorModelName:  "",
		AcceleratorFBUsageMin:   1,
		AcceleratorFBUsageMax:   8,
		AcceleratorFBUsageAvg: 4,
	}
	assert.False(t, r.HasGPU())
}
