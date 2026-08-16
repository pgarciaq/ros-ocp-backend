package node

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/fixedpoint"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// defaultLowConfidenceThreshold is the confidence level below which a warning is emitted.
const defaultLowConfidenceThreshold float32 = 0.5

// defaultSparseDataThreshold is the data-day count at or below which a sparsity warning is emitted.
const defaultSparseDataThreshold int = 2

// ThresholdSettings holds node classification and dual-engine sizing parameters.
type ThresholdSettings struct {
	UnderutilThreshold                  float64 `json:"underutil_threshold"`
	OvercommitThreshold                 float64 `json:"overcommit_threshold"`
	AllocatableFactor                   float64 `json:"allocatable_factor"`
	StrandedImbalanceThreshold          float64 `json:"stranded_imbalance_threshold"`
	EMAAlpha                            float64 `json:"ema_alpha"`
	CostTargetUtilization               float64 `json:"cost_target_utilization"`
	PerfTargetUtilization               float64 `json:"perf_target_utilization"`
	PerfConsolidationHeadroomMultiplier float64 `json:"perf_consolidation_headroom_multiplier"`
	TrendMinDays                        int     `json:"trend_min_days"`
	ZombieCPUP95MC                      int64   `json:"zombie_cpu_p95_mc"`
	ZombieMaxPods                       int64   `json:"zombie_max_pods"`
	IdleCPUUtilPct                      int64   `json:"idle_cpu_util_pct"`
	IdleMemUtilPct                      int64   `json:"idle_mem_util_pct"`
	IdleMaxPods                         int64   `json:"idle_max_pods"`
	PodHeadroomConsolidationGate        float64 `json:"pod_headroom_consolidation_gate"`
	PodHeadroomNotificationThreshold    float64 `json:"pod_headroom_notification_threshold"`
}

// EngineConfig holds per-engine sizing parameters for node recommendations.
type EngineConfig struct {
	Name              string
	TargetUtilization float64
}

// RecConfig holds configuration parameters for the node recommendation engine.
// Ratio thresholds are stored as basis points (MarginScale = 10000 = 100%).
type RecConfig struct {
	UnderutilThresholdBP         int32
	OvercommitThresholdBP        int32
	AllocatableFactor            float64
	StrandedImbalanceThresholdBP int32
	EMAAlpha                     float64
}

// EnginesFromThresholds builds per-engine target utilization from resolved settings.
func EnginesFromThresholds(th ThresholdSettings) []EngineConfig {
	return []EngineConfig{
		{Name: "cost", TargetUtilization: th.CostTargetUtilization},
		{Name: "performance", TargetUtilization: th.PerfTargetUtilization},
	}
}

// RecConfigFromThresholds converts resolved node threshold settings to RecConfig.
func RecConfigFromThresholds(th ThresholdSettings) RecConfig {
	return RecConfig{
		UnderutilThresholdBP:         fixedpoint.FloatToBasisPoints(th.UnderutilThreshold),
		OvercommitThresholdBP:        fixedpoint.RatioToBasisPoints(th.OvercommitThreshold),
		AllocatableFactor:            th.AllocatableFactor,
		StrandedImbalanceThresholdBP: fixedpoint.FloatToBasisPoints(th.StrandedImbalanceThreshold),
		EMAAlpha:                     th.EMAAlpha,
	}
}

// DigestRow represents a single daily digest for a node, loaded from the database.
type DigestRow struct {
	BucketDate        time.Time
	Node              string
	CPUUsageP50MC     int64
	CPUUsageP95MC     int64
	CPUUsageMaxMC     int64
	MemUsageP50KiB    int64
	MemUsageP95KiB    int64
	MemUsageMaxKiB    int64
	MaxCPUAllocMC     *int64
	MaxMemAllocKiB    *int64
	MaxCPURequestsMC  int64
	MaxMemRequestsKiB int64
	MaxPodCount       int64
	PodCapacity       int64
	InstanceType      string
	MachineSetName    string
	SampleCount       int64
	NodeGPUCount      *int64
}

// Rec holds the computed recommendation for a single node within a single term and engine.
type Rec struct {
	Node                         string
	Term                         string
	Engine                       string
	CPUUtilP50                   float32
	CPUUtilP95                   float32
	MemUtilP50                   float32
	MemUtilP95                   float32
	CPUOvercommitRatio           float32
	Category                     string
	IdleState                    types.IdleState
	StrandedResource             *string
	PodCount                     int64
	PodCapacity                  int64
	MachineSetName               string
	TrendSlope                   float32
	CurrentCPUMC                 int64
	CurrentMemKiB                int64
	RecommendedCPUMC             int64
	RecommendedMemKiB            int64
	NodeCountReduction           int
	EstimatedMonthlySavingsCents int64
	InstanceType                 string
	SuggestedInstanceType        string
	InstanceTypeReason           string
	DataDays                     int
	ConfidenceLevel              float32
	NotificationCodes            []int16
	Expl                         types.NodeExplanationFactors
	NodeGPUCount                 *int64
}

// DefaultThresholdSettings returns compiled defaults for node recommendations.
func DefaultThresholdSettings() ThresholdSettings {
	return ThresholdSettings{
		UnderutilThreshold:                  0.30,
		OvercommitThreshold:                 1.50,
		AllocatableFactor:                   0.93,
		StrandedImbalanceThreshold:          0.60,
		EMAAlpha:                            0.30,
		CostTargetUtilization:               0.80,
		PerfTargetUtilization:               0.55,
		PerfConsolidationHeadroomMultiplier: 2.0,
		TrendMinDays:                        3,
		ZombieCPUP95MC:                      200,
		ZombieMaxPods:                       5,
		IdleCPUUtilPct:                      10,
		IdleMemUtilPct:                      10,
		IdleMaxPods:                         10,
		PodHeadroomConsolidationGate:        0.15,
		PodHeadroomNotificationThreshold:    0.10,
	}
}
