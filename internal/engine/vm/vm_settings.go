package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

const vmRecommendationType = "vm"

// VMThresholdSettingsAPI is the thresholds block in VM settings responses.
type VMThresholdSettingsAPI struct {
	CPUPercentileCost       float64 `json:"cpu_percentile_cost"`
	CPUPercentilePerf       float64 `json:"cpu_percentile_perf"`
	CPUMarginMin            float64 `json:"cpu_margin_min"`
	CPUMarginMax            float64 `json:"cpu_margin_max"`
	MemMarginMin            float64 `json:"mem_margin_min"`
	DownsizeHysteresisRatio float64 `json:"downsize_hysteresis_ratio"`
	MinVCPUChange           int32   `json:"min_vcpu_change"`
	MinGiBChange            int32   `json:"min_gib_change"`
	IdleCPUMC               int64   `json:"idle_cpu_mc"`
	IdleMemoryMiB           int64   `json:"idle_memory_mib"`
	IdleCPUMCWindows        int64   `json:"idle_cpu_mc_windows"`
	IdleMemoryMiBWindows    int64   `json:"idle_memory_mib_windows"`
	AbandonedMinDays        int32   `json:"abandoned_min_days"`
}

// VMDiskSettingsAPI is the disk block in VM settings responses.
type VMDiskSettingsAPI struct {
	ProjectionWindowDays int32   `json:"projection_window_days"`
	HeadroomPct          float64 `json:"headroom_pct"`
	RoundStepGiB         int32   `json:"round_step_gib"`
	MinGrowthMiBPerDay   int64   `json:"min_growth_mib_per_day"`
}

// VMIOSettingsAPI is the I/O block in VM settings responses.
type VMIOSettingsAPI struct {
	HighIOPSThreshold        int64 `json:"high_iops_threshold"`
	SequentialThresholdBytes int64 `json:"sequential_threshold_bytes"`
	RandomThresholdBytes     int64 `json:"random_threshold_bytes"`
	MinIOPSForClassification int64 `json:"min_iops_for_classification"`
}

// VMNetworkSettingsAPI holds n1 network-optimized classification thresholds.
type VMNetworkSettingsAPI struct {
	ThroughputThresholdBPS int64 `json:"throughput_threshold_bps"`
	PPSThreshold           int64 `json:"pps_threshold"`
	DropRatioBP            int32 `json:"drop_ratio_bp"`
	SustainedDays          int   `json:"sustained_days"`
	EnableNetworkSeries    bool  `json:"enable_network_series"`
}

// VMGPUSettingsAPI holds GPU classification thresholds and vGPU time-slicing tunables for VM recommendations.
type VMGPUSettingsAPI struct {
	IdleThresholdBP                 int32 `json:"idle_threshold_bp"`
	UnderutilThresholdBP            int32 `json:"underutil_threshold_bp"`
	FBSaturationMiB                 int32 `json:"fb_saturation_mib"`
	ComputeSaturationThresholdBP    int32 `json:"compute_saturation_threshold_bp"`
	TimeSliceMinReplicas            int32 `json:"gpu_timeslice_min_replicas"`
	TimeSliceMaxReplicas            int32 `json:"gpu_timeslice_max_replicas"`
	TimeSliceFBSafetyThresholdBP    int32 `json:"gpu_timeslice_fb_safety_threshold_bp"`
	TimeSliceDRAMPenaltyThresholdBP int32 `json:"gpu_timeslice_dram_penalty_threshold_bp"`
}

// VMMemoryFloorsSettingsAPI is the memory floor block in VM settings responses.
type VMMemoryFloorsSettingsAPI struct {
	LinuxGiB                int32   `json:"linux_gib"`
	WindowsGiB              int32   `json:"windows_gib"`
	WindowsKernelReserveGiB float64 `json:"windows_kernel_reserve_gib"`
}

// VMPowerScheduleSettingsAPI holds power-off scheduling detection thresholds.
type VMPowerScheduleSettingsAPI struct {
	Enabled            bool    `json:"enabled"`
	MinIdleDays        int32   `json:"min_idle_days"`
	IdleRatioThreshold float64 `json:"idle_ratio_threshold"`
}

// VMNetworkQoSSettingsAPI holds simplified network QoS hint thresholds (notifications 65–66).
type VMNetworkQoSSettingsAPI struct {
	Enabled            bool    `json:"enabled"`
	SRIOVDropThreshold float64 `json:"sriov_drop_threshold"`
	SRIOVThroughputBPS int64   `json:"sriov_throughput_bps"`
	DPDKPPSThreshold   int64   `json:"dpdk_pps_threshold"`
}

// VMStorageTieringSettingsAPI holds simplified storage tier hint thresholds (notifications 67–69).
type VMStorageTieringSettingsAPI struct {
	Enabled           bool  `json:"enabled"`
	MinDays           int   `json:"min_days"`
	ColdMinDays       int   `json:"cold_min_days"`
	IOPSMinDays       int   `json:"iops_min_days"`
	ThroughputMinDays int   `json:"throughput_min_days"`
	HighIOPSThreshold int64 `json:"high_iops_threshold"`
	HighThroughputBPS int64 `json:"high_throughput_bps"`
}

// VMPlacementSettingsAPI holds cluster placement, correlated workload, and NUMA checks.
type VMPlacementSettingsAPI struct {
	EnablePlacementChecks      bool    `json:"enable_placement_checks"`
	PlacementSkewRatio         int     `json:"placement_skew_ratio"`
	EnableSharedPVCCorrelation bool    `json:"enable_shared_pvc_correlation"`
	NUMANodeMemoryGiB          float64 `json:"numa_node_memory_gib"`
	NUMAAssumedSockets         int     `json:"numa_assumed_sockets"`
}

// VMStabilitySettingsAPI holds performance-engine downsize stability settings.
type VMStabilitySettingsAPI struct {
	DownsizeStabilityDays     int   `json:"downsize_stability_days"`
	CrashLoopRestartThreshold int32 `json:"crash_loop_restart_threshold"`
}

