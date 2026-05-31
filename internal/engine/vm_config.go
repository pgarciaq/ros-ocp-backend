package engine

import (
	"os"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// VMRecConfig holds all configurable thresholds for VM recommendations.
type VMRecConfig struct {
	// CPU percentiles
	CPUPercentileCost float64 // default 0.95
	CPUPercentilePerf float64 // default 0.99

	// Margins
	CPUMarginMin float64 // default 0.15 (15%)
	CPUMarginMax float64 // default 0.50 (50%)
	MemMarginMin float64 // default 0.20 (20%)

	// Downsize hysteresis
	DownsizeHysteresisRatio float64 // default 0.60
	MinVCPUChange           int32   // default 2
	MinGiBChange            int32   // default 2

	// Idle thresholds (Linux)
	IdleCPUMC     int64 // default 50
	IdleMemoryMiB int64 // default 512

	// Idle thresholds (Windows)
	IdleCPUMCWindows     int64 // default 200
	IdleMemoryMiBWindows int64 // default 3072

	// Memory floors
	LinuxMemoryFloorGiB   int32 // default 1 (0.5 GiB rounds to 1)
	WindowsMemoryFloorGiB int32 // default 2

	// Windows kernel memory reserve subtracted before sizing (GiB)
	WindowsKernelReserveGiB float64 // default 1.5

	// Performance-engine downsize: require N consecutive days below threshold
	DownsizeStabilityDays int // default 3

	// Crash loop: sum of restart_count across term window
	CrashLoopRestartThreshold int32 // default 3

	// Disk
	DiskProjectionWindowDays int32   // default 30
	DiskHeadroomPct          float64 // default 0.25
	DiskRoundStepGiB         int32   // default 10
	DiskMinGrowthMiBPerDay   int64   // default 100 — hypervisor-only trending threshold

	// I/O
	HighIOPSThreshold int64 // default 3000

	// Instance type
	EnableInstanceTypeMatching bool // default true

	// Abandoned: minimum consecutive days of zero CPU and memory max usage in the term window
	AbandonedMinDays int32 // default 3 (≈72h at daily digest granularity)
}

// DefaultVMRecConfig returns the compiled defaults for VM recommendations.
func DefaultVMRecConfig() VMRecConfig {
	return VMRecConfig{
		CPUPercentileCost:          0.95,
		CPUPercentilePerf:          0.99,
		CPUMarginMin:               0.15,
		CPUMarginMax:               0.50,
		MemMarginMin:               0.20,
		DownsizeHysteresisRatio:    0.60,
		MinVCPUChange:              2,
		MinGiBChange:               2,
		IdleCPUMC:                  50,
		IdleMemoryMiB:              512,
		IdleCPUMCWindows:           200,
		IdleMemoryMiBWindows:       3072,
		LinuxMemoryFloorGiB:        1,
		WindowsMemoryFloorGiB:      2,
		WindowsKernelReserveGiB:    1.5,
		DownsizeStabilityDays:      3,
		CrashLoopRestartThreshold:  3,
		DiskProjectionWindowDays:   30,
		DiskHeadroomPct:            0.25,
		DiskRoundStepGiB:           10,
		DiskMinGrowthMiBPerDay:     100,
		HighIOPSThreshold:          3000,
		EnableInstanceTypeMatching: true,
		AbandonedMinDays:           3,
	}
}

var defaultVMRecConfig = DefaultVMRecConfig()

// InitVMRecDefaults copies VM recommendation thresholds from the central config.
// Call once after config load (e.g. alongside InitGPUEngine).
func InitVMRecDefaults(cfg *config.Config) {
	if cfg == nil {
		return
	}
	defaultVMRecConfig = applyVMEnvLocks(DefaultVMRecConfig(), cfg)
}

// VMRecConfigResolved returns the process-wide VM recommendation config.
func VMRecConfigResolved() VMRecConfig {
	return defaultVMRecConfig
}

func applyVMEnvLocks(base VMRecConfig, cfg *config.Config) VMRecConfig {
	if _, ok := os.LookupEnv("ROS_VM_CPU_PERCENTILE_COST"); ok {
		base.CPUPercentileCost = cfg.VMCPUPercentileCost
	}
	if _, ok := os.LookupEnv("ROS_VM_CPU_PERCENTILE_PERF"); ok {
		base.CPUPercentilePerf = cfg.VMCPUPercentilePerf
	}
	if _, ok := os.LookupEnv("ROS_VM_CPU_MARGIN_MIN"); ok {
		base.CPUMarginMin = cfg.VMCPUMarginMin
	}
	if _, ok := os.LookupEnv("ROS_VM_CPU_MARGIN_MAX"); ok {
		base.CPUMarginMax = cfg.VMCPUMarginMax
	}
	if _, ok := os.LookupEnv("ROS_VM_MEM_MARGIN_MIN"); ok {
		base.MemMarginMin = cfg.VMMemMarginMin
	}
	if _, ok := os.LookupEnv("ROS_VM_DOWNSIZE_HYSTERESIS_RATIO"); ok {
		base.DownsizeHysteresisRatio = cfg.VMDownsizeHysteresisRatio
	}
	if _, ok := os.LookupEnv("ROS_VM_MIN_VCPU_CHANGE"); ok {
		base.MinVCPUChange = cfg.VMMinVCPUChange
	}
	if _, ok := os.LookupEnv("ROS_VM_MIN_GIB_CHANGE"); ok {
		base.MinGiBChange = cfg.VMMinGiBChange
	}
	if _, ok := os.LookupEnv("ROS_VM_IDLE_CPU_MC"); ok {
		base.IdleCPUMC = cfg.VMIdleCPUMC
	}
	if _, ok := os.LookupEnv("ROS_VM_IDLE_MEMORY_MIB"); ok {
		base.IdleMemoryMiB = cfg.VMIdleMemoryMiB
	}
	if _, ok := os.LookupEnv("ROS_VM_IDLE_CPU_MC_WINDOWS"); ok {
		base.IdleCPUMCWindows = cfg.VMIdleCPUMCWindows
	}
	if _, ok := os.LookupEnv("ROS_VM_IDLE_MEMORY_MIB_WINDOWS"); ok {
		base.IdleMemoryMiBWindows = cfg.VMIdleMemoryMiBWindows
	}
	if _, ok := os.LookupEnv("ROS_VM_LINUX_MEMORY_FLOOR_GIB"); ok {
		base.LinuxMemoryFloorGiB = cfg.VMLinuxMemoryFloorGiB
	}
	if _, ok := os.LookupEnv("ROS_VM_WINDOWS_MEMORY_FLOOR_GIB"); ok {
		base.WindowsMemoryFloorGiB = cfg.VMWindowsMemoryFloorGiB
	}
	if _, ok := os.LookupEnv("ROS_VM_DISK_PROJECTION_DAYS"); ok {
		base.DiskProjectionWindowDays = cfg.VMDiskProjectionDays
	}
	if _, ok := os.LookupEnv("ROS_VM_DISK_HEADROOM_PCT"); ok {
		base.DiskHeadroomPct = cfg.VMDiskHeadroomPct
	}
	if _, ok := os.LookupEnv("ROS_VM_DISK_ROUND_STEP_GIB"); ok {
		base.DiskRoundStepGiB = cfg.VMDiskRoundStepGiB
	}
	if _, ok := os.LookupEnv("ROS_VM_DISK_MIN_GROWTH_MIB_PER_DAY"); ok {
		base.DiskMinGrowthMiBPerDay = cfg.VMDiskMinGrowthMiBPerDay
	}
	if _, ok := os.LookupEnv("ROS_VM_HIGH_IOPS_THRESHOLD"); ok {
		base.HighIOPSThreshold = cfg.VMHighIOPSThreshold
	}
	if _, ok := os.LookupEnv("ROS_VM_ENABLE_INSTANCE_TYPE_MATCHING"); ok {
		base.EnableInstanceTypeMatching = cfg.VMEnableInstanceTypeMatching
	}
	if _, ok := os.LookupEnv("ROS_VM_ABANDONED_MIN_DAYS"); ok {
		base.AbandonedMinDays = cfg.VMAbandonedMinDays
	}
	if _, ok := os.LookupEnv("ROS_VM_WINDOWS_KERNEL_RESERVE_GIB"); ok {
		base.WindowsKernelReserveGiB = cfg.VMWindowsKernelReserveGiB
	}
	if _, ok := os.LookupEnv("ROS_VM_DOWNSIZE_STABILITY_DAYS"); ok {
		base.DownsizeStabilityDays = cfg.VMDownsizeStabilityDays
	}
	if _, ok := os.LookupEnv("ROS_VM_CRASH_LOOP_RESTART_THRESHOLD"); ok {
		base.CrashLoopRestartThreshold = cfg.VMCrashLoopRestartThreshold
	}
	return base
}
