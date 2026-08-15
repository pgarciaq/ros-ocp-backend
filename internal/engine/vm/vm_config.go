package vm

import (
	"os"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

var DefaultVMRecConfigVar = DefaultVMRecConfig()

// InitVMRecDefaults copies VM recommendation thresholds from the central config.
// Call once after config load (e.g. alongside InitGPUEngine).
func InitVMRecDefaults(cfg *config.Config) {
	if cfg == nil {
		return
	}
	DefaultVMRecConfigVar = applyVMEnvLocks(DefaultVMRecConfig(), cfg)
}

// VMRecConfigResolved returns the process-wide VM recommendation config.
func VMRecConfigResolved() VMRecConfig {
	return DefaultVMRecConfigVar
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
	if _, ok := os.LookupEnv("ROS_VM_CPU_ADAPTIVE_MARGIN_ENABLED"); ok {
		base.CPUAdaptiveMarginEnabled = cfg.VMCPUAdaptiveMarginEnabled
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
	if _, ok := os.LookupEnv("ROS_VM_IO_SEQUENTIAL_THRESHOLD_BYTES"); ok {
		base.IOSequentialThresholdBytes = cfg.VMIOSequentialThresholdBytes
	}
	if _, ok := os.LookupEnv("ROS_VM_IO_RANDOM_THRESHOLD_BYTES"); ok {
		base.IORandomThresholdBytes = cfg.VMIORandomThresholdBytes
	}
	if _, ok := os.LookupEnv("ROS_VM_IO_MIN_IOPS_CLASSIFICATION"); ok {
		base.IOMinIOPSForClassification = cfg.VMIOMinIOPSClassification
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
	if _, ok := os.LookupEnv("ROS_VM_GPU_IDLE_THRESHOLD"); ok {
		base.GPUIdleThreshold = cfg.VMGPUIdleThreshold
	}
	if _, ok := os.LookupEnv("ROS_VM_GPU_UNDERUTIL_THRESHOLD"); ok {
		base.GPUUnderutilThreshold = cfg.VMGPUUnderutilThreshold
	}
	if _, ok := os.LookupEnv("ROS_VM_GPU_COMPUTE_SATURATION_THRESHOLD"); ok {
		base.GPUComputeSaturationThreshold = cfg.VMGPUComputeSaturationThreshold
	}
	if _, ok := os.LookupEnv("ROS_VM_GPU_FB_SATURATION_MIB"); ok {
		base.GPUFBSaturationMiB = cfg.VMGPUFBSaturationMiB
	}
	if _, ok := os.LookupEnv("ROS_VM_GPU_TIMESLICE_MIN_REPLICAS"); ok {
		base.GPUTimeSliceMinReplicas = cfg.VMGPUTimeSliceMinReplicas
	}
	if _, ok := os.LookupEnv("ROS_VM_GPU_TIMESLICE_MAX_REPLICAS"); ok {
		base.GPUTimeSliceMaxReplicas = cfg.VMGPUTimeSliceMaxReplicas
	}
	if _, ok := os.LookupEnv("ROS_VM_GPU_TIMESLICE_FB_SAFETY_BP"); ok {
		base.GPUTimeSliceFBSafetyThresholdBP = cfg.VMGPUTimeSliceFBSafetyBP
	}
	if _, ok := os.LookupEnv("ROS_VM_GPU_TIMESLICE_DRAM_PENALTY_BP"); ok {
		base.GPUTimeSliceDRAMPenaltyThresholdBP = cfg.VMGPUTimeSliceDRAMPenaltyBP
	}
	if _, ok := os.LookupEnv("ROS_VM_NETWORK_THROUGHPUT_THRESHOLD_BPS"); ok {
		base.NetworkThroughputThresholdBPS = cfg.VMNetworkThroughputThresholdBPS
	}
	if _, ok := os.LookupEnv("ROS_VM_NETWORK_PPS_THRESHOLD"); ok {
		base.NetworkPPSThreshold = cfg.VMNetworkPPSThreshold
	}
	if _, ok := os.LookupEnv("ROS_VM_NETWORK_DROP_RATIO_BP"); ok {
		base.NetworkDropRatioBP = cfg.VMNetworkDropRatioBP
	}
	if _, ok := os.LookupEnv("ROS_VM_NETWORK_SUSTAINED_DAYS"); ok {
		base.NetworkSustainedDays = cfg.VMNetworkSustainedDays
	}
	if _, ok := os.LookupEnv("ROS_VM_ENABLE_NETWORK_SERIES"); ok {
		base.EnableNetworkSeries = cfg.VMEnableNetworkSeries
	}
	if _, ok := os.LookupEnv("ROS_VM_NETWORK_QOS_ENABLED"); ok {
		base.NetworkQoSEnabled = cfg.VMNetworkQoSEnabled
	}
	if _, ok := os.LookupEnv("ROS_VM_NETWORK_QOS_SRIOV_DROP_THRESHOLD"); ok {
		base.NetworkQoSSRIOVDropThreshold = cfg.VMNetworkQoSSRIOVDropThreshold
	}
	if _, ok := os.LookupEnv("ROS_VM_NETWORK_QOS_SRIOV_THROUGHPUT_BPS"); ok {
		base.NetworkQoSSRIOVThroughputBPS = cfg.VMNetworkQoSSRIOVThroughputBPS
	}
	if _, ok := os.LookupEnv("ROS_VM_NETWORK_QOS_DPDK_PPS_THRESHOLD"); ok {
		base.NetworkQoSDPDKPPSThreshold = cfg.VMNetworkQoSDPDKPPSThreshold
	}
	if _, ok := os.LookupEnv("ROS_VM_STORAGE_TIERING_ENABLED"); ok {
		base.StorageTieringEnabled = cfg.VMStorageTieringEnabled
	}
	if _, ok := os.LookupEnv("ROS_VM_STORAGE_TIERING_MIN_DAYS"); ok {
		base.StorageTieringMinDays = cfg.VMStorageTieringMinDays
	}
	if _, ok := os.LookupEnv("ROS_VM_STORAGE_TIERING_COLD_MIN_DAYS"); ok {
		base.StorageTieringColdMinDays = cfg.VMStorageTieringColdMinDays
	}
	if _, ok := os.LookupEnv("ROS_VM_STORAGE_TIERING_IOPS_MIN_DAYS"); ok {
		base.StorageTieringIOPSMinDays = cfg.VMStorageTieringIOPSMinDays
	}
	if _, ok := os.LookupEnv("ROS_VM_STORAGE_TIERING_THROUGHPUT_MIN_DAYS"); ok {
		base.StorageTieringThroughputMinDays = cfg.VMStorageTieringThroughputMinDays
	}
	if _, ok := os.LookupEnv("ROS_VM_STORAGE_TIERING_HIGH_IOPS_THRESHOLD"); ok {
		base.StorageTieringHighIOPSThreshold = cfg.VMStorageTieringHighIOPSThreshold
	}
	if _, ok := os.LookupEnv("ROS_VM_STORAGE_TIERING_HIGH_THROUGHPUT_BPS"); ok {
		base.StorageTieringHighThroughputBPS = cfg.VMStorageTieringHighThroughputBPS
	}
	if _, ok := os.LookupEnv("ROS_VM_ENABLE_PLACEMENT_CHECKS"); ok {
		base.EnablePlacementChecks = cfg.VMEnablePlacementChecks
	}
	if _, ok := os.LookupEnv("ROS_VM_PLACEMENT_SKEW_RATIO"); ok {
		base.PlacementSkewRatio = cfg.VMPlacementSkewRatio
	}
	if _, ok := os.LookupEnv("ROS_VM_ENABLE_SHARED_PVC_CORRELATION"); ok {
		base.EnableSharedPVCCorrelation = cfg.VMEnableSharedPVCCorrelation
	}
	if _, ok := os.LookupEnv("ROS_VM_NUMA_NODE_MEMORY_GIB"); ok {
		base.NUMANodeMemoryGiB = cfg.VMNUMANodeMemoryGiB
	}
	if _, ok := os.LookupEnv("ROS_VM_NUMA_ASSUMED_SOCKETS"); ok {
		base.NUMAAssumedSockets = cfg.VMNUMAAssumedSockets
	}
	if _, ok := os.LookupEnv("ROS_VM_ENABLE_POWER_SCHEDULE"); ok {
		base.EnablePowerSchedule = cfg.VMEnablePowerSchedule
	}
	if _, ok := os.LookupEnv("ROS_VM_POWER_OFF_MIN_IDLE_DAYS"); ok {
		base.PowerOffMinIdleDays = cfg.VMPowerOffMinIdleDays
	}
	if _, ok := os.LookupEnv("ROS_VM_POWER_OFF_IDLE_RATIO_THRESHOLD"); ok {
		base.PowerOffIdleRatioThreshold = cfg.VMPowerOffIdleRatioThreshold
	}
	return base
}