// VMSettingsResponse is the API GET/PUT response for VM recommendation settings.
type VMSettingsResponse struct {
	Enabled                  bool                        `json:"enabled"`
	CPUAdaptiveMarginEnabled bool                        `json:"cpu_adaptive_margin_enabled"`
	HistoryRetentionDays     int                         `json:"history_retention_days"`
	Thresholds               VMThresholdSettingsAPI      `json:"thresholds"`
	MemoryFloors             VMMemoryFloorsSettingsAPI   `json:"memory_floors"`
	Stability                VMStabilitySettingsAPI      `json:"stability"`
	Disk                     VMDiskSettingsAPI           `json:"disk"`
	IO                       VMIOSettingsAPI             `json:"io"`
	Network                  VMNetworkSettingsAPI        `json:"network"`
	GPU                      VMGPUSettingsAPI            `json:"gpu"`
	Placement                VMPlacementSettingsAPI      `json:"placement"`
	PowerSchedule            VMPowerScheduleSettingsAPI  `json:"power_schedule"`
	NetworkQoS               VMNetworkQoSSettingsAPI     `json:"network_qos"`
	StorageTiering           VMStorageTieringSettingsAPI `json:"storage_tiering"`
	InstanceTypeMatching     bool                        `json:"instance_type_matching"`
	LockedFields             []string                    `json:"locked_fields,omitempty"`
	SettingsLocked           bool                        `json:"settings_locked,omitempty"`
}

type vmSettingsStored struct {
	CPUAdaptiveMarginEnabled *bool                        `json:"cpu_adaptive_margin_enabled,omitempty"`
	Thresholds               *VMThresholdSettingsAPI      `json:"thresholds,omitempty"`
	MemoryFloors             *VMMemoryFloorsSettingsAPI   `json:"memory_floors,omitempty"`
	Disk                     *VMDiskSettingsAPI           `json:"disk,omitempty"`
	IO                       *VMIOSettingsAPI             `json:"io,omitempty"`
	Network                  *VMNetworkSettingsAPI        `json:"network,omitempty"`
	GPU                      *VMGPUSettingsAPI            `json:"gpu,omitempty"`
	Placement                *VMPlacementSettingsAPI      `json:"placement,omitempty"`
	PowerSchedule            *VMPowerScheduleSettingsAPI  `json:"power_schedule,omitempty"`
	NetworkQoS               *VMNetworkQoSSettingsAPI     `json:"network_qos,omitempty"`
	StorageTiering           *VMStorageTieringSettingsAPI `json:"storage_tiering,omitempty"`
	InstanceTypeMatching     *bool                        `json:"instance_type_matching,omitempty"`
	Stability                *VMStabilitySettingsAPI      `json:"stability,omitempty"`
}

