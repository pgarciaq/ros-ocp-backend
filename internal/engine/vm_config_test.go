package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestVMDefaultVMRecConfig(t *testing.T) {
	cfg := DefaultVMRecConfig()
	assert.InDelta(t, 0.95, cfg.CPUPercentileCost, 1e-9)
	assert.InDelta(t, 0.99, cfg.CPUPercentilePerf, 1e-9)
	assert.InDelta(t, 0.15, cfg.CPUMarginMin, 1e-9)
	assert.InDelta(t, 0.50, cfg.CPUMarginMax, 1e-9)
	assert.InDelta(t, 0.20, cfg.MemMarginMin, 1e-9)
	assert.InDelta(t, 0.60, cfg.DownsizeHysteresisRatio, 1e-9)
	assert.Equal(t, int32(2), cfg.MinVCPUChange)
	assert.Equal(t, int32(2), cfg.MinGiBChange)
	assert.Equal(t, int64(50), cfg.IdleCPUMC)
	assert.Equal(t, int64(512), cfg.IdleMemoryMiB)
	assert.Equal(t, int64(200), cfg.IdleCPUMCWindows)
	assert.Equal(t, int64(3072), cfg.IdleMemoryMiBWindows)
	assert.Equal(t, int32(1), cfg.LinuxMemoryFloorGiB)
	assert.Equal(t, int32(2), cfg.WindowsMemoryFloorGiB)
	assert.Equal(t, int32(30), cfg.DiskProjectionWindowDays)
	assert.InDelta(t, 0.25, cfg.DiskHeadroomPct, 1e-9)
	assert.Equal(t, int32(10), cfg.DiskRoundStepGiB)
	assert.Equal(t, int64(100), cfg.DiskMinGrowthMiBPerDay)
	assert.Equal(t, int64(3000), cfg.HighIOPSThreshold)
	assert.True(t, cfg.EnableInstanceTypeMatching)
	assert.Equal(t, int32(3), cfg.AbandonedMinDays)
	assert.InDelta(t, 1.5, cfg.WindowsKernelReserveGiB, 1e-9)
	assert.Equal(t, 3, cfg.DownsizeStabilityDays)
	assert.Equal(t, int32(3), cfg.CrashLoopRestartThreshold)
}

func TestVMInitVMRecDefaults_EnvOverrides(t *testing.T) {
	saved := defaultVMRecConfig
	t.Cleanup(func() { defaultVMRecConfig = saved })

	t.Setenv("ROS_VM_IDLE_CPU_MC", "99")
	t.Setenv("ROS_VM_HIGH_IOPS_THRESHOLD", "4500")
	t.Setenv("ROS_VM_LINUX_MEMORY_FLOOR_GIB", "4")
	t.Setenv("ROS_VM_WINDOWS_MEMORY_FLOOR_GIB", "8")
	config.ResetForTest()
	InitVMRecDefaults(config.GetConfig())

	got := VMRecConfigResolved()
	assert.Equal(t, int64(99), got.IdleCPUMC)
	assert.Equal(t, int64(4500), got.HighIOPSThreshold)
	assert.Equal(t, int32(4), got.LinuxMemoryFloorGiB)
	assert.Equal(t, int32(8), got.WindowsMemoryFloorGiB)
	assert.Equal(t, int64(200), got.IdleCPUMCWindows, "windows idle CPU unchanged without env")
}
