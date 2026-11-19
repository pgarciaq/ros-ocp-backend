package vm

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

func vmRecForSavings() *Recommendation {
	return &Recommendation{
		ClusterUUID:          uuid.New(),
		CurrentVCPU:          8,
		CurrentMemoryGiB:     32,
		RecommendedVCPU:      4,
		RecommendedMemoryGiB: 16,
	}
}

// Rates are micro-cents per the metric's unit (#461), as Koku now emits them.
func vmCostData(cpuRate, memRate, gpuRate int64) *costdata.ClusterCostData {
	return vmCostDataWithVMRate(cpuRate, memRate, gpuRate, 0)
}

func vmCostDataWithVMRate(cpuRate, memRate, gpuRate, vmMonthlyRate int64) *costdata.ClusterCostData {
	rates := map[string]costdata.RatePair{
		"cpu_core_request_per_hour":  {Supplementary: cpuRate * 4 / 10},
		"cpu_core_usage_per_hour":    {Supplementary: cpuRate * 6 / 10},
		"memory_gb_request_per_hour": {Supplementary: memRate * 3 / 10},
		"memory_gb_usage_per_hour":   {Supplementary: memRate * 7 / 10},
		"gpu_cost_per_month":         {Infrastructure: gpuRate},
	}
	if vmMonthlyRate > 0 {
		rates["vm_cost_per_month"] = costdata.RatePair{Infrastructure: vmMonthlyRate}
	}
	return &costdata.ClusterCostData{
		Currency:        "EUR",
		ConfiguredRates: rates,
	}
}

func TestEffectiveCPUCoreHourlyRate_UsesMaxOfRequestAndUsage(t *testing.T) {
	cd := vmCostData(0, 0, 0)
	cd.ConfiguredRates["cpu_core_request_per_hour"] = costdata.RatePair{Supplementary: 20_000_000}
	cd.ConfiguredRates["cpu_core_usage_per_hour"] = costdata.RatePair{Supplementary: 50_000_000}
	assert.Equal(t, int64(50_000_000), engine.EffectiveCPUCoreHourlyRate(cd))
}

func TestComputeVMSavings_Downsize(t *testing.T) {
	rec := vmRecForSavings()
	cd := vmCostData(100_000_000, 200_000_000, 0)

	got := ComputeVMSavings(rec, cd, 730)
	require.NotNil(t, got)
	// effective cpu=0.6, mem=1.4 → (4)*0.6*730 + (16)*1.4*730 = 18104
	assert.InDelta(t, 18104.0, *got, 0.01)
}

func TestComputeVMSavings_Idle(t *testing.T) {
	rec := vmRecForSavings()
	rec.Category = VMCategoryIdle
	rec.RecommendedVCPU = 1
	rec.RecommendedMemoryGiB = 4
	cd := vmCostData(100_000_000, 200_000_000, 60_000_000_000)

	got := ComputeVMSavings(rec, cd, 730)
	require.NotNil(t, got)
	// 8*0.6*730 + 32*1.4*730 = 3504 + 32704 = 36208
	assert.InDelta(t, 36208.0, *got, 0.01)
}

func TestComputeVMSavings_IdleIncludesVMCostPerMonth(t *testing.T) {
	rec := vmRecForSavings()
	rec.Category = VMCategoryIdle
	cd := vmCostDataWithVMRate(0, 0, 0, 25_000_000_000)

	got := ComputeVMSavings(rec, cd, 730)
	require.NotNil(t, got)
	assert.InDelta(t, 250.0, *got, 0.01)
}

func TestComputeVMSavings_AbandonedWithGPU(t *testing.T) {
	rec := vmRecForSavings()
	rec.Category = VMCategoryAbandoned
	rec.GPUCount = 2
	cd := vmCostData(100_000_000, 200_000_000, 60_000_000_000)

	got := ComputeVMSavings(rec, cd, 730)
	require.NotNil(t, got)
	// 36208 + 2*600 = 37408
	assert.InDelta(t, 37408.0, *got, 0.01)
}

func TestComputeVMSavings_PowerOffCandidate(t *testing.T) {
	rec := vmRecForSavings()
	rec.Category = VMCategoryPowerOffCandidate
	bp := int32(7000)
	rec.PowerOffIdleRatio = &bp
	cd := vmCostData(100_000_000, 200_000_000, 0)

	got := ComputeVMSavings(rec, cd, 730)
	require.NotNil(t, got)
	// downsize base would apply if not power-off; power-off uses idle base * 0.7
	// idle base: 8*0.6*730 + 32*1.4*730 = 36208 → *0.7 = 25345.6
	assert.InDelta(t, 25345.6, *got, 0.01)
}

func TestComputeVMSavings_GPURemove(t *testing.T) {
	rec := vmRecForSavings()
	rec.RecommendedVCPU = 8
	rec.RecommendedMemoryGiB = 32
	rec.GPUCount = 1
	rec.GPUModel = "NVIDIA-A100-SXM4-80GB"
	rec.RecommendedGPUAction = vmGPUActionRemoveGPU
	cd := vmCostData(100_000_000, 200_000_000, 60_000_000_000)

	got := ComputeVMSavings(rec, cd, 730)
	require.NotNil(t, got)
	assert.InDelta(t, 600.0, *got, 0.01)
}

func TestComputeVMSavings_NilCostData(t *testing.T) {
	rec := vmRecForSavings()
	assert.Nil(t, ComputeVMSavings(rec, nil, 730))
}

func TestComputeVMSavings_NoRates(t *testing.T) {
	rec := vmRecForSavings()
	cd := &costdata.ClusterCostData{ConfiguredRates: map[string]costdata.RatePair{}}
	assert.Nil(t, ComputeVMSavings(rec, cd, 730))
}

func TestApplyVMSavings_Disabled(t *testing.T) {
	rec := vmRecForSavings()
	recs := []Recommendation{*rec}
	cd := vmCostData(100_000_000, 200_000_000, 0)

	ApplyVMSavings(recs, cd, false, 730)
	assert.Nil(t, recs[0].EstimatedSavingsCents)
	assert.Nil(t, recs[0].SavingsCurrency)
}

func TestApplyVMSavings_NilProviderData(t *testing.T) {
	rec := vmRecForSavings()
	recs := []Recommendation{*rec}

	ApplyVMSavings(recs, nil, true, 730)
	assert.Nil(t, recs[0].EstimatedSavingsCents)
}

func TestApplyVMSavings_CurrencyFromCostData(t *testing.T) {
	rec := vmRecForSavings()
	recs := []Recommendation{*rec}
	cd := vmCostData(100_000_000, 200_000_000, 0)

	ApplyVMSavings(recs, cd, true, 730)
	require.NotNil(t, recs[0].EstimatedSavingsCents)
	require.NotNil(t, recs[0].SavingsCurrency)
	assert.Equal(t, "EUR", *recs[0].SavingsCurrency)
}

func TestApplyVMSavings_NilCostDataProvider_EmptyRates(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	t.Setenv("KOKU_MASU_URL", "")

	provider := engine.RecalcCostDataProvider()
	_, ok := provider.(*costdata.NilCostDataProvider)
	require.True(t, ok)

	rec := vmRecForSavings()
	recs := []Recommendation{*rec}
	ApplyVMSavings(recs, &costdata.ClusterCostData{ConfiguredRates: map[string]costdata.RatePair{}}, true, 730)
	assert.Nil(t, recs[0].EstimatedSavingsCents)
}