func vmEnvLockMap() map[string]string {
	return map[string]string{
		"ROS_VM_CPU_PERCENTILE_COST":                 "thresholds.cpu_percentile_cost",
		"ROS_VM_CPU_PERCENTILE_PERF":                 "thresholds.cpu_percentile_perf",
		"ROS_VM_CPU_MARGIN_MIN":                      "thresholds.cpu_margin_min",
		"ROS_VM_CPU_MARGIN_MAX":                      "thresholds.cpu_margin_max",
		"ROS_VM_MEM_MARGIN_MIN":                      "thresholds.mem_margin_min",
		"ROS_VM_DOWNSIZE_HYSTERESIS_RATIO":           "thresholds.downsize_hysteresis_ratio",
		"ROS_VM_MIN_VCPU_CHANGE":                     "thresholds.min_vcpu_change",
		"ROS_VM_MIN_GIB_CHANGE":                      "thresholds.min_gib_change",
		"ROS_VM_IDLE_CPU_MC":                         "thresholds.idle_cpu_mc",
		"ROS_VM_IDLE_MEMORY_MIB":                     "thresholds.idle_memory_mib",
		"ROS_VM_IDLE_CPU_MC_WINDOWS":                 "thresholds.idle_cpu_mc_windows",
		"ROS_VM_IDLE_MEMORY_MIB_WINDOWS":             "thresholds.idle_memory_mib_windows",
		"ROS_VM_ABANDONED_MIN_DAYS":                  "thresholds.abandoned_min_days",
		"ROS_VM_LINUX_MEMORY_FLOOR_GIB":              "memory_floors.linux_gib",
		"ROS_VM_WINDOWS_MEMORY_FLOOR_GIB":            "memory_floors.windows_gib",
		"ROS_VM_DISK_PROJECTION_DAYS":                "disk.projection_window_days",
		"ROS_VM_DISK_HEADROOM_PCT":                   "disk.headroom_pct",
		"ROS_VM_DISK_ROUND_STEP_GIB":                 "disk.round_step_gib",
		"ROS_VM_DISK_MIN_GROWTH_MIB_PER_DAY":         "disk.min_growth_mib_per_day",
		"ROS_VM_HIGH_IOPS_THRESHOLD":                 "io.high_iops_threshold",
		"ROS_VM_IO_SEQUENTIAL_THRESHOLD_BYTES":       "io.sequential_threshold_bytes",
		"ROS_VM_IO_RANDOM_THRESHOLD_BYTES":           "io.random_threshold_bytes",
		"ROS_VM_IO_MIN_IOPS_CLASSIFICATION":          "io.min_iops_for_classification",
		"ROS_VM_ENABLE_INSTANCE_TYPE_MATCHING":       "instance_type_matching",
		"ROS_VM_CPU_ADAPTIVE_MARGIN_ENABLED":         "cpu_adaptive_margin_enabled",
		"ROS_VM_WINDOWS_KERNEL_RESERVE_GIB":          "memory_floors.windows_kernel_reserve_gib",
		"ROS_VM_DOWNSIZE_STABILITY_DAYS":             "stability.downsize_stability_days",
		"ROS_VM_CRASH_LOOP_RESTART_THRESHOLD":        "stability.crash_loop_restart_threshold",
		"ROS_VM_GPU_IDLE_THRESHOLD":                  "gpu.idle_threshold_bp",
		"ROS_VM_GPU_UNDERUTIL_THRESHOLD":             "gpu.underutil_threshold_bp",
		"ROS_VM_GPU_FB_SATURATION_MIB":               "gpu.fb_saturation_mib",
		"ROS_VM_GPU_COMPUTE_SATURATION_THRESHOLD":    "gpu.compute_saturation_threshold_bp",
		"ROS_VM_GPU_TIMESLICE_MIN_REPLICAS":          "gpu.gpu_timeslice_min_replicas",
		"ROS_VM_GPU_TIMESLICE_MAX_REPLICAS":          "gpu.gpu_timeslice_max_replicas",
		"ROS_VM_GPU_TIMESLICE_FB_SAFETY_BP":          "gpu.gpu_timeslice_fb_safety_threshold_bp",
		"ROS_VM_GPU_TIMESLICE_DRAM_PENALTY_BP":       "gpu.gpu_timeslice_dram_penalty_threshold_bp",
		"ROS_VM_NETWORK_THROUGHPUT_THRESHOLD_BPS":    "network.throughput_threshold_bps",
		"ROS_VM_NETWORK_PPS_THRESHOLD":               "network.pps_threshold",
		"ROS_VM_NETWORK_DROP_RATIO_BP":               "network.drop_ratio_bp",
		"ROS_VM_NETWORK_SUSTAINED_DAYS":              "network.sustained_days",
		"ROS_VM_ENABLE_NETWORK_SERIES":               "network.enable_network_series",
		"ROS_VM_ENABLE_PLACEMENT_CHECKS":             "placement.enable_placement_checks",
		"ROS_VM_PLACEMENT_SKEW_RATIO":                "placement.placement_skew_ratio",
		"ROS_VM_ENABLE_SHARED_PVC_CORRELATION":       "placement.enable_shared_pvc_correlation",
		"ROS_VM_NUMA_NODE_MEMORY_GIB":                "placement.numa_node_memory_gib",
		"ROS_VM_NUMA_ASSUMED_SOCKETS":                "placement.numa_assumed_sockets",
		"ROS_VM_ENABLE_POWER_SCHEDULE":               "power_schedule.enabled",
		"ROS_VM_POWER_OFF_MIN_IDLE_DAYS":             "power_schedule.min_idle_days",
		"ROS_VM_POWER_OFF_IDLE_RATIO_THRESHOLD":      "power_schedule.idle_ratio_threshold",
		"ROS_VM_NETWORK_QOS_ENABLED":                 "network_qos.enabled",
		"ROS_VM_NETWORK_QOS_SRIOV_DROP_THRESHOLD":    "network_qos.sriov_drop_threshold",
		"ROS_VM_NETWORK_QOS_SRIOV_THROUGHPUT_BPS":    "network_qos.sriov_throughput_bps",
		"ROS_VM_NETWORK_QOS_DPDK_PPS_THRESHOLD":      "network_qos.dpdk_pps_threshold",
		"ROS_VM_STORAGE_TIERING_ENABLED":             "storage_tiering.enabled",
		"ROS_VM_STORAGE_TIERING_MIN_DAYS":            "storage_tiering.min_days",
		"ROS_VM_STORAGE_TIERING_COLD_MIN_DAYS":       "storage_tiering.cold_min_days",
		"ROS_VM_STORAGE_TIERING_IOPS_MIN_DAYS":       "storage_tiering.iops_min_days",
		"ROS_VM_STORAGE_TIERING_THROUGHPUT_MIN_DAYS": "storage_tiering.throughput_min_days",
		"ROS_VM_STORAGE_TIERING_HIGH_IOPS_THRESHOLD": "storage_tiering.high_iops_threshold",
		"ROS_VM_STORAGE_TIERING_HIGH_THROUGHPUT_BPS": "storage_tiering.high_throughput_bps",
	}
}

func lockedVMFieldsFromEnv() []string {
	return engine.LockedFieldsFromEnvMap(vmEnvLockMap())
}

func vmRecConfigToThresholdAPI(cfg VMRecConfig) VMThresholdSettingsAPI {
	return VMThresholdSettingsAPI{
		CPUPercentileCost:       cfg.CPUPercentileCost,
		CPUPercentilePerf:       cfg.CPUPercentilePerf,
		CPUMarginMin:            cfg.CPUMarginMin,
		CPUMarginMax:            cfg.CPUMarginMax,
		MemMarginMin:            cfg.MemMarginMin,
		DownsizeHysteresisRatio: cfg.DownsizeHysteresisRatio,
		MinVCPUChange:           cfg.MinVCPUChange,
		MinGiBChange:            cfg.MinGiBChange,
		IdleCPUMC:               cfg.IdleCPUMC,
		IdleMemoryMiB:           cfg.IdleMemoryMiB,
		IdleCPUMCWindows:        cfg.IdleCPUMCWindows,
		IdleMemoryMiBWindows:    cfg.IdleMemoryMiBWindows,
		AbandonedMinDays:        cfg.AbandonedMinDays,
	}
}

func vmRecConfigToDiskAPI(cfg VMRecConfig) VMDiskSettingsAPI {
	return VMDiskSettingsAPI{
		ProjectionWindowDays: cfg.DiskProjectionWindowDays,
		HeadroomPct:          cfg.DiskHeadroomPct,
		RoundStepGiB:         cfg.DiskRoundStepGiB,
		MinGrowthMiBPerDay:   cfg.DiskMinGrowthMiBPerDay,
	}
}

func vmRecConfigToIOAPI(cfg VMRecConfig) VMIOSettingsAPI {
	return VMIOSettingsAPI{
		HighIOPSThreshold:        cfg.HighIOPSThreshold,
		SequentialThresholdBytes: cfg.IOSequentialThresholdBytes,
		RandomThresholdBytes:     cfg.IORandomThresholdBytes,
		MinIOPSForClassification: cfg.IOMinIOPSForClassification,
	}
}

