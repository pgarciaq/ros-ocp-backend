package engine

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyGPUSavings_NilCostData(t *testing.T) {
	rec := &GPURec{Classification: GPUClassIdle, GPUModelName: "A100"}
	ApplyGPUSavings(rec, nil)
	assert.Nil(t, rec.EstimatedGPUSavingsUSD)
}

func TestApplyGPUSavings_NoGPURate(t *testing.T) {
	rec := &GPURec{Classification: GPUClassIdle, GPUModelName: "A100"}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"cpu_core_usage_per_hour": {Infrastructure: 0, Supplementary: 0.007},
		},
	}
	ApplyGPUSavings(rec, cd)
	assert.Nil(t, rec.EstimatedGPUSavingsUSD)
}

func TestApplyGPUSavings_IdleGPU(t *testing.T) {
	rec := &GPURec{Classification: GPUClassIdle, GPUModelName: "A100"}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"gpu_cost_per_month": {Infrastructure: 500, Supplementary: 100},
		},
	}
	ApplyGPUSavings(rec, cd)
	require.NotNil(t, rec.EstimatedGPUSavingsUSD)
	assert.InDelta(t, 600.0, float64(*rec.EstimatedGPUSavingsUSD), 0.01)
}

func TestApplyGPUSavings_MIGRightSizing_A100_80GB(t *testing.T) {
	rec := &GPURec{
		Classification:        GPUClassUnderutilized,
		GPUModelName:          "NVIDIA A100-SXM4-80GB",
		RecommendedGPUProfile: "1g.10gb",
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"gpu_cost_per_month": {Infrastructure: 0, Supplementary: 700},
		},
	}
	ApplyGPUSavings(rec, cd)
	require.NotNil(t, rec.EstimatedGPUSavingsUSD)

	// A100-80GB: "1g.10gb" = 1 slice, "7g.80gb" = 7 slices (largest profile)
	// savings = (1 - 1/7) * 700 = 600
	assert.InDelta(t, 600.0, float64(*rec.EstimatedGPUSavingsUSD), 1.0)
}

func TestApplyGPUSavings_MIGRightSizing_FullGPU(t *testing.T) {
	rec := &GPURec{
		Classification:        GPUClassUnderutilized,
		GPUModelName:          "NVIDIA A100-SXM4-80GB",
		RecommendedGPUProfile: "full_gpu",
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"gpu_cost_per_month": {Infrastructure: 700, Supplementary: 0},
		},
	}
	ApplyGPUSavings(rec, cd)
	// full_gpu means no MIG savings, but cost data is available so we report $0
	require.NotNil(t, rec.EstimatedGPUSavingsUSD)
	assert.InDelta(t, 0.0, float64(*rec.EstimatedGPUSavingsUSD), 0.01)
}

func TestApplyGPUSavings_WellUtilized(t *testing.T) {
	rec := &GPURec{
		Classification: GPUClassWellUtilized,
		GPUModelName:   "A100",
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"gpu_cost_per_month": {Infrastructure: 500, Supplementary: 0},
		},
	}
	ApplyGPUSavings(rec, cd)
	// Well-utilized: cost data available, savings = $0 (not nil)
	require.NotNil(t, rec.EstimatedGPUSavingsUSD)
	assert.InDelta(t, 0.0, float64(*rec.EstimatedGPUSavingsUSD), 0.01)
}

func TestApplyGPUSavings_NonMIGGPU_Underutilized(t *testing.T) {
	rec := &GPURec{
		Classification:        GPUClassUnderutilized,
		GPUModelName:          "Tesla T4",
		RecommendedGPUProfile: "", // T4 doesn't support MIG
	}
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"gpu_cost_per_month": {Infrastructure: 300, Supplementary: 0},
		},
	}
	ApplyGPUSavings(rec, cd)
	// No MIG profile recommended, cost data available, so savings = $0
	require.NotNil(t, rec.EstimatedGPUSavingsUSD)
	assert.InDelta(t, 0.0, float64(*rec.EstimatedGPUSavingsUSD), 0.01)
}

func TestApplyGPUSavings_NilRec(t *testing.T) {
	cd := &costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"gpu_cost_per_month": {Infrastructure: 500, Supplementary: 0},
		},
	}
	ApplyGPUSavings(nil, cd) // should not panic
}

func TestGpuMonthlyRate(t *testing.T) {
	assert.Equal(t, 0.0, GPUMonthlyRate(nil))
	assert.Equal(t, 0.0, GPUMonthlyRate(&costdata.ClusterCostData{}))
	assert.Equal(t, 0.0, GPUMonthlyRate(&costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{},
	}))
	assert.InDelta(t, 700.0, GPUMonthlyRate(&costdata.ClusterCostData{
		ConfiguredRates: map[string]costdata.RatePair{
			"gpu_cost_per_month": {Infrastructure: 500, Supplementary: 200},
		},
	}), 0.01)
}

func TestMigTotalSlices(t *testing.T) {
	spec := MatchGPUModel("NVIDIA A100-SXM4-80GB")
	require.NotNil(t, spec)
	assert.Equal(t, 7, migTotalSlices(spec))
}

func TestMigProfileSlices(t *testing.T) {
	spec := MatchGPUModel("NVIDIA A100-SXM4-80GB")
	require.NotNil(t, spec)
	assert.Equal(t, 1, migProfileSlices(spec, "1g.10gb"))
	assert.Equal(t, 3, migProfileSlices(spec, "3g.40gb"))
	assert.Equal(t, 7, migProfileSlices(spec, "7g.80gb"))
	assert.Equal(t, 0, migProfileSlices(spec, "nonexistent"))
}
