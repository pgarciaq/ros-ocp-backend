package vm

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func vmRecForSavings() *model.VMRecommendation {
	return &model.VMRecommendation{
		ClusterUUID:          uuid.New(),
		CurrentVCPU:          8,
		CurrentMemoryGiB:     32,
		RecommendedVCPU:      4,
		RecommendedMemoryGiB: 16,
	}
}

func vmCostData(cpuRate, memRate, gpuRate float64) *costdata.ClusterCostData {
	return vmCostDataWithVMRate(cpuRate, memRate, gpuRate, 0)
}

func vmCostDataWithVMRate(cpuRate, memRate, gpuRate, vmMonthlyRate float64) *costdata.ClusterCostData {
	rates := map[string]costdata.RatePair{
		"cpu_core_request_per_hour":  {Supplementary: cpuRate * 0.4},
		"cpu_core_usage_per_hour":    {Supplementary: cpuRate * 0.6},
		"memory_gb_request_per_hour": {Supplementary: memRate * 0.3},
		"memory_gb_usage_per_hour":   {Supplementary: memRate * 0.7},
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
	cd.ConfiguredRates["cpu_core_request_per_hour"] = costdata.RatePair{Supplementary: 0.2}
	cd.ConfiguredRates["cpu_core_usage_per_hour"] = costdata.RatePair{Supplementary: 0.5}
	assert.InDelta(t, 0.5, engine.EffectiveCPUCoreHourlyRate(cd), 1e-9)
}

func TestComputeVMSavings_Downsize(t *testing.T) {
	rec := vmRecForSavings()
	cd := vmCostData(1.0, 2.0, 0)

	got := ComputeVMSavings(rec, cd)
	require.NotNil(t, got)
	// effective cpu=0.6, mem=1.4 → (4)*0.6*730 + (16)*1.4*730 = 18104
	assert.InDelta(t, 18104.0, *got, 0.01)
}

func TestComputeVMSavings_Idle(t *testing.T) {
	rec := vmRecForSavings()
	rec.Category = model.VMCategoryIdle
	rec.RecommendedVCPU = 1
	rec.RecommendedMemoryGiB = 4
	cd := vmCostData(1.0, 2.0, 600)

	got := ComputeVMSavings(rec, cd)
	require.NotNil(t, got)
	// 8*0.6*730 + 32*1.4*730 = 3504 + 32704 = 36208
	assert.InDelta(t, 36208.0, *got, 0.01)
}

func TestComputeVMSavings_IdleIncludesVMCostPerMonth(t *testing.T) {
	rec := vmRecForSavings()
	rec.Category = model.VMCategoryIdle
	cd := vmCostDataWithVMRate(0, 0, 0, 250)

	got := ComputeVMSavings(rec, cd)
	require.NotNil(t, got)
	assert.InDelta(t, 250.0, *got, 0.01)
}

func TestComputeVMSavings_AbandonedWithGPU(t *testing.T) {
	rec := vmRecForSavings()
	rec.Category = model.VMCategoryAbandoned
	rec.GPUCount = 2
	cd := vmCostData(1.0, 2.0, 600)

	got := ComputeVMSavings(rec, cd)
	require.NotNil(t, got)
	// 36208 + 2*600 = 37408
	assert.InDelta(t, 37408.0, *got, 0.01)
}

func TestComputeVMSavings_PowerOffCandidate(t *testing.T) {
	rec := vmRecForSavings()
	rec.Category = model.VMCategoryPowerOffCandidate
	bp := int32(7000)
	rec.PowerOffIdleRatio = &bp
	cd := vmCostData(1.0, 2.0, 0)

	got := ComputeVMSavings(rec, cd)
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
	cd := vmCostData(1.0, 2.0, 600)

	got := ComputeVMSavings(rec, cd)
	require.NotNil(t, got)
	assert.InDelta(t, 600.0, *got, 0.01)
}

func TestComputeVMSavings_NilCostData(t *testing.T) {
	rec := vmRecForSavings()
	assert.Nil(t, ComputeVMSavings(rec, nil))
}

func TestComputeVMSavings_NoRates(t *testing.T) {
	rec := vmRecForSavings()
	cd := &costdata.ClusterCostData{ConfiguredRates: map[string]costdata.RatePair{}}
	assert.Nil(t, ComputeVMSavings(rec, cd))
}

func TestApplyVMSavings_Disabled(t *testing.T) {
	rec := vmRecForSavings()
	recs := []model.VMRecommendation{*rec}
	cd := vmCostData(1.0, 2.0, 0)

	ApplyVMSavings(recs, cd, false)
	assert.Nil(t, recs[0].EstimatedSavingsCents)
	assert.Nil(t, recs[0].SavingsCurrency)
}

func TestApplyVMSavings_NilProviderData(t *testing.T) {
	rec := vmRecForSavings()
	recs := []model.VMRecommendation{*rec}

	ApplyVMSavings(recs, nil, true)
	assert.Nil(t, recs[0].EstimatedSavingsCents)
}

func TestApplyVMSavings_CurrencyFromCostData(t *testing.T) {
	rec := vmRecForSavings()
	recs := []model.VMRecommendation{*rec}
	cd := vmCostData(1.0, 2.0, 0)

	ApplyVMSavings(recs, cd, true)
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
	recs := []model.VMRecommendation{*rec}
	ApplyVMSavings(recs, &costdata.ClusterCostData{ConfiguredRates: map[string]costdata.RatePair{}}, true)
	assert.Nil(t, recs[0].EstimatedSavingsCents)
}