func vmRecConfigToPowerScheduleAPI(cfg VMRecConfig) VMPowerScheduleSettingsAPI {
	return VMPowerScheduleSettingsAPI{
		Enabled:            cfg.EnablePowerSchedule,
		MinIdleDays:        cfg.PowerOffMinIdleDays,
		IdleRatioThreshold: cfg.PowerOffIdleRatioThreshold,
	}
}

func vmRecConfigToNetworkQoSAPI(cfg VMRecConfig) VMNetworkQoSSettingsAPI {
	return VMNetworkQoSSettingsAPI{
		Enabled:            cfg.NetworkQoSEnabled,
		SRIOVDropThreshold: cfg.NetworkQoSSRIOVDropThreshold,
		SRIOVThroughputBPS: cfg.NetworkQoSSRIOVThroughputBPS,
		DPDKPPSThreshold:   cfg.NetworkQoSDPDKPPSThreshold,
	}
}

func vmRecConfigToStorageTieringAPI(cfg VMRecConfig) VMStorageTieringSettingsAPI {
	return VMStorageTieringSettingsAPI{
		Enabled:           cfg.StorageTieringEnabled,
		MinDays:           cfg.StorageTieringMinDays,
		ColdMinDays:       cfg.StorageTieringColdMinDays,
		IOPSMinDays:       cfg.StorageTieringIOPSMinDays,
		ThroughputMinDays: cfg.StorageTieringThroughputMinDays,
		HighIOPSThreshold: cfg.StorageTieringHighIOPSThreshold,
		HighThroughputBPS: cfg.StorageTieringHighThroughputBPS,
	}
}

func vmRecConfigToPlacementAPI(cfg VMRecConfig) VMPlacementSettingsAPI {
	return VMPlacementSettingsAPI{
		EnablePlacementChecks:      cfg.EnablePlacementChecks,
		PlacementSkewRatio:         cfg.PlacementSkewRatio,
		EnableSharedPVCCorrelation: cfg.EnableSharedPVCCorrelation,
		NUMANodeMemoryGiB:          cfg.NUMANodeMemoryGiB,
		NUMAAssumedSockets:         cfg.NUMAAssumedSockets,
	}
}

func vmRecConfigToNetworkAPI(cfg VMRecConfig) VMNetworkSettingsAPI {
	return VMNetworkSettingsAPI{
		ThroughputThresholdBPS: cfg.NetworkThroughputThresholdBPS,
		PPSThreshold:           cfg.NetworkPPSThreshold,
		DropRatioBP:            cfg.NetworkDropRatioBP,
		SustainedDays:          cfg.NetworkSustainedDays,
		EnableNetworkSeries:    cfg.EnableNetworkSeries,
	}
}

func vmFractionToBasisPoints(f float64) int32 {
	return int32(f * 10000)
}

func vmBasisPointsToThresholdFraction(bp int32) float64 {
	return engine.VMBasisPointsToFraction(bp)
}

func vmRecConfigToGPUAPI(cfg VMRecConfig) VMGPUSettingsAPI {
	return VMGPUSettingsAPI{
		IdleThresholdBP:                 vmFractionToBasisPoints(cfg.GPUIdleThreshold),
		UnderutilThresholdBP:            vmFractionToBasisPoints(cfg.GPUUnderutilThreshold),
		FBSaturationMiB:                 int32(cfg.GPUFBSaturationMiB),
		ComputeSaturationThresholdBP:    vmFractionToBasisPoints(cfg.GPUComputeSaturationThreshold),
		TimeSliceMinReplicas:            cfg.GPUTimeSliceMinReplicas,
		TimeSliceMaxReplicas:            cfg.GPUTimeSliceMaxReplicas,
		TimeSliceFBSafetyThresholdBP:    cfg.GPUTimeSliceFBSafetyThresholdBP,
		TimeSliceDRAMPenaltyThresholdBP: cfg.GPUTimeSliceDRAMPenaltyThresholdBP,
	}
}

func vmRecConfigToMemoryFloorsAPI(cfg VMRecConfig) VMMemoryFloorsSettingsAPI {
	return VMMemoryFloorsSettingsAPI{
		LinuxGiB:                cfg.LinuxMemoryFloorGiB,
		WindowsGiB:              cfg.WindowsMemoryFloorGiB,
		WindowsKernelReserveGiB: cfg.WindowsKernelReserveGiB,
	}
}

func vmRecConfigToStabilityAPI(cfg VMRecConfig) VMStabilitySettingsAPI {
	return VMStabilitySettingsAPI{
		DownsizeStabilityDays:     cfg.DownsizeStabilityDays,
		CrashLoopRestartThreshold: cfg.CrashLoopRestartThreshold,
	}
}

func vmHistoryRetentionDaysFromConfig() int {
	cfg := config.GetConfig()
	if cfg == nil || cfg.VMRecHistoryRetentionDays < 1 {
		return 90
	}
	return cfg.VMRecHistoryRetentionDays
}

func vmSettingsResponseFromConfig(cfg VMRecConfig) VMSettingsResponse {
	envLocked := lockedVMFieldsFromEnv()
	return VMSettingsResponse{
		Enabled:                  vmFeatureEnabled(),
		CPUAdaptiveMarginEnabled: cfg.CPUAdaptiveMarginEnabled,
		HistoryRetentionDays:     vmHistoryRetentionDaysFromConfig(),
		Thresholds:               vmRecConfigToThresholdAPI(cfg),
		MemoryFloors:             vmRecConfigToMemoryFloorsAPI(cfg),
		Stability:                vmRecConfigToStabilityAPI(cfg),
		Disk:                     vmRecConfigToDiskAPI(cfg),
		IO:                       vmRecConfigToIOAPI(cfg),
		Network:                  vmRecConfigToNetworkAPI(cfg),
		GPU:                      vmRecConfigToGPUAPI(cfg),
		Placement:                vmRecConfigToPlacementAPI(cfg),
		PowerSchedule:            vmRecConfigToPowerScheduleAPI(cfg),
		NetworkQoS:               vmRecConfigToNetworkQoSAPI(cfg),
		StorageTiering:           vmRecConfigToStorageTieringAPI(cfg),
		InstanceTypeMatching:     cfg.EnableInstanceTypeMatching,
		LockedFields:             engine.LockedFieldsForAPI(vmRecommendationType, envLocked),
		SettingsLocked:           engine.IsSettingsLocked(vmRecommendationType),
	}
}

