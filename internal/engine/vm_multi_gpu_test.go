package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestMultiGPU_AllActive(t *testing.T) {
	digests := []model.DailyVMDigest{{
		HasGPU: true, GPUCount: 2,
		Devices: []model.GPUDeviceDigest{
			{UUID: "g1", Model: "NVIDIA A100", SMActiveAvgBP: 5000, TensorAvgBP: 4000, UtilAvgBP: 6000},
			{UUID: "g2", Model: "NVIDIA A100", SMActiveAvgBP: 5500, TensorAvgBP: 4500, UtilAvgBP: 6500},
		},
	}}
	analysis := analyzeVMGPU(digests, DefaultVMRecConfig())
	assert.Equal(t, int32(2), analysis.GPUCount)
	assert.NotContains(t, analysis.NotificationCodes, NotifVMGPUMixedIdle)
}

func TestMultiGPU_SomeIdle_ReduceCount(t *testing.T) {
	digests := []model.DailyVMDigest{{
		HasGPU: true, GPUCount: 2,
		Devices: []model.GPUDeviceDigest{
			{UUID: "g1", Model: "NVIDIA A100", SMActiveAvgBP: 100, TensorAvgBP: 100, UtilAvgBP: 200},
			{UUID: "g2", Model: "NVIDIA A100", SMActiveAvgBP: 6000, TensorAvgBP: 5000, UtilAvgBP: 7000},
		},
	}}
	analysis := analyzeVMGPU(digests, DefaultVMRecConfig())
	assert.Contains(t, analysis.NotificationCodes, NotifVMGPUMixedIdle)
	assert.Equal(t, int32(1), analysis.ActiveGPUCount)
}

func TestMultiGPU_AllIdle(t *testing.T) {
	digests := []model.DailyVMDigest{{
		HasGPU: true, GPUCount: 2,
		Devices: []model.GPUDeviceDigest{
			{UUID: "g1", Model: "NVIDIA A100", SMActiveAvgBP: 100, TensorAvgBP: 100},
			{UUID: "g2", Model: "NVIDIA A100", SMActiveAvgBP: 200, TensorAvgBP: 150},
		},
	}}
	analysis := analyzeVMGPU(digests, DefaultVMRecConfig())
	assert.Equal(t, "idle", analysis.Classification)
	assert.Equal(t, vmGPUActionRemoveGPU, analysis.Action)
}

func TestMultiGPU_MixedModels(t *testing.T) {
	digests := []model.DailyVMDigest{{
		HasGPU: true, GPUCount: 2,
		Devices: []model.GPUDeviceDigest{
			{UUID: "g1", Model: "NVIDIA T4", SMActiveAvgBP: 500, TensorAvgBP: 400, UtilAvgBP: 2000},
			{UUID: "g2", Model: "NVIDIA A100-SXM4-40GB", MaxSlices: 7, MIGProfile: "7g.40gb",
				SMActiveAvgBP: 500, TensorAvgBP: 400, UtilAvgBP: 1500, FBUsedMaxMiB: 4096},
		},
	}}
	analysis := analyzeVMGPU(digests, DefaultVMRecConfig())
	require.Len(t, analysis.GPUDevices, 2)
	assert.Equal(t, vmGPUActionUseMIGProfile, analysis.Action)
}

func TestRecommendVM_AdaptiveMarginCostEngine(t *testing.T) {
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	digests := vmDigestDays(base, 5, func(d *model.DailyVMDigest) {
		d.CPUUsageP95MC = 10000
		d.MemUsageP95KiB = 8 * 1024 * 1024
	})
	cfg := DefaultVMRecConfig()
	cfg.CPUAdaptiveMarginEnabled = true
	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.GreaterOrEqual(t, rec.RecommendedVCPU, int32(1))
}
