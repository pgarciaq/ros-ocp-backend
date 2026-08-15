package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
)

func TestMIGOptimal_SmallWorkload_1g5gb(t *testing.T) {
	got := OptimalMIGProfile("NVIDIA A100-SXM4-40GB", "7g.40gb", 2048, 500)
	assert.Equal(t, "1g.5gb", got)
}

func TestMIGOptimal_MediumWorkload_3g20gb(t *testing.T) {
	// 14 GiB peak FB * 1.2 headroom fits 3g.20gb (20480 MiB), not 7g.40gb.
	got := OptimalMIGProfile("NVIDIA A100-SXM4-40GB", "1g.5gb", 14*1024, 2000)
	assert.Equal(t, "3g.20gb", got)
}

func TestMIGOptimal_LargeWorkload_7g40gb(t *testing.T) {
	// 28 GiB peak FB * 1.2 headroom fits largest 40GB A100 MIG partition.
	got := OptimalMIGProfile("NVIDIA A100-SXM4-40GB", "1g.5gb", 28*1024, 5000)
	assert.Equal(t, "7g.40gb", got)
}

func TestMIGOptimal_NeedsUpsize(t *testing.T) {
	got := OptimalMIGProfile("NVIDIA A100-SXM4-40GB", "1g.5gb", 38*1024, 8000)
	assert.Equal(t, "full_gpu", got)
}

func TestMIGOptimal_A30Profiles(t *testing.T) {
	got := OptimalMIGProfile("NVIDIA A30", "", 8*1024, 1000)
	require.NotEmpty(t, got)
	assert.Contains(t, []string{"1g.6gb", "2g.12gb", "4g.24gb", "full_gpu"}, got)
}

func TestMIGOptimal_H100Profiles(t *testing.T) {
	spec := gpu.MatchGPUModel("NVIDIA H100 80GB HBM3")
	require.NotNil(t, spec)
	got := OptimalMIGProfile("NVIDIA H100 80GB HBM3", "", 12*1024, 1500)
	require.NotEmpty(t, got)
	if got != "full_gpu" {
		found := false
		for _, p := range spec.Profiles {
			if p.Name == got {
				found = true
				break
			}
		}
		assert.True(t, found, "profile %q should be in H100 catalog", got)
	}
}

func TestVGPU_MIGCapable_RecommendsProfile(t *testing.T) {
	dev := GPUDeviceDigest{
		UUID: "gpu-1", Model: "NVIDIA A100-SXM4-40GB", MaxSlices: 7,
		SMActiveAvgBP: 500, TensorAvgBP: 400, UtilAvgBP: 1500, FBUsedMaxMiB: 4096,
		MIGProfile: "7g.40gb",
	}
	cls := classifyGPUDevice(dev, DefaultVMRecConfig(), 7)
	assert.Equal(t, vmGPUActionUseMIGProfile, cls.action)
	assert.NotEmpty(t, cls.profile)
}

func TestVGPU_NotMIGCapable_RecommendsTimeSlicing(t *testing.T) {
	dev := GPUDeviceDigest{
		UUID: "gpu-1", Model: "NVIDIA T4",
		SMActiveAvgBP: 500, TensorAvgBP: 400, UtilAvgBP: 2000,
	}
	cls := classifyGPUDevice(dev, DefaultVMRecConfig(), 7)
	assert.Equal(t, vmGPUActionEnableTimeSlicing, cls.action)
	// SM 5% → ceil(1/0.05)=20, capped at max replicas (16) by production time-slicing logic.
	assert.Equal(t, int32(16), cls.timeSlices)
	assert.Equal(t, "high", cls.timeSliceConfidence)
}

func TestVGPU_HighUtil_NoSharing(t *testing.T) {
	dev := GPUDeviceDigest{
		UUID: "gpu-1", Model: "NVIDIA T4",
		SMActiveAvgBP: 7000, TensorAvgBP: 6000, UtilAvgBP: 8500,
	}
	cls := classifyGPUDevice(dev, DefaultVMRecConfig(), 7)
	assert.Equal(t, vmGPUActionNoChange, cls.action)
	assert.Equal(t, int32(0), cls.timeSlices)
}