func vmFeatureEnabled() bool {
	return plugin.EnabledFor(vmRecommendationType)
}

// ResolveVMRecConfig returns effective VM recommendation config for an org.
func ResolveVMRecConfig(ctx context.Context, pool *pgxpool.Pool, orgID string) (VMRecConfig, error) {
	return engine.ResolveThresholdCached(ctx, pool, orgID, vmRecommendationType, resolveVMRecConfigUncached)
}

func resolveVMRecConfigUncached(ctx context.Context, pool *pgxpool.Pool, orgID string) (VMRecConfig, error) {
	result := VMRecConfigResolved()
	if !engine.IsSettingsLocked(vmRecommendationType) {
		overlay, err := loadVMSettingsStored(ctx, pool, orgID)
		if err != nil {
			return result, err
		}
		applyVMStoredOverlay(&result, overlay)
	}
	result = applyVMEnvLocks(result, config.GetConfig())
	return result, nil
}

// GetVMSettingsForAPI returns merged VM settings for GET.
func GetVMSettingsForAPI(ctx context.Context, pool *pgxpool.Pool, orgID string) (VMSettingsResponse, error) {
	cfg, err := ResolveVMRecConfig(ctx, pool, orgID)
	if err != nil {
		return VMSettingsResponse{}, err
	}
	return vmSettingsResponseFromConfig(cfg), nil
}

