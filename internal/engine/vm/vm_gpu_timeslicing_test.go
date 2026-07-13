package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestRecommendVMTimeSlicing_LowUtilLowFB_HighConfidence(t *testing.T) {
	cfg := DefaultVMRecConfig()
	dev := model.GPUDeviceDigest{
		UUID:          "gpu-1",
		Model:         "T4",
		SMActiveAvgBP: 500, // 5%
		DRAMAvgBP:     300,
		FBUsedMaxMiB:  1024, // ~6% of 16GiB
	}
	rec := RecommendVMTimeSlicingForDevice(dev, 7, cfg)
	assert.True(t, rec.EnableTimeSlicing)
	assert.GreaterOrEqual(t, rec.RecommendedSliceCount, cfg.GPUTimeSliceMinReplicas)
	assert.Equal(t, "high", rec.Confidence)
	assert.NotEmpty(t, rec.Rationale)
}

func TestRecommendVMTimeSlicing_HighFB_NoRecommend(t *testing.T) {
	cfg := DefaultVMRecConfig()
	dev := model.GPUDeviceDigest{
		UUID:          "gpu-1",
		Model:         "T4",
		SMActiveAvgBP: 500,
		DRAMAvgBP:     300,
		FBUsedMaxMiB:  14000, // ~85% of 16GiB
	}
	rec := RecommendVMTimeSlicingForDevice(dev, 7, cfg)
	assert.False(t, rec.EnableTimeSlicing)
	assert.True(t, rec.FBUnsafe)
	assert.Contains(t, rec.Rationale, "unsafe")
}

func TestRecommendVMTimeSlicing_HighDRAM_ReducedMaxSlices(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.GPUTimeSliceMaxReplicas = 16
	cfg.GPUTimeSliceDRAMPenaltyThresholdBP = 5000
	dev := model.GPUDeviceDigest{
		UUID:          "gpu-1",
		Model:         "T4",
		SMActiveAvgBP: 200,
		DRAMAvgBP:     6000, // 60%
		FBUsedMaxMiB:  2048,
	}
	rec := RecommendVMTimeSlicingForDevice(dev, 7, cfg)
	assert.LessOrEqual(t, rec.MaxSlices, int32(8))
}

func TestRecommendVMTimeSlicing_MIGCapable_PreferMIG(t *testing.T) {
	cfg := DefaultVMRecConfig()
	dev := model.GPUDeviceDigest{
		UUID:          "gpu-1",
		Model:         "NVIDIA A100-SXM4-80GB",
		SMActiveAvgBP: 1000,
		MaxSlices:     7,
	}
	rec := RecommendVMTimeSlicingForDevice(dev, 7, cfg)
	assert.False(t, rec.EnableTimeSlicing)
	assert.True(t, rec.PreferMIG)
}

func TestRecommendVMTimeSlicing_MultiDeviceAggregate(t *testing.T) {
	cfg := DefaultVMRecConfig()
	devices := []model.GPUDeviceDigest{
		{UUID: "a", Model: "T4", SMActiveAvgBP: 400, DRAMAvgBP: 200, FBUsedMaxMiB: 1024},
		{UUID: "b", Model: "T4", SMActiveAvgBP: 600, DRAMAvgBP: 300, FBUsedMaxMiB: 1536},
	}
	rec := RecommendVMTimeSlicing(devices, 7, cfg)
	assert.True(t, rec.EnableTimeSlicing)
	assert.GreaterOrEqual(t, rec.RecommendedSliceCount, cfg.GPUTimeSliceMinReplicas)
}

func TestRecommendVMTimeSlicing_LegacyNoDRAM(t *testing.T) {
	cfg := DefaultVMRecConfig()
	dev := model.GPUDeviceDigest{
		UUID:          "gpu-1",
		Model:         "T4",
		UtilAvgBP:     800,
		SMActiveAvgBP: 0,
		DRAMAvgBP:     0,
		FBUsedMaxMiB:  2048,
	}
	rec := RecommendVMTimeSlicingForDevice(dev, 3, cfg)
	assert.True(t, rec.EnableTimeSlicing || rec.Rationale != "")
}

func TestRecommendVGPUProfile_A100(t *testing.T) {
	profile := engine.RecommendVGPUProfile("NVIDIA A100-SXM4-80GB", 8192)
	assert.Equal(t, "grid_a100d-10c", profile)
}

func TestRecommendVGPUProfile_T4Smallest(t *testing.T) {
	profile := engine.RecommendVGPUProfile("Tesla T4", 512)
	require.NotEmpty(t, profile)
	assert.Equal(t, "grid_t4-4c", profile)
}

func TestVMGPU_T4_TimeSlicingIntegration(t *testing.T) {
	digests := vmGPUDigests(func(d *model.DailyVMDigest) {
		d.GPUModel = "Tesla T4"
		d.GPUSMActiveAvgBP = 1200
		d.GPUTensorAvgBP = 600
		d.GPUUtilAvgBP = 1500
		d.GPUFBUsedMaxMiB = 2048
	})
	cfg := DefaultVMRecConfig()
	analysis := analyzeVMGPU(digests, cfg)
	assert.Equal(t, "underutilized", analysis.Classification)
	assert.Equal(t, vmGPUActionEnableTimeSlicing, analysis.Action)
	assert.GreaterOrEqual(t, analysis.RecommendedTimeSliceCount, cfg.GPUTimeSliceMinReplicas)
	assert.NotEmpty(t, analysis.GPUTimeSliceConfidence)
}

func TestVMGPU_HighFB_TimeSliceUnsafeNotification(t *testing.T) {
	digests := vmGPUDigests(func(d *model.DailyVMDigest) {
		d.GPUModel = "Tesla T4"
		d.GPUSMActiveAvgBP = 1200
		d.GPUTensorAvgBP = 600
		d.GPUUtilAvgBP = 1500
		d.GPUFBUsedMaxMiB = 15000
	})
	analysis := analyzeVMGPU(digests, DefaultVMRecConfig())
	assert.Contains(t, analysis.NotificationCodes, NotifVMGPUTimeSliceUnsafeFB)
}
