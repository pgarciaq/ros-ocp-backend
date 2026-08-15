package vm

// VMRecConfig holds all configurable thresholds for VM recommendations.
type VMRecConfig struct {
	// CPU percentiles
	CPUPercentileCost float64 // default 0.95
	CPUPercentilePerf float64 // default 0.99

	// Margins
	CPUMarginMin             float64 // default 0.15 (15%)
	CPUMarginMax             float64 // default 0.50 (50%)
	CPUAdaptiveMarginEnabled bool    // default true — cost engine uses CV-based margin
	MemMarginMin             float64 // default 0.20 (20%)

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
	HighIOPSThreshold          int64 // default 3000
	IOSequentialThresholdBytes int64 // default 65536 (64 KiB)
	IORandomThresholdBytes     int64 // default 16384 (16 KiB)
	IOMinIOPSForClassification int64 // default 100

	// Instance type
	EnableInstanceTypeMatching bool // default true

	// Abandoned: minimum consecutive days of zero CPU and memory max usage in the term window
	AbandonedMinDays int32 // default 3 (≈72h at daily digest granularity)

	// Power-off scheduling (periodically idle VMs)
	EnablePowerSchedule        bool    // default true
	PowerOffMinIdleDays        int32   // default 14 — minimum digest history for detection
	PowerOffIdleRatioThreshold float64 // default 0.7 — fraction of days that must be idle

	// GPU thresholds for VM passthrough/vGPU recommendations
	GPUIdleThreshold              float64 // default 0.05 (5%)
	GPUUnderutilThreshold         float64 // default 0.30 (30%)
	GPUFBSaturationMiB            float64 // default 0 (auto-detect from model catalog)
	GPUComputeSaturationThreshold float64 // default 0.85 (85%)

	// vGPU time-slicing (non-MIG passthrough)
	GPUTimeSliceMinReplicas            int32 // default 2
	GPUTimeSliceMaxReplicas            int32 // default 16
	GPUTimeSliceFBSafetyThresholdBP    int32 // default 8000 (80% FB → do not time-slice)
	GPUTimeSliceDRAMPenaltyThresholdBP int32 // default 5000 (50% DRAM → reduce max slices)

	// Network-optimized (n1) classification
	NetworkThroughputThresholdBPS int64 // default 62_500_000 (~500 Mbps)
	NetworkPPSThreshold           int64 // default 100_000
	NetworkDropRatioBP            int32 // default 10 (0.1%)
	NetworkSustainedDays          int   // default 7
	EnableNetworkSeries           bool  // default true

	// Network QoS hints (SR-IOV / DPDK notifications 65–66)
	NetworkQoSEnabled            bool    // default true
	NetworkQoSSRIOVDropThreshold float64 // default 0.01 (1% drop rate)
	NetworkQoSSRIOVThroughputBPS int64   // default 5_000_000_000 (5 Gbps)
	NetworkQoSDPDKPPSThreshold   int64   // default 500_000

	// Storage tiering hints (notifications 67–69)
	StorageTieringEnabled           bool  // default true
	StorageTieringMinDays           int   // default 7 — minimum digest history
	StorageTieringColdMinDays       int   // default 14 — low-io days for cold tier hint
	StorageTieringIOPSMinDays       int   // default 7 — random high-IOPS days
	StorageTieringThroughputMinDays int   // default 7 — sequential high-throughput days
	StorageTieringHighIOPSThreshold int64 // default 5000
	StorageTieringHighThroughputBPS int64 // default 104857600 (100 MiB/s)

	// Placement / NUMA (cluster-wide checks; no app labels in VM CSV today)
	EnablePlacementChecks      bool    // default true
	PlacementSkewRatio         int     // default 3 — max:min VM count per node before skew notification
	EnableSharedPVCCorrelation bool    // default true — namespace profile peers until PVC column exists
	NUMANodeMemoryGiB          float64 // default 64 — per-NUMA-node cap when node digest memory is unavailable
	NUMAAssumedSockets         int     // default 2 — divides node allocatable memory for per-NUMA estimate
}

// DefaultVMRecConfig returns the compiled defaults for VM recommendations.
func DefaultVMRecConfig() VMRecConfig {
	return VMRecConfig{
		CPUPercentileCost:                  0.95,
		CPUPercentilePerf:                  0.99,
		CPUMarginMin:                       0.15,
		CPUMarginMax:                       0.50,
		CPUAdaptiveMarginEnabled:           true,
		MemMarginMin:                       0.20,
		DownsizeHysteresisRatio:            0.60,
		MinVCPUChange:                      2,
		MinGiBChange:                       2,
		IdleCPUMC:                          50,
		IdleMemoryMiB:                      512,
		IdleCPUMCWindows:                   200,
		IdleMemoryMiBWindows:               3072,
		LinuxMemoryFloorGiB:                1,
		WindowsMemoryFloorGiB:              2,
		WindowsKernelReserveGiB:            1.5,
		DownsizeStabilityDays:              3,
		CrashLoopRestartThreshold:          3,
		DiskProjectionWindowDays:           30,
		DiskHeadroomPct:                    0.25,
		DiskRoundStepGiB:                   10,
		DiskMinGrowthMiBPerDay:             100,
		HighIOPSThreshold:                  3000,
		IOSequentialThresholdBytes:         65536,
		IORandomThresholdBytes:             16384,
		IOMinIOPSForClassification:         100,
		EnableInstanceTypeMatching:         true,
		AbandonedMinDays:                   3,
		EnablePowerSchedule:                true,
		PowerOffMinIdleDays:                14,
		PowerOffIdleRatioThreshold:         0.7,
		GPUIdleThreshold:                   0.05,
		GPUUnderutilThreshold:              0.30,
		GPUFBSaturationMiB:                 0,
		GPUComputeSaturationThreshold:      0.85,
		GPUTimeSliceMinReplicas:            2,
		GPUTimeSliceMaxReplicas:            16,
		GPUTimeSliceFBSafetyThresholdBP:    8000,
		GPUTimeSliceDRAMPenaltyThresholdBP: 5000,
		NetworkThroughputThresholdBPS:      62_500_000,
		NetworkPPSThreshold:                100_000,
		NetworkDropRatioBP:                 10,
		NetworkSustainedDays:               7,
		EnableNetworkSeries:                true,
		NetworkQoSEnabled:                  true,
		NetworkQoSSRIOVDropThreshold:       0.01,
		NetworkQoSSRIOVThroughputBPS:       5_000_000_000,
		NetworkQoSDPDKPPSThreshold:         500_000,
		StorageTieringEnabled:              true,
		StorageTieringMinDays:              7,
		StorageTieringColdMinDays:          14,
		StorageTieringIOPSMinDays:          7,
		StorageTieringThroughputMinDays:    7,
		StorageTieringHighIOPSThreshold:    5000,
		StorageTieringHighThroughputBPS:    104857600,
		EnablePlacementChecks:              true,
		PlacementSkewRatio:                 3,
		EnableSharedPVCCorrelation:         true,
		NUMANodeMemoryGiB:                  64,
		NUMAAssumedSockets:                 2,
	}
}