func loadVMSettingsStored(ctx context.Context, pool *pgxpool.Pool, orgID string) (*vmSettingsStored, error) {
	if pool == nil {
		return nil, nil
	}
	var raw []byte
	err := pool.QueryRow(ctx, `
		SELECT thresholds FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, vmRecommendationType,
	).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query VM settings: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var stored vmSettingsStored
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("decode VM settings: %w", err)
	}
	return &stored, nil
}

func applyVMStoredOverlay(dest *VMRecConfig, stored *vmSettingsStored) {
	if stored == nil {
		return
	}
	if stored.Thresholds != nil {
		t := stored.Thresholds
		dest.CPUPercentileCost = t.CPUPercentileCost
		dest.CPUPercentilePerf = t.CPUPercentilePerf
		dest.CPUMarginMin = t.CPUMarginMin
		dest.CPUMarginMax = t.CPUMarginMax
		dest.MemMarginMin = t.MemMarginMin
		dest.DownsizeHysteresisRatio = t.DownsizeHysteresisRatio
		dest.MinVCPUChange = t.MinVCPUChange
		dest.MinGiBChange = t.MinGiBChange
		dest.IdleCPUMC = t.IdleCPUMC
		dest.IdleMemoryMiB = t.IdleMemoryMiB
		dest.IdleCPUMCWindows = t.IdleCPUMCWindows
		dest.IdleMemoryMiBWindows = t.IdleMemoryMiBWindows
		dest.AbandonedMinDays = t.AbandonedMinDays
	}
	if stored.MemoryFloors != nil {
		dest.LinuxMemoryFloorGiB = stored.MemoryFloors.LinuxGiB
		dest.WindowsMemoryFloorGiB = stored.MemoryFloors.WindowsGiB
		dest.WindowsKernelReserveGiB = stored.MemoryFloors.WindowsKernelReserveGiB
	}
	if stored.Stability != nil {
		dest.DownsizeStabilityDays = stored.Stability.DownsizeStabilityDays
		dest.CrashLoopRestartThreshold = stored.Stability.CrashLoopRestartThreshold
	}
	if stored.Disk != nil {
		dest.DiskProjectionWindowDays = stored.Disk.ProjectionWindowDays
		dest.DiskHeadroomPct = stored.Disk.HeadroomPct
		dest.DiskRoundStepGiB = stored.Disk.RoundStepGiB
		dest.DiskMinGrowthMiBPerDay = stored.Disk.MinGrowthMiBPerDay
	}
	if stored.IO != nil {
		dest.HighIOPSThreshold = stored.IO.HighIOPSThreshold
		dest.IOSequentialThresholdBytes = stored.IO.SequentialThresholdBytes
		dest.IORandomThresholdBytes = stored.IO.RandomThresholdBytes
		dest.IOMinIOPSForClassification = stored.IO.MinIOPSForClassification
	}
	if stored.Network != nil {
		n := stored.Network
		dest.NetworkThroughputThresholdBPS = n.ThroughputThresholdBPS
		dest.NetworkPPSThreshold = n.PPSThreshold
		dest.NetworkDropRatioBP = n.DropRatioBP
		dest.NetworkSustainedDays = n.SustainedDays
		dest.EnableNetworkSeries = n.EnableNetworkSeries
	}
	if stored.Placement != nil {
		p := stored.Placement
		dest.EnablePlacementChecks = p.EnablePlacementChecks
		dest.PlacementSkewRatio = p.PlacementSkewRatio
		dest.EnableSharedPVCCorrelation = p.EnableSharedPVCCorrelation
		dest.NUMANodeMemoryGiB = p.NUMANodeMemoryGiB
		if p.NUMAAssumedSockets > 0 {
			dest.NUMAAssumedSockets = p.NUMAAssumedSockets
		}
	}
	if stored.PowerSchedule != nil {
		ps := stored.PowerSchedule
		dest.EnablePowerSchedule = ps.Enabled
		dest.PowerOffMinIdleDays = ps.MinIdleDays
		dest.PowerOffIdleRatioThreshold = ps.IdleRatioThreshold
	}
	if stored.NetworkQoS != nil {
		nq := stored.NetworkQoS
		dest.NetworkQoSEnabled = nq.Enabled
		dest.NetworkQoSSRIOVDropThreshold = nq.SRIOVDropThreshold
		dest.NetworkQoSSRIOVThroughputBPS = nq.SRIOVThroughputBPS
		dest.NetworkQoSDPDKPPSThreshold = nq.DPDKPPSThreshold
	}
	if stored.StorageTiering != nil {
		st := stored.StorageTiering
		dest.StorageTieringEnabled = st.Enabled
		dest.StorageTieringMinDays = st.MinDays
		dest.StorageTieringColdMinDays = st.ColdMinDays
		dest.StorageTieringIOPSMinDays = st.IOPSMinDays
		dest.StorageTieringThroughputMinDays = st.ThroughputMinDays
		dest.StorageTieringHighIOPSThreshold = st.HighIOPSThreshold
		dest.StorageTieringHighThroughputBPS = st.HighThroughputBPS
	}
	if stored.GPU != nil {
		g := stored.GPU
		dest.GPUIdleThreshold = vmBasisPointsToThresholdFraction(g.IdleThresholdBP)
		dest.GPUUnderutilThreshold = vmBasisPointsToThresholdFraction(g.UnderutilThresholdBP)
		dest.GPUFBSaturationMiB = float64(g.FBSaturationMiB)
		dest.GPUComputeSaturationThreshold = vmBasisPointsToThresholdFraction(g.ComputeSaturationThresholdBP)
		dest.GPUTimeSliceMinReplicas = g.TimeSliceMinReplicas
		dest.GPUTimeSliceMaxReplicas = g.TimeSliceMaxReplicas
		dest.GPUTimeSliceFBSafetyThresholdBP = g.TimeSliceFBSafetyThresholdBP
		dest.GPUTimeSliceDRAMPenaltyThresholdBP = g.TimeSliceDRAMPenaltyThresholdBP
	}
	if stored.InstanceTypeMatching != nil {
		dest.EnableInstanceTypeMatching = *stored.InstanceTypeMatching
	}
	if stored.CPUAdaptiveMarginEnabled != nil {
		dest.CPUAdaptiveMarginEnabled = *stored.CPUAdaptiveMarginEnabled
	}
}

// UpdateVMSettings validates and persists tenant VM settings overrides (partial updates allowed).
func UpdateVMSettings(ctx context.Context, pool *pgxpool.Pool, orgID string, rawUpdate json.RawMessage) error {
	if err := validateVMSettingsUpdate(rawUpdate); err != nil {
		return err
	}
	if locked := lockedVMFieldsInUpdate(rawUpdate); len(locked) > 0 {
		return fmt.Errorf("%w: %v", engine.ErrFieldsLocked, locked)
	}

	current, err := ResolveVMRecConfig(ctx, pool, orgID)
	if err != nil {
		return err
	}
	resp := vmSettingsResponseFromConfig(current)

	var patch map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &patch); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if raw, ok := patch["thresholds"]; ok {
		if err := json.Unmarshal(raw, &resp.Thresholds); err != nil {
			return fmt.Errorf("invalid thresholds: %w", err)
		}
	}
	if raw, ok := patch["memory_floors"]; ok {
		if err := json.Unmarshal(raw, &resp.MemoryFloors); err != nil {
			return fmt.Errorf("invalid memory_floors: %w", err)
		}
	}
	if raw, ok := patch["stability"]; ok {
		if err := json.Unmarshal(raw, &resp.Stability); err != nil {
			return fmt.Errorf("invalid stability: %w", err)
		}
	}
	if raw, ok := patch["disk"]; ok {
		if err := json.Unmarshal(raw, &resp.Disk); err != nil {
			return fmt.Errorf("invalid disk: %w", err)
		}
	}
	if raw, ok := patch["io"]; ok {
		if err := json.Unmarshal(raw, &resp.IO); err != nil {
			return fmt.Errorf("invalid io: %w", err)
		}
	}
	if raw, ok := patch["gpu"]; ok {
		if err := json.Unmarshal(raw, &resp.GPU); err != nil {
			return fmt.Errorf("invalid gpu: %w", err)
		}
	}
	if raw, ok := patch["network"]; ok {
		if err := json.Unmarshal(raw, &resp.Network); err != nil {
			return fmt.Errorf("invalid network: %w", err)
		}
	}
	if raw, ok := patch["placement"]; ok {
		if err := json.Unmarshal(raw, &resp.Placement); err != nil {
			return fmt.Errorf("invalid placement: %w", err)
		}
	}
	if raw, ok := patch["power_schedule"]; ok {
		if err := json.Unmarshal(raw, &resp.PowerSchedule); err != nil {
			return fmt.Errorf("invalid power_schedule: %w", err)
		}
	}
	if raw, ok := patch["network_qos"]; ok {
		if err := json.Unmarshal(raw, &resp.NetworkQoS); err != nil {
			return fmt.Errorf("invalid network_qos: %w", err)
		}
	}
	if raw, ok := patch["storage_tiering"]; ok {
		if err := json.Unmarshal(raw, &resp.StorageTiering); err != nil {
			return fmt.Errorf("invalid storage_tiering: %w", err)
		}
	}
	if raw, ok := patch["instance_type_matching"]; ok {
		if err := json.Unmarshal(raw, &resp.InstanceTypeMatching); err != nil {
			return fmt.Errorf("invalid instance_type_matching: %w", err)
		}
	}
	if raw, ok := patch["cpu_adaptive_margin_enabled"]; ok {
		if err := json.Unmarshal(raw, &resp.CPUAdaptiveMarginEnabled); err != nil {
			return fmt.Errorf("invalid cpu_adaptive_margin_enabled: %w", err)
		}
	}

	if err := validateVMSettingsResponse(resp); err != nil {
		return err
	}

	overrides := map[string]json.RawMessage{}
	if b, err := json.Marshal(resp.Thresholds); err == nil {
		overrides["thresholds"] = b
	}
	if b, err := json.Marshal(resp.MemoryFloors); err == nil {
		overrides["memory_floors"] = b
	}
	if b, err := json.Marshal(resp.Stability); err == nil {
		overrides["stability"] = b
	}
	if b, err := json.Marshal(resp.Disk); err == nil {
		overrides["disk"] = b
	}
	if b, err := json.Marshal(resp.IO); err == nil {
		overrides["io"] = b
	}
	if b, err := json.Marshal(resp.GPU); err == nil {
		overrides["gpu"] = b
	}
	if b, err := json.Marshal(resp.Network); err == nil {
		overrides["network"] = b
	}
	if b, err := json.Marshal(resp.Placement); err == nil {
		overrides["placement"] = b
	}
	if b, err := json.Marshal(resp.PowerSchedule); err == nil {
		overrides["power_schedule"] = b
	}
	if b, err := json.Marshal(resp.NetworkQoS); err == nil {
		overrides["network_qos"] = b
	}
	if b, err := json.Marshal(resp.StorageTiering); err == nil {
		overrides["storage_tiering"] = b
	}
	if b, err := json.Marshal(resp.InstanceTypeMatching); err == nil {
		overrides["instance_type_matching"] = b
	}
	if b, err := json.Marshal(resp.CPUAdaptiveMarginEnabled); err == nil {
		overrides["cpu_adaptive_margin_enabled"] = b
	}
	if err := engine.UpsertThresholdOverrides(ctx, pool, orgID, vmRecommendationType, overrides); err != nil {
		return err
	}
	engine.InvalidateThresholdCache(orgID, vmRecommendationType)
	return nil
}

func lockedVMFieldsInUpdate(rawUpdate json.RawMessage) []string {
	var update map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &update); err != nil {
		return nil
	}
	lockMap := vmEnvLockMap()
	var locked []string
	for envKey, field := range lockMap {
		if _, set := os.LookupEnv(envKey); !set {
			continue
		}
		topKey := field
		if idx := indexDot(field); idx >= 0 {
			topKey = field[:idx]
		}
		if _, ok := update[topKey]; ok {
			locked = append(locked, field)
		}
	}
	return locked
}

func indexDot(s string) int {
	for i, c := range s {
		if c == '.' {
			return i
		}
	}
	return -1
}

func validateVMSettingsUpdate(rawUpdate json.RawMessage) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &top); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	allowed := map[string]struct{}{
		"enabled": {}, "thresholds": {}, "memory_floors": {}, "stability": {}, "disk": {}, "io": {}, "network": {}, "gpu": {}, "placement": {}, "power_schedule": {}, "network_qos": {}, "storage_tiering": {},
		"instance_type_matching": {}, "cpu_adaptive_margin_enabled": {}, "locked_fields": {},
	}
	v := &engine.FieldValidator{}
	for key := range top {
		if _, ok := allowed[key]; !ok {
			v.AddConstraint("body", fmt.Sprintf("unknown field %q", key))
		}
	}
	return v.Result()
}

func validateVMSettingsResponse(resp VMSettingsResponse) error {
	v := &engine.FieldValidator{}

	v.AddRangeFloat("thresholds.cpu_percentile_cost", resp.Thresholds.CPUPercentileCost, 0.01, 1.0)
	v.AddRangeFloat("thresholds.cpu_percentile_perf", resp.Thresholds.CPUPercentilePerf, 0.01, 1.0)
	v.AddRangeFloat("thresholds.cpu_margin_min", resp.Thresholds.CPUMarginMin, 0.0, 5.0)
	v.AddRangeFloat("thresholds.cpu_margin_max", resp.Thresholds.CPUMarginMax, 0.0, 5.0)
	if resp.Thresholds.CPUMarginMin > resp.Thresholds.CPUMarginMax {
		v.AddConstraint("thresholds.cpu_margin_min", "must be less than or equal to cpu_margin_max")
	}
	v.AddRangeFloat("thresholds.mem_margin_min", resp.Thresholds.MemMarginMin, 0.0, 5.0)
	v.AddRangeFloat("thresholds.downsize_hysteresis_ratio", resp.Thresholds.DownsizeHysteresisRatio, 0.01, 1.0)
	v.AddRangeInt("thresholds.min_vcpu_change", int(resp.Thresholds.MinVCPUChange), 1, 64)
	v.AddRangeInt("thresholds.min_gib_change", int(resp.Thresholds.MinGiBChange), 1, 1024)
	v.AddRangeInt64("thresholds.idle_cpu_mc", resp.Thresholds.IdleCPUMC, 0, 100000)
	v.AddRangeInt64("thresholds.idle_memory_mib", resp.Thresholds.IdleMemoryMiB, 0, 1048576)
	v.AddRangeInt64("thresholds.idle_cpu_mc_windows", resp.Thresholds.IdleCPUMCWindows, 0, 100000)
	v.AddRangeInt64("thresholds.idle_memory_mib_windows", resp.Thresholds.IdleMemoryMiBWindows, 0, 1048576)
	v.AddRangeInt("thresholds.abandoned_min_days", int(resp.Thresholds.AbandonedMinDays), 1, 90)

	v.AddRangeInt("memory_floors.linux_gib", int(resp.MemoryFloors.LinuxGiB), 1, 1024)
	v.AddRangeInt("memory_floors.windows_gib", int(resp.MemoryFloors.WindowsGiB), 1, 1024)
	v.AddRangeFloat("memory_floors.windows_kernel_reserve_gib", resp.MemoryFloors.WindowsKernelReserveGiB, 0.0, 64.0)

	v.AddRangeInt("stability.downsize_stability_days", resp.Stability.DownsizeStabilityDays, 1, 30)
	v.AddRangeInt("stability.crash_loop_restart_threshold", int(resp.Stability.CrashLoopRestartThreshold), 1, 100)

	v.AddRangeInt("disk.projection_window_days", int(resp.Disk.ProjectionWindowDays), 1, 365)
	v.AddRangeFloat("disk.headroom_pct", resp.Disk.HeadroomPct, 0.0, 5.0)
	v.AddRangeInt("disk.round_step_gib", int(resp.Disk.RoundStepGiB), 1, 1024)
	v.AddRangeInt64("disk.min_growth_mib_per_day", resp.Disk.MinGrowthMiBPerDay, 1, 1048576)

	v.AddRangeInt64("io.high_iops_threshold", resp.IO.HighIOPSThreshold, 1, 10000000)
	v.AddRangeInt64("io.sequential_threshold_bytes", resp.IO.SequentialThresholdBytes, 4096, 1048576)
	v.AddRangeInt64("io.random_threshold_bytes", resp.IO.RandomThresholdBytes, 512, 524288)
	if resp.IO.RandomThresholdBytes >= resp.IO.SequentialThresholdBytes {
		v.AddConstraint("io.random_threshold_bytes", "must be less than sequential_threshold_bytes")
	}
	v.AddRangeInt64("io.min_iops_for_classification", resp.IO.MinIOPSForClassification, 1, 1000000)

	v.AddRangeInt64("network.throughput_threshold_bps", resp.Network.ThroughputThresholdBPS, 1, 1_000_000_000_000)
	v.AddRangeInt64("network.pps_threshold", resp.Network.PPSThreshold, 1, 100_000_000)
	v.AddRangeInt("network.drop_ratio_bp", int(resp.Network.DropRatioBP), 0, 10000)
	v.AddRangeInt("network.sustained_days", resp.Network.SustainedDays, 1, 90)

	v.AddRangeInt("gpu.idle_threshold_bp", int(resp.GPU.IdleThresholdBP), 0, 10000)
	v.AddRangeInt("gpu.underutil_threshold_bp", int(resp.GPU.UnderutilThresholdBP), 0, 10000)
	v.AddRangeInt("gpu.fb_saturation_mib", int(resp.GPU.FBSaturationMiB), 0, 1048576)
	v.AddRangeInt("gpu.compute_saturation_threshold_bp", int(resp.GPU.ComputeSaturationThresholdBP), 0, 10000)
	if resp.GPU.IdleThresholdBP > resp.GPU.UnderutilThresholdBP {
		v.AddConstraint("gpu.idle_threshold_bp", "must be less than or equal to underutil_threshold_bp")
	}
	if resp.GPU.UnderutilThresholdBP > resp.GPU.ComputeSaturationThresholdBP {
		v.AddConstraint("gpu.underutil_threshold_bp", "must be less than or equal to compute_saturation_threshold_bp")
	}

	v.AddRangeInt("gpu.gpu_timeslice_min_replicas", int(resp.GPU.TimeSliceMinReplicas), 1, 16)
	v.AddRangeInt("gpu.gpu_timeslice_max_replicas", int(resp.GPU.TimeSliceMaxReplicas), 1, 32)
	if resp.GPU.TimeSliceMinReplicas > resp.GPU.TimeSliceMaxReplicas {
		v.AddConstraint("gpu.gpu_timeslice_min_replicas", "must be less than or equal to gpu_timeslice_max_replicas")
	}
	v.AddRangeInt("gpu.gpu_timeslice_fb_safety_threshold_bp", int(resp.GPU.TimeSliceFBSafetyThresholdBP), 1000, 10000)
	v.AddRangeInt("gpu.gpu_timeslice_dram_penalty_threshold_bp", int(resp.GPU.TimeSliceDRAMPenaltyThresholdBP), 1000, 10000)

	v.AddRangeInt("placement.placement_skew_ratio", resp.Placement.PlacementSkewRatio, 2, 20)
	v.AddRangeFloat("placement.numa_node_memory_gib", resp.Placement.NUMANodeMemoryGiB, 1.0, 2048.0)
	v.AddRangeInt("placement.numa_assumed_sockets", resp.Placement.NUMAAssumedSockets, 1, 64)

	v.AddRangeInt("power_schedule.min_idle_days", int(resp.PowerSchedule.MinIdleDays), 1, 90)
	v.AddRangeFloat("power_schedule.idle_ratio_threshold", resp.PowerSchedule.IdleRatioThreshold, 0.01, 1.0)

	v.AddRangeFloat("network_qos.sriov_drop_threshold", resp.NetworkQoS.SRIOVDropThreshold, 0.0, 1.0)
	v.AddRangeInt64("network_qos.sriov_throughput_bps", resp.NetworkQoS.SRIOVThroughputBPS, 1, 1_000_000_000_000)
	v.AddRangeInt64("network_qos.dpdk_pps_threshold", resp.NetworkQoS.DPDKPPSThreshold, 1, 100_000_000)

	v.AddRangeInt("storage_tiering.min_days", resp.StorageTiering.MinDays, 1, 90)
	v.AddRangeInt("storage_tiering.cold_min_days", resp.StorageTiering.ColdMinDays, 1, 90)
	v.AddRangeInt("storage_tiering.iops_min_days", resp.StorageTiering.IOPSMinDays, 1, 90)
	v.AddRangeInt("storage_tiering.throughput_min_days", resp.StorageTiering.ThroughputMinDays, 1, 90)
	v.AddRangeInt64("storage_tiering.high_iops_threshold", resp.StorageTiering.HighIOPSThreshold, 1, 10_000_000)
	v.AddRangeInt64("storage_tiering.high_throughput_bps", resp.StorageTiering.HighThroughputBPS, 1, 1_000_000_000_000)

	return v.Result()
}

// DeleteVMTermSettings removes tenant VM term overrides.
func DeleteVMTermSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	_, err := pool.Exec(ctx,
		`DELETE FROM org_recommendation_terms WHERE org_id = $1 AND recommendation_type = $2`,
		orgID, vmRecommendationType)
	if err != nil {
		return fmt.Errorf("delete VM term settings: %w", err)
	}
	engine.InvalidateTermCache(orgID, vmRecommendationType)
	return nil
}

// DeleteVMSettings removes tenant VM settings overrides.
func DeleteVMSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	_, err := pool.Exec(ctx, `
		DELETE FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, vmRecommendationType)
	if err != nil {
		return fmt.Errorf("delete VM settings: %w", err)
	}
	engine.InvalidateThresholdCache(orgID, vmRecommendationType)
	return nil
}
