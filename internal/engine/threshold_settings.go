package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

const thresholdSettingsCacheTTL = 60 * time.Second

type thresholdSettingsCacheKey struct {
	orgID              string
	recommendationType string
}

type thresholdSettingsCacheEntry struct {
	value any
	until time.Time
}

var (
	thresholdSettingsMu    sync.RWMutex
	thresholdSettingsCache = map[thresholdSettingsCacheKey]thresholdSettingsCacheEntry{}
)

// InvalidateThresholdCache removes cached threshold entries for an org + recommendation type,
// ensuring subsequent Resolve* calls will re-read from DB.
func InvalidateThresholdCache(orgID, recommendationType string) {
	thresholdSettingsMu.Lock()
	delete(thresholdSettingsCache, thresholdSettingsCacheKey{orgID: orgID, recommendationType: recommendationType})
	thresholdSettingsMu.Unlock()
}

func resolveThresholdCached[T any](
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, recType string,
	resolve func(context.Context, *pgxpool.Pool, string) (T, error),
) (T, error) {
	var zero T
	if pool == nil {
		return resolve(ctx, pool, orgID)
	}
	now := time.Now().UTC()
	key := thresholdSettingsCacheKey{orgID: orgID, recommendationType: recType}

	thresholdSettingsMu.RLock()
	e, ok := thresholdSettingsCache[key]
	thresholdSettingsMu.RUnlock()
	if ok && now.Before(e.until) {
		if v, ok := e.value.(T); ok {
			return v, nil
		}
	}

	result, err := resolve(ctx, pool, orgID)
	if err != nil {
		return zero, err
	}
	thresholdSettingsMu.Lock()
	thresholdSettingsCache[key] = thresholdSettingsCacheEntry{value: result, until: now.Add(thresholdSettingsCacheTTL)}
	thresholdSettingsMu.Unlock()
	return result, nil
}

// Valid threshold recommendation types for the Settings API.
var validThresholdRecommendationTypes = map[string]struct{}{
	"container": {},
	"namespace": {},
	"node":      {},
	"gpu":       {},
	"pvc":       {},
}

// SizingThresholdSettings holds CPU/memory sizing and notification thresholds
// for container and namespace recommendation plugins.
type SizingThresholdSettings struct {
	CPUCostPercentile        float64 `json:"cpu_cost_percentile"`
	CPUPerfPercentile        float64 `json:"cpu_perf_percentile"`
	MemCostPercentile        float64 `json:"mem_cost_percentile"`
	MemPerfPercentile        float64 `json:"mem_perf_percentile"`
	MinMargin                float64 `json:"min_margin"`
	MaxMargin                float64 `json:"max_margin"`
	LimitMultiplier          float64 `json:"limit_multiplier"`
	CPUFloorMC               int64   `json:"cpu_floor_mc"`
	IdleCPUThresholdMC       int64   `json:"idle_cpu_threshold_mc"`
	IdleMemThresholdKiB      int64   `json:"idle_mem_threshold_kib"`
	MemTrendSlopeThreshold   float64 `json:"mem_trend_slope_threshold"`
	LowConfidenceThreshold   float32 `json:"low_confidence_threshold"`
}

// SizingThresholdSettingsResponse is the API GET response for container/namespace.
type SizingThresholdSettingsResponse struct {
	SizingThresholdSettings
	LockedFields []string `json:"locked_fields"`
}

// SizingThresholdSettingsUpdate is the API PUT body for container/namespace.
type SizingThresholdSettingsUpdate struct {
	CPUCostPercentile      *float64 `json:"cpu_cost_percentile,omitempty"`
	CPUPerfPercentile      *float64 `json:"cpu_perf_percentile,omitempty"`
	MemCostPercentile      *float64 `json:"mem_cost_percentile,omitempty"`
	MemPerfPercentile      *float64 `json:"mem_perf_percentile,omitempty"`
	MinMargin              *float64 `json:"min_margin,omitempty"`
	MaxMargin              *float64 `json:"max_margin,omitempty"`
	LimitMultiplier        *float64 `json:"limit_multiplier,omitempty"`
	CPUFloorMC             *int64   `json:"cpu_floor_mc,omitempty"`
	IdleCPUThresholdMC     *int64   `json:"idle_cpu_threshold_mc,omitempty"`
	IdleMemThresholdKiB    *int64   `json:"idle_mem_threshold_kib,omitempty"`
	MemTrendSlopeThreshold *float64 `json:"mem_trend_slope_threshold,omitempty"`
	LowConfidenceThreshold *float32 `json:"low_confidence_threshold,omitempty"`
}

// NodeThresholdSettings holds node classification and dual-engine sizing parameters.
type NodeThresholdSettings struct {
	UnderutilThreshold                  float64 `json:"underutil_threshold"`
	OvercommitThreshold                 float64 `json:"overcommit_threshold"`
	AllocatableFactor                   float64 `json:"allocatable_factor"`
	StrandedImbalanceThreshold          float64 `json:"stranded_imbalance_threshold"`
	EMAAlpha                            float64 `json:"ema_alpha"`
	CostTargetUtilization               float64 `json:"cost_target_utilization"`
	PerfTargetUtilization               float64 `json:"perf_target_utilization"`
	PerfConsolidationHeadroomMultiplier float64 `json:"perf_consolidation_headroom_multiplier"`
	TrendMinDays                        int     `json:"trend_min_days"`
}

// NodeThresholdSettingsResponse is the API GET response for node thresholds.
type NodeThresholdSettingsResponse struct {
	NodeThresholdSettings
	LockedFields []string `json:"locked_fields"`
}

// NodeThresholdSettingsUpdate is the API PUT body for node thresholds.
type NodeThresholdSettingsUpdate struct {
	UnderutilThreshold                  *float64 `json:"underutil_threshold,omitempty"`
	OvercommitThreshold                 *float64 `json:"overcommit_threshold,omitempty"`
	AllocatableFactor                   *float64 `json:"allocatable_factor,omitempty"`
	StrandedImbalanceThreshold          *float64 `json:"stranded_imbalance_threshold,omitempty"`
	EMAAlpha                            *float64 `json:"ema_alpha,omitempty"`
	CostTargetUtilization               *float64 `json:"cost_target_utilization,omitempty"`
	PerfTargetUtilization               *float64 `json:"perf_target_utilization,omitempty"`
	PerfConsolidationHeadroomMultiplier *float64 `json:"perf_consolidation_headroom_multiplier,omitempty"`
	TrendMinDays                        *int     `json:"trend_min_days,omitempty"`
}

// GPUThresholdSettings holds GPU classification, confidence, and time-slicing parameters.
type GPUThresholdSettings struct {
	GPUThresholds
	ComputeBoundDRAMThreshold     float64 `json:"compute_bound_dram_threshold"`
	MIGFBPercentile               float64 `json:"mig_fb_percentile"`
	ConfidenceDaysTier1           int     `json:"confidence_days_tier1"`
	ConfidenceDaysTier2           int     `json:"confidence_days_tier2"`
	ConfidenceDaysTier3           int     `json:"confidence_days_tier3"`
	SpikeRatioThreshold           float64 `json:"spike_ratio_threshold"`
	SpikeConfidencePenalty        float64 `json:"spike_confidence_penalty"`
	NoProfilingConfidenceFactor   float64 `json:"no_profiling_confidence_factor"`
	TimeslicingMajorityThreshold  float64 `json:"timeslicing_majority_threshold"`
	TimeslicingMinReplicas        int     `json:"timeslicing_min_replicas"`
	TimeslicingMaxReplicas        int     `json:"timeslicing_max_replicas"`
	TimeslicingBasePenalty        float64 `json:"timeslicing_base_penalty"`
	TimeslicingImpactedWeight     float64 `json:"timeslicing_impacted_weight"`
	NodeFreshnessDays             int     `json:"node_freshness_days"`
}

// GPUThresholdSettingsResponse is the API GET response for GPU thresholds.
type GPUThresholdSettingsResponse struct {
	GPUThresholdSettings
	LockedFields []string `json:"locked_fields"`
}

// GPUThresholdSettingsUpdate is the API PUT body for GPU thresholds.
type GPUThresholdSettingsUpdate struct {
	IdleThreshold                   *float64 `json:"idle_threshold,omitempty"`
	UnderutilizedSMThreshold        *float64 `json:"underutilized_sm_threshold,omitempty"`
	UnderutilizedTensorThreshold    *float64 `json:"underutilized_tensor_threshold,omitempty"`
	MemBoundDRAMThreshold           *float64 `json:"membound_dram_threshold,omitempty"`
	MemBoundTensorThreshold         *float64 `json:"membound_tensor_threshold,omitempty"`
	FBHeadroomFactor                *float64 `json:"fb_headroom_factor,omitempty"`
	ComputeBoundDRAMThreshold       *float64 `json:"compute_bound_dram_threshold,omitempty"`
	MIGFBPercentile                 *float64 `json:"mig_fb_percentile,omitempty"`
	ConfidenceDaysTier1             *int     `json:"confidence_days_tier1,omitempty"`
	ConfidenceDaysTier2             *int     `json:"confidence_days_tier2,omitempty"`
	ConfidenceDaysTier3             *int     `json:"confidence_days_tier3,omitempty"`
	SpikeRatioThreshold             *float64 `json:"spike_ratio_threshold,omitempty"`
	SpikeConfidencePenalty          *float64 `json:"spike_confidence_penalty,omitempty"`
	NoProfilingConfidenceFactor     *float64 `json:"no_profiling_confidence_factor,omitempty"`
	TimeslicingMajorityThreshold    *float64 `json:"timeslicing_majority_threshold,omitempty"`
	TimeslicingMinReplicas          *int     `json:"timeslicing_min_replicas,omitempty"`
	TimeslicingMaxReplicas          *int     `json:"timeslicing_max_replicas,omitempty"`
	TimeslicingBasePenalty          *float64 `json:"timeslicing_base_penalty,omitempty"`
	TimeslicingImpactedWeight       *float64 `json:"timeslicing_impacted_weight,omitempty"`
	NodeFreshnessDays               *int     `json:"node_freshness_days,omitempty"`
}

// PVCThresholdSettings holds PVC right-sizing classification parameters.
type PVCThresholdSettings struct {
	OversizedThreshold          float64 `json:"oversized_threshold"`
	NearFullThreshold           float64 `json:"near_full_threshold"`
	MinTrendDays                int     `json:"min_trend_days"`
	RecommendedSizeMultiplier   int     `json:"recommended_size_multiplier"`
	MinRecommendedGiB           int     `json:"min_recommended_gib"`
	DaysToFullAlert             int     `json:"days_to_full_alert"`
}

// PVCThresholdSettingsResponse is the API GET response for PVC thresholds.
type PVCThresholdSettingsResponse struct {
	PVCThresholdSettings
	LockedFields []string `json:"locked_fields"`
}

// PVCThresholdSettingsUpdate is the API PUT body for PVC thresholds.
type PVCThresholdSettingsUpdate struct {
	OversizedThreshold        *float64 `json:"oversized_threshold,omitempty"`
	NearFullThreshold         *float64 `json:"near_full_threshold,omitempty"`
	MinTrendDays              *int     `json:"min_trend_days,omitempty"`
	RecommendedSizeMultiplier *int     `json:"recommended_size_multiplier,omitempty"`
	MinRecommendedGiB         *int     `json:"min_recommended_gib,omitempty"`
	DaysToFullAlert           *int     `json:"days_to_full_alert,omitempty"`
}

// DefaultContainerSizingThresholds returns compiled defaults for container recommendations.
func DefaultContainerSizingThresholds() SizingThresholdSettings {
	return SizingThresholdSettings{
		CPUCostPercentile:      0.60,
		CPUPerfPercentile:      0.98,
		MemCostPercentile:      0.95,
		MemPerfPercentile:      1.0,
		MinMargin:              1.15,
		MaxMargin:              1.50,
		LimitMultiplier:        1.05,
		CPUFloorMC:             25,
		IdleCPUThresholdMC:     DefaultIdleThresholdMC,
		IdleMemThresholdKiB:    DefaultIdleThresholdMemKiB,
		MemTrendSlopeThreshold: 100.0,
		LowConfidenceThreshold: 0.5,
	}
}

// DefaultNamespaceSizingThresholds returns compiled defaults for namespace recommendations.
func DefaultNamespaceSizingThresholds() SizingThresholdSettings {
	th := DefaultContainerSizingThresholds()
	// Namespace aggregates exhibit larger absolute memory swings than a single container.
	th.MemTrendSlopeThreshold = namespaceMemTrendSlopeThreshold
	return th
}

// DefaultNodeThresholdSettings returns compiled defaults for node recommendations.
func DefaultNodeThresholdSettings() NodeThresholdSettings {
	return NodeThresholdSettings{
		UnderutilThreshold:                  0.30,
		OvercommitThreshold:                 1.50,
		AllocatableFactor:                   0.93,
		StrandedImbalanceThreshold:          0.60,
		EMAAlpha:                            0.30,
		CostTargetUtilization:               0.80,
		PerfTargetUtilization:               0.55,
		PerfConsolidationHeadroomMultiplier: 2.0,
		TrendMinDays:                        3,
	}
}

// DefaultGPUThresholdSettings returns compiled defaults for GPU recommendations.
func DefaultGPUThresholdSettings() GPUThresholdSettings {
	return GPUThresholdSettings{
		GPUThresholds:                 DefaultGPUThresholds(),
		ComputeBoundDRAMThreshold:       0.30,
		MIGFBPercentile:                 0.98,
		ConfidenceDaysTier1:             3,
		ConfidenceDaysTier2:             7,
		ConfidenceDaysTier3:             14,
		SpikeRatioThreshold:             5.0,
		SpikeConfidencePenalty:          0.70,
		NoProfilingConfidenceFactor:     0.50,
		TimeslicingMajorityThreshold:    0.50,
		TimeslicingMinReplicas:          2,
		TimeslicingMaxReplicas:          8,
		TimeslicingBasePenalty:          0.70,
		TimeslicingImpactedWeight:         0.30,
		NodeFreshnessDays:               7,
	}
}

// DefaultPVCThresholdSettings returns compiled defaults for PVC recommendations.
func DefaultPVCThresholdSettings() PVCThresholdSettings {
	return PVCThresholdSettings{
		OversizedThreshold:        0.20,
		NearFullThreshold:         0.85,
		MinTrendDays:              2,
		RecommendedSizeMultiplier: 2,
		MinRecommendedGiB:         1,
		DaysToFullAlert:           30,
	}
}

// NotificationThresholds holds notification evaluation thresholds derived from sizing settings.
type NotificationThresholds struct {
	MemTrendSlopeThreshold float64
	LowConfidenceThreshold float32
}

// NotificationThresholdsFromSizing extracts notification thresholds from sizing settings.
func NotificationThresholdsFromSizing(th SizingThresholdSettings) NotificationThresholds {
	return NotificationThresholds{
		MemTrendSlopeThreshold: th.MemTrendSlopeThreshold,
		LowConfidenceThreshold: th.LowConfidenceThreshold,
	}
}

var (
	defaultContainerSizingThresholds = DefaultContainerSizingThresholds()
	defaultNamespaceSizingThresholds = DefaultNamespaceSizingThresholds()
	defaultNodeThresholdSettings     = DefaultNodeThresholdSettings()
	defaultGPUThresholdSettings      = DefaultGPUThresholdSettings()
	defaultPVCThresholdSettings    = DefaultPVCThresholdSettings()
)

// InitThresholdDefaults copies admin env configuration into process-wide engine defaults.
// Call once after config load (alongside InitGPUEngine).
func InitThresholdDefaults(cfg *config.Config) {
	if cfg == nil {
		return
	}
	defaultContainerSizingThresholds = applyContainerEnvLocks(DefaultContainerSizingThresholds(), cfg)
	defaultNamespaceSizingThresholds = applyNamespaceEnvLocks(DefaultNamespaceSizingThresholds(), cfg)
	defaultNodeThresholdSettings = applyNodeEnvLocks(DefaultNodeThresholdSettings(), cfg)
	defaultGPUThresholdSettings = applyGPUEnvLocks(DefaultGPUThresholdSettings(), cfg)
	defaultPVCThresholdSettings = applyPVCEnvLocks(DefaultPVCThresholdSettings(), cfg)
}

func applyContainerEnvLocks(base SizingThresholdSettings, cfg *config.Config) SizingThresholdSettings {
	if _, ok := os.LookupEnv("ROS_CONTAINER_CPU_COST_PERCENTILE"); ok {
		base.CPUCostPercentile = cfg.ContainerCPUCostPercentile
	}
	if _, ok := os.LookupEnv("ROS_CONTAINER_CPU_PERF_PERCENTILE"); ok {
		base.CPUPerfPercentile = cfg.ContainerCPUPerfPercentile
	}
	if _, ok := os.LookupEnv("ROS_CONTAINER_MEM_COST_PERCENTILE"); ok {
		base.MemCostPercentile = cfg.ContainerMemCostPercentile
	}
	if _, ok := os.LookupEnv("ROS_CONTAINER_MEM_PERF_PERCENTILE"); ok {
		base.MemPerfPercentile = cfg.ContainerMemPerfPercentile
	}
	if _, ok := os.LookupEnv("ROS_CONTAINER_MIN_MARGIN"); ok {
		base.MinMargin = cfg.ContainerMinMargin
	}
	if _, ok := os.LookupEnv("ROS_CONTAINER_MAX_MARGIN"); ok {
		base.MaxMargin = cfg.ContainerMaxMargin
	}
	if _, ok := os.LookupEnv("ROS_CONTAINER_LIMIT_MULTIPLIER"); ok {
		base.LimitMultiplier = cfg.ContainerLimitMultiplier
	}
	if _, ok := os.LookupEnv("ROS_CONTAINER_CPU_FLOOR_MC"); ok {
		base.CPUFloorMC = cfg.ContainerCPUFloorMC
	}
	if _, ok := os.LookupEnv("ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC"); ok {
		base.IdleCPUThresholdMC = cfg.ContainerIdleCPUThresholdMC
	}
	if _, ok := os.LookupEnv("ROS_CONTAINER_IDLE_MEM_THRESHOLD_KIB"); ok {
		base.IdleMemThresholdKiB = cfg.ContainerIdleMemThresholdKiB
	}
	if _, ok := os.LookupEnv("ROS_CONTAINER_MEM_TREND_SLOPE_THRESHOLD"); ok {
		base.MemTrendSlopeThreshold = cfg.ContainerMemTrendSlopeThreshold
	}
	if _, ok := os.LookupEnv("ROS_CONTAINER_LOW_CONFIDENCE_THRESHOLD"); ok {
		base.LowConfidenceThreshold = cfg.ContainerLowConfidenceThreshold
	}
	return base
}

func applyNamespaceEnvLocks(base SizingThresholdSettings, cfg *config.Config) SizingThresholdSettings {
	if _, ok := os.LookupEnv("ROS_NAMESPACE_CPU_COST_PERCENTILE"); ok {
		base.CPUCostPercentile = cfg.NamespaceCPUCostPercentile
	}
	if _, ok := os.LookupEnv("ROS_NAMESPACE_CPU_PERF_PERCENTILE"); ok {
		base.CPUPerfPercentile = cfg.NamespaceCPUPerfPercentile
	}
	if _, ok := os.LookupEnv("ROS_NAMESPACE_MEM_COST_PERCENTILE"); ok {
		base.MemCostPercentile = cfg.NamespaceMemCostPercentile
	}
	if _, ok := os.LookupEnv("ROS_NAMESPACE_MEM_PERF_PERCENTILE"); ok {
		base.MemPerfPercentile = cfg.NamespaceMemPerfPercentile
	}
	if _, ok := os.LookupEnv("ROS_NAMESPACE_MIN_MARGIN"); ok {
		base.MinMargin = cfg.NamespaceMinMargin
	}
	if _, ok := os.LookupEnv("ROS_NAMESPACE_MAX_MARGIN"); ok {
		base.MaxMargin = cfg.NamespaceMaxMargin
	}
	if _, ok := os.LookupEnv("ROS_NAMESPACE_LIMIT_MULTIPLIER"); ok {
		base.LimitMultiplier = cfg.NamespaceLimitMultiplier
	}
	if _, ok := os.LookupEnv("ROS_NAMESPACE_CPU_FLOOR_MC"); ok {
		base.CPUFloorMC = cfg.NamespaceCPUFloorMC
	}
	if _, ok := os.LookupEnv("ROS_NAMESPACE_IDLE_CPU_THRESHOLD_MC"); ok {
		base.IdleCPUThresholdMC = cfg.NamespaceIdleCPUThresholdMC
	}
	if _, ok := os.LookupEnv("ROS_NAMESPACE_IDLE_MEM_THRESHOLD_KIB"); ok {
		base.IdleMemThresholdKiB = cfg.NamespaceIdleMemThresholdKiB
	}
	if _, ok := os.LookupEnv("ROS_NAMESPACE_MEM_TREND_SLOPE_THRESHOLD"); ok {
		base.MemTrendSlopeThreshold = cfg.NamespaceMemTrendSlopeThreshold
	}
	if _, ok := os.LookupEnv("ROS_NAMESPACE_LOW_CONFIDENCE_THRESHOLD"); ok {
		base.LowConfidenceThreshold = cfg.NamespaceLowConfidenceThreshold
	}
	return base
}

func applyNodeEnvLocks(base NodeThresholdSettings, cfg *config.Config) NodeThresholdSettings {
	if _, ok := os.LookupEnv("ROS_NODE_UNDERUTIL_THRESHOLD"); ok {
		base.UnderutilThreshold = cfg.NodeUnderutilThreshold
	}
	if _, ok := os.LookupEnv("ROS_NODE_OVERCOMMIT_THRESHOLD"); ok {
		base.OvercommitThreshold = cfg.NodeOvercommitThreshold
	}
	if _, ok := os.LookupEnv("ROS_NODE_ALLOCATABLE_FACTOR"); ok {
		base.AllocatableFactor = cfg.NodeAllocatableFactor
	}
	if _, ok := os.LookupEnv("ROS_NODE_STRANDED_IMBALANCE_THRESHOLD"); ok {
		base.StrandedImbalanceThreshold = cfg.NodeStrandedImbalanceThreshold
	}
	if _, ok := os.LookupEnv("ROS_NODE_EMA_ALPHA"); ok {
		base.EMAAlpha = cfg.NodeEMAAlpha
	}
	if _, ok := os.LookupEnv("ROS_NODE_COST_TARGET_UTILIZATION"); ok {
		base.CostTargetUtilization = cfg.NodeCostTargetUtilization
	}
	if _, ok := os.LookupEnv("ROS_NODE_PERF_TARGET_UTILIZATION"); ok {
		base.PerfTargetUtilization = cfg.NodePerfTargetUtilization
	}
	if _, ok := os.LookupEnv("ROS_NODE_PERF_CONSOLIDATION_HEADROOM_MULTIPLIER"); ok {
		base.PerfConsolidationHeadroomMultiplier = cfg.NodePerfConsolidationHeadroomMultiplier
	}
	if _, ok := os.LookupEnv("ROS_NODE_TREND_MIN_DAYS"); ok {
		base.TrendMinDays = cfg.NodeTrendMinDays
	}
	return base
}

func applyGPUEnvLocks(base GPUThresholdSettings, cfg *config.Config) GPUThresholdSettings {
	if _, ok := os.LookupEnv("ROS_GPU_IDLE_THRESHOLD"); ok {
		base.IdleThreshold = cfg.GPUIdleThreshold
	}
	if _, ok := os.LookupEnv("ROS_GPU_UNDERUTILIZED_SM_THRESHOLD"); ok {
		base.UnderutilizedSM = cfg.GPUUnderutilizedSMThreshold
	}
	if _, ok := os.LookupEnv("ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD"); ok {
		base.UnderutilizedTensor = cfg.GPUUnderutilizedTensorThreshold
	}
	if _, ok := os.LookupEnv("ROS_GPU_MEMBOUND_DRAM_THRESHOLD"); ok {
		base.MemBoundDRAM = cfg.GPUMemBoundDRAMThreshold
	}
	if _, ok := os.LookupEnv("ROS_GPU_MEMBOUND_TENSOR_THRESHOLD"); ok {
		base.MemBoundTensor = cfg.GPUMemBoundTensorThreshold
	}
	if _, ok := os.LookupEnv("ROS_GPU_FB_HEADROOM_FACTOR"); ok {
		base.FBHeadroomFactor = cfg.GPUFBHeadroomFactor
	}
	if _, ok := os.LookupEnv("ROS_GPU_COMPUTE_BOUND_DRAM_THRESHOLD"); ok {
		base.ComputeBoundDRAMThreshold = cfg.GPUComputeBoundDRAMThreshold
	}
	if _, ok := os.LookupEnv("ROS_GPU_MIG_FB_PERCENTILE"); ok {
		base.MIGFBPercentile = cfg.GPUMIGFBPercentile
	}
	if _, ok := os.LookupEnv("ROS_GPU_CONFIDENCE_DAYS_TIER1"); ok {
		base.ConfidenceDaysTier1 = cfg.GPUConfidenceDaysTier1
	}
	if _, ok := os.LookupEnv("ROS_GPU_CONFIDENCE_DAYS_TIER2"); ok {
		base.ConfidenceDaysTier2 = cfg.GPUConfidenceDaysTier2
	}
	if _, ok := os.LookupEnv("ROS_GPU_CONFIDENCE_DAYS_TIER3"); ok {
		base.ConfidenceDaysTier3 = cfg.GPUConfidenceDaysTier3
	}
	if _, ok := os.LookupEnv("ROS_GPU_SPIKE_RATIO_THRESHOLD"); ok {
		base.SpikeRatioThreshold = cfg.GPUSpikeRatioThreshold
	}
	if _, ok := os.LookupEnv("ROS_GPU_SPIKE_CONFIDENCE_PENALTY"); ok {
		base.SpikeConfidencePenalty = cfg.GPUSpikeConfidencePenalty
	}
	if _, ok := os.LookupEnv("ROS_GPU_NO_PROFILING_CONFIDENCE_FACTOR"); ok {
		base.NoProfilingConfidenceFactor = cfg.GPUNoProfilingConfidenceFactor
	}
	if _, ok := os.LookupEnv("ROS_GPU_TIMESLICING_MAJORITY_THRESHOLD"); ok {
		base.TimeslicingMajorityThreshold = cfg.GPUTimeslicingMajorityThreshold
	}
	if _, ok := os.LookupEnv("ROS_GPU_TIMESLICING_MIN_REPLICAS"); ok {
		base.TimeslicingMinReplicas = cfg.GPUTimeslicingMinReplicas
	}
	if _, ok := os.LookupEnv("ROS_GPU_TIMESLICING_MAX_REPLICAS"); ok {
		base.TimeslicingMaxReplicas = cfg.GPUTimeslicingMaxReplicas
	}
	if _, ok := os.LookupEnv("ROS_GPU_TIMESLICING_BASE_PENALTY"); ok {
		base.TimeslicingBasePenalty = cfg.GPUTimeslicingBasePenalty
	}
	if _, ok := os.LookupEnv("ROS_GPU_TIMESLICING_IMPACTED_WEIGHT"); ok {
		base.TimeslicingImpactedWeight = cfg.GPUTimeslicingImpactedWeight
	}
	if _, ok := os.LookupEnv("ROS_GPU_NODE_FRESHNESS_DAYS"); ok {
		base.NodeFreshnessDays = cfg.GPUNodeFreshnessDays
	}
	return base
}

func applyPVCEnvLocks(base PVCThresholdSettings, cfg *config.Config) PVCThresholdSettings {
	if _, ok := os.LookupEnv("ROS_PVC_OVERSIZED_THRESHOLD"); ok {
		base.OversizedThreshold = cfg.PVCOversizedThreshold
	}
	if _, ok := os.LookupEnv("ROS_PVC_NEAR_FULL_THRESHOLD"); ok {
		base.NearFullThreshold = cfg.PVCNearFullThreshold
	}
	if _, ok := os.LookupEnv("ROS_PVC_MIN_TREND_DAYS"); ok {
		base.MinTrendDays = cfg.PVCMinTrendDays
	}
	if _, ok := os.LookupEnv("ROS_PVC_RECOMMENDED_SIZE_MULTIPLIER"); ok {
		base.RecommendedSizeMultiplier = cfg.PVCRecommendedSizeMultiplier
	}
	if _, ok := os.LookupEnv("ROS_PVC_MIN_RECOMMENDED_GIB"); ok {
		base.MinRecommendedGiB = cfg.PVCMinRecommendedGiB
	}
	if _, ok := os.LookupEnv("ROS_PVC_DAYS_TO_FULL_ALERT"); ok {
		base.DaysToFullAlert = cfg.PVCDaysToFullAlert
	}
	return base
}

// ResolveContainerSizingThresholds resolves container sizing thresholds for an org.
func ResolveContainerSizingThresholds(ctx context.Context, pool *pgxpool.Pool, orgID string) (SizingThresholdSettings, error) {
	return resolveThresholdCached(ctx, pool, orgID, "container", resolveContainerSizingThresholdsUncached)
}

// ResolveNamespaceSizingThresholds resolves namespace sizing thresholds for an org.
func ResolveNamespaceSizingThresholds(ctx context.Context, pool *pgxpool.Pool, orgID string) (SizingThresholdSettings, error) {
	return resolveThresholdCached(ctx, pool, orgID, "namespace", resolveNamespaceSizingThresholdsUncached)
}

// ResolveNodeThresholdSettings resolves node thresholds for an org.
func ResolveNodeThresholdSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) (NodeThresholdSettings, error) {
	return resolveThresholdCached(ctx, pool, orgID, "node", resolveNodeThresholdSettingsUncached)
}

// ResolveGPUThresholdSettings resolves GPU thresholds for an org.
func ResolveGPUThresholdSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) (GPUThresholdSettings, error) {
	return resolveThresholdCached(ctx, pool, orgID, "gpu", resolveGPUThresholdSettingsUncached)
}

// ResolvePVCThresholdSettings resolves PVC thresholds for an org.
func ResolvePVCThresholdSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) (PVCThresholdSettings, error) {
	return resolveThresholdCached(ctx, pool, orgID, "pvc", resolvePVCThresholdSettingsUncached)
}

func resolveContainerSizingThresholdsUncached(ctx context.Context, pool *pgxpool.Pool, orgID string) (SizingThresholdSettings, error) {
	return resolveSizingThresholds(ctx, pool, orgID, "container", DefaultContainerSizingThresholds(), containerEnvLockMap())
}

func resolveNamespaceSizingThresholdsUncached(ctx context.Context, pool *pgxpool.Pool, orgID string) (SizingThresholdSettings, error) {
	return resolveSizingThresholds(ctx, pool, orgID, "namespace", DefaultNamespaceSizingThresholds(), namespaceEnvLockMap())
}

func resolveNodeThresholdSettingsUncached(ctx context.Context, pool *pgxpool.Pool, orgID string) (NodeThresholdSettings, error) {
	result := DefaultNodeThresholdSettings()
	if err := overlayThresholdJSON(ctx, pool, orgID, "node", &result); err != nil {
		return result, err
	}
	cfg := config.GetConfig()
	result = applyNodeEnvLocks(result, cfg)
	return result, nil
}

func resolveGPUThresholdSettingsUncached(ctx context.Context, pool *pgxpool.Pool, orgID string) (GPUThresholdSettings, error) {
	result := DefaultGPUThresholdSettings()
	if err := overlayThresholdJSON(ctx, pool, orgID, "gpu", &result); err != nil {
		return result, err
	}
	cfg := config.GetConfig()
	result = applyGPUEnvLocks(result, cfg)
	return result, nil
}

func resolvePVCThresholdSettingsUncached(ctx context.Context, pool *pgxpool.Pool, orgID string) (PVCThresholdSettings, error) {
	result := DefaultPVCThresholdSettings()
	if err := overlayThresholdJSON(ctx, pool, orgID, "pvc", &result); err != nil {
		return result, err
	}
	cfg := config.GetConfig()
	result = applyPVCEnvLocks(result, cfg)
	return result, nil
}

func resolveSizingThresholds(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, recType string,
	defaults SizingThresholdSettings,
	lockMap map[string]string,
) (SizingThresholdSettings, error) {
	result := defaults
	if err := overlayThresholdJSON(ctx, pool, orgID, recType, &result); err != nil {
		return result, err
	}
	cfg := config.GetConfig()
	switch recType {
	case "container":
		result = applyContainerEnvLocks(result, cfg)
	case "namespace":
		result = applyNamespaceEnvLocks(result, cfg)
	}
	return result, nil
}

func overlayThresholdJSON(ctx context.Context, pool *pgxpool.Pool, orgID, recType string, dest any) error {
	var raw []byte
	err := pool.QueryRow(ctx, `
		SELECT thresholds FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, recType,
	).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query recommendation thresholds: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("decode recommendation thresholds: %w", err)
	}
	return nil
}

func containerEnvLockMap() map[string]string {
	return map[string]string{
		"ROS_CONTAINER_CPU_COST_PERCENTILE":         "cpu_cost_percentile",
		"ROS_CONTAINER_CPU_PERF_PERCENTILE":         "cpu_perf_percentile",
		"ROS_CONTAINER_MEM_COST_PERCENTILE":         "mem_cost_percentile",
		"ROS_CONTAINER_MEM_PERF_PERCENTILE":         "mem_perf_percentile",
		"ROS_CONTAINER_MIN_MARGIN":                  "min_margin",
		"ROS_CONTAINER_MAX_MARGIN":                  "max_margin",
		"ROS_CONTAINER_LIMIT_MULTIPLIER":            "limit_multiplier",
		"ROS_CONTAINER_CPU_FLOOR_MC":                "cpu_floor_mc",
		"ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC":       "idle_cpu_threshold_mc",
		"ROS_CONTAINER_IDLE_MEM_THRESHOLD_KIB":      "idle_mem_threshold_kib",
		"ROS_CONTAINER_MEM_TREND_SLOPE_THRESHOLD":   "mem_trend_slope_threshold",
		"ROS_CONTAINER_LOW_CONFIDENCE_THRESHOLD":    "low_confidence_threshold",
	}
}

func namespaceEnvLockMap() map[string]string {
	return map[string]string{
		"ROS_NAMESPACE_CPU_COST_PERCENTILE":         "cpu_cost_percentile",
		"ROS_NAMESPACE_CPU_PERF_PERCENTILE":         "cpu_perf_percentile",
		"ROS_NAMESPACE_MEM_COST_PERCENTILE":         "mem_cost_percentile",
		"ROS_NAMESPACE_MEM_PERF_PERCENTILE":         "mem_perf_percentile",
		"ROS_NAMESPACE_MIN_MARGIN":                  "min_margin",
		"ROS_NAMESPACE_MAX_MARGIN":                  "max_margin",
		"ROS_NAMESPACE_LIMIT_MULTIPLIER":            "limit_multiplier",
		"ROS_NAMESPACE_CPU_FLOOR_MC":                "cpu_floor_mc",
		"ROS_NAMESPACE_IDLE_CPU_THRESHOLD_MC":       "idle_cpu_threshold_mc",
		"ROS_NAMESPACE_IDLE_MEM_THRESHOLD_KIB":      "idle_mem_threshold_kib",
		"ROS_NAMESPACE_MEM_TREND_SLOPE_THRESHOLD":   "mem_trend_slope_threshold",
		"ROS_NAMESPACE_LOW_CONFIDENCE_THRESHOLD":    "low_confidence_threshold",
	}
}

func nodeEnvLockMap() map[string]string {
	return map[string]string{
		"ROS_NODE_UNDERUTIL_THRESHOLD":                      "underutil_threshold",
		"ROS_NODE_OVERCOMMIT_THRESHOLD":                     "overcommit_threshold",
		"ROS_NODE_ALLOCATABLE_FACTOR":                      "allocatable_factor",
		"ROS_NODE_STRANDED_IMBALANCE_THRESHOLD":             "stranded_imbalance_threshold",
		"ROS_NODE_EMA_ALPHA":                                "ema_alpha",
		"ROS_NODE_COST_TARGET_UTILIZATION":                  "cost_target_utilization",
		"ROS_NODE_PERF_TARGET_UTILIZATION":                  "perf_target_utilization",
		"ROS_NODE_PERF_CONSOLIDATION_HEADROOM_MULTIPLIER":   "perf_consolidation_headroom_multiplier",
		"ROS_NODE_TREND_MIN_DAYS":                           "trend_min_days",
	}
}

func gpuEnvLockMap() map[string]string {
	return map[string]string{
		"ROS_GPU_IDLE_THRESHOLD":                      "idle_threshold",
		"ROS_GPU_UNDERUTILIZED_SM_THRESHOLD":          "underutilized_sm_threshold",
		"ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD":      "underutilized_tensor_threshold",
		"ROS_GPU_MEMBOUND_DRAM_THRESHOLD":             "membound_dram_threshold",
		"ROS_GPU_MEMBOUND_TENSOR_THRESHOLD":           "membound_tensor_threshold",
		"ROS_GPU_FB_HEADROOM_FACTOR":                  "fb_headroom_factor",
		"ROS_GPU_COMPUTE_BOUND_DRAM_THRESHOLD":        "compute_bound_dram_threshold",
		"ROS_GPU_MIG_FB_PERCENTILE":                   "mig_fb_percentile",
		"ROS_GPU_CONFIDENCE_DAYS_TIER1":               "confidence_days_tier1",
		"ROS_GPU_CONFIDENCE_DAYS_TIER2":               "confidence_days_tier2",
		"ROS_GPU_CONFIDENCE_DAYS_TIER3":               "confidence_days_tier3",
		"ROS_GPU_SPIKE_RATIO_THRESHOLD":               "spike_ratio_threshold",
		"ROS_GPU_SPIKE_CONFIDENCE_PENALTY":            "spike_confidence_penalty",
		"ROS_GPU_NO_PROFILING_CONFIDENCE_FACTOR":      "no_profiling_confidence_factor",
		"ROS_GPU_TIMESLICING_MAJORITY_THRESHOLD":      "timeslicing_majority_threshold",
		"ROS_GPU_TIMESLICING_MIN_REPLICAS":            "timeslicing_min_replicas",
		"ROS_GPU_TIMESLICING_MAX_REPLICAS":            "timeslicing_max_replicas",
		"ROS_GPU_TIMESLICING_BASE_PENALTY":            "timeslicing_base_penalty",
		"ROS_GPU_TIMESLICING_IMPACTED_WEIGHT":         "timeslicing_impacted_weight",
		"ROS_GPU_NODE_FRESHNESS_DAYS":                 "node_freshness_days",
	}
}

func pvcEnvLockMap() map[string]string {
	return map[string]string{
		"ROS_PVC_OVERSIZED_THRESHOLD":           "oversized_threshold",
		"ROS_PVC_NEAR_FULL_THRESHOLD":           "near_full_threshold",
		"ROS_PVC_MIN_TREND_DAYS":                "min_trend_days",
		"ROS_PVC_RECOMMENDED_SIZE_MULTIPLIER":   "recommended_size_multiplier",
		"ROS_PVC_MIN_RECOMMENDED_GIB":           "min_recommended_gib",
		"ROS_PVC_DAYS_TO_FULL_ALERT":            "days_to_full_alert",
	}
}

func lockedFieldsFromEnvMap(lockMap map[string]string) []string {
	var locked []string
	for envKey, fieldName := range lockMap {
		if _, ok := os.LookupEnv(envKey); ok {
			locked = append(locked, fieldName)
		}
	}
	return locked
}

func isFieldLockedInMap(lockMap map[string]string, field string) bool {
	for envKey, fieldName := range lockMap {
		if fieldName == field {
			if _, ok := os.LookupEnv(envKey); ok {
				return true
			}
		}
	}
	return false
}

// GetThresholdSettingsForAPI returns merged threshold settings with locked_fields.
func GetThresholdSettingsForAPI(ctx context.Context, pool *pgxpool.Pool, orgID, recType string) (any, error) {
	if _, ok := validThresholdRecommendationTypes[recType]; !ok {
		return nil, fmt.Errorf("unsupported recommendation_type %q", recType)
	}
	switch recType {
	case "container":
		settings, err := ResolveContainerSizingThresholds(ctx, pool, orgID)
		if err != nil {
			return nil, err
		}
		return SizingThresholdSettingsResponse{
			SizingThresholdSettings: settings,
			LockedFields:            lockedFieldsFromEnvMap(containerEnvLockMap()),
		}, nil
	case "namespace":
		settings, err := ResolveNamespaceSizingThresholds(ctx, pool, orgID)
		if err != nil {
			return nil, err
		}
		return SizingThresholdSettingsResponse{
			SizingThresholdSettings: settings,
			LockedFields:            lockedFieldsFromEnvMap(namespaceEnvLockMap()),
		}, nil
	case "node":
		settings, err := ResolveNodeThresholdSettings(ctx, pool, orgID)
		if err != nil {
			return nil, err
		}
		return NodeThresholdSettingsResponse{
			NodeThresholdSettings: settings,
			LockedFields:          lockedFieldsFromEnvMap(nodeEnvLockMap()),
		}, nil
	case "gpu":
		settings, err := ResolveGPUThresholdSettings(ctx, pool, orgID)
		if err != nil {
			return nil, err
		}
		return GPUThresholdSettingsResponse{
			GPUThresholdSettings: settings,
			LockedFields:         lockedFieldsFromEnvMap(gpuEnvLockMap()),
		}, nil
	case "pvc":
		settings, err := ResolvePVCThresholdSettings(ctx, pool, orgID)
		if err != nil {
			return nil, err
		}
		return PVCThresholdSettingsResponse{
			PVCThresholdSettings: settings,
			LockedFields:         lockedFieldsFromEnvMap(pvcEnvLockMap()),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported recommendation_type %q", recType)
	}
}

// UpdateThresholdSettings applies a partial tenant update for the given recommendation type.
func UpdateThresholdSettings(ctx context.Context, pool *pgxpool.Pool, orgID, recType string, rawUpdate json.RawMessage) error {
	if _, ok := validThresholdRecommendationTypes[recType]; !ok {
		return fmt.Errorf("unsupported recommendation_type %q", recType)
	}

	if err := ValidateThresholdSettingsUpdate(ctx, pool, orgID, recType, rawUpdate); err != nil {
		return err
	}

	var lockedAttempts []string
	switch recType {
	case "container":
		var update SizingThresholdSettingsUpdate
		if err := json.Unmarshal(rawUpdate, &update); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		lockedAttempts = lockedSizingFieldsInUpdate(update, containerEnvLockMap())
	case "namespace":
		var update SizingThresholdSettingsUpdate
		if err := json.Unmarshal(rawUpdate, &update); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		lockedAttempts = lockedSizingFieldsInUpdate(update, namespaceEnvLockMap())
	case "node":
		var update NodeThresholdSettingsUpdate
		if err := json.Unmarshal(rawUpdate, &update); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		lockedAttempts = lockedNodeFieldsInUpdate(update)
	case "gpu":
		var update GPUThresholdSettingsUpdate
		if err := json.Unmarshal(rawUpdate, &update); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		lockedAttempts = lockedGPUFieldsInUpdate(update)
	case "pvc":
		var update PVCThresholdSettingsUpdate
		if err := json.Unmarshal(rawUpdate, &update); err != nil {
			return fmt.Errorf("invalid request body: %w", err)
		}
		lockedAttempts = lockedPVCFieldsInUpdate(update)
	}
	if len(lockedAttempts) > 0 {
		return fmt.Errorf("%w: %v", ErrFieldsLocked, lockedAttempts)
	}

	overrides, err := loadThresholdOverrides(ctx, pool, orgID, recType)
	if err != nil {
		return err
	}
	if overrides == nil {
		overrides = map[string]json.RawMessage{}
	}
	if err := mergeRawUpdateIntoOverrides(overrides, rawUpdate); err != nil {
		return err
	}
	if err := upsertThresholdOverrides(ctx, pool, orgID, recType, overrides); err != nil {
		return err
	}
	InvalidateThresholdCache(orgID, recType)
	return nil
}

// DeleteThresholdSettings removes tenant overrides for the given recommendation type.
func DeleteThresholdSettings(ctx context.Context, pool *pgxpool.Pool, orgID, recType string) error {
	if _, ok := validThresholdRecommendationTypes[recType]; !ok {
		return fmt.Errorf("unsupported recommendation_type %q", recType)
	}
	_, err := pool.Exec(ctx, `
		DELETE FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, recType)
	if err != nil {
		return fmt.Errorf("delete recommendation thresholds: %w", err)
	}
	InvalidateThresholdCache(orgID, recType)
	return nil
}

func loadThresholdOverrides(ctx context.Context, pool *pgxpool.Pool, orgID, recType string) (map[string]json.RawMessage, error) {
	var raw []byte
	err := pool.QueryRow(ctx, `
		SELECT thresholds FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, recType,
	).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query recommendation thresholds: %w", err)
	}
	overrides := map[string]json.RawMessage{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &overrides); err != nil {
			return nil, fmt.Errorf("decode recommendation threshold overrides: %w", err)
		}
	}
	return overrides, nil
}

func mergeRawUpdateIntoOverrides(overrides map[string]json.RawMessage, rawUpdate json.RawMessage) error {
	var update map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &update); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	for key, val := range update {
		if key == "locked_fields" {
			continue
		}
		overrides[key] = val
	}
	return nil
}

func upsertThresholdOverrides(ctx context.Context, pool *pgxpool.Pool, orgID, recType string, overrides map[string]json.RawMessage) error {
	payload, err := json.Marshal(overrides)
	if err != nil {
		return fmt.Errorf("encode recommendation threshold overrides: %w", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (org_id, recommendation_type)
		DO UPDATE SET thresholds = EXCLUDED.thresholds, updated_at = NOW()`,
		orgID, recType, payload)
	if err != nil {
		return fmt.Errorf("upsert recommendation thresholds: %w", err)
	}
	return nil
}

func lockedSizingFieldsInUpdate(update SizingThresholdSettingsUpdate, lockMap map[string]string) []string {
	var locked []string
	check := func(field string, set bool) {
		if set && isFieldLockedInMap(lockMap, field) {
			locked = append(locked, field)
		}
	}
	check("cpu_cost_percentile", update.CPUCostPercentile != nil)
	check("cpu_perf_percentile", update.CPUPerfPercentile != nil)
	check("mem_cost_percentile", update.MemCostPercentile != nil)
	check("mem_perf_percentile", update.MemPerfPercentile != nil)
	check("min_margin", update.MinMargin != nil)
	check("max_margin", update.MaxMargin != nil)
	check("limit_multiplier", update.LimitMultiplier != nil)
	check("cpu_floor_mc", update.CPUFloorMC != nil)
	check("idle_cpu_threshold_mc", update.IdleCPUThresholdMC != nil)
	check("idle_mem_threshold_kib", update.IdleMemThresholdKiB != nil)
	check("mem_trend_slope_threshold", update.MemTrendSlopeThreshold != nil)
	check("low_confidence_threshold", update.LowConfidenceThreshold != nil)
	return locked
}

func lockedNodeFieldsInUpdate(update NodeThresholdSettingsUpdate) []string {
	lockMap := nodeEnvLockMap()
	var locked []string
	check := func(field string, set bool) {
		if set && isFieldLockedInMap(lockMap, field) {
			locked = append(locked, field)
		}
	}
	check("underutil_threshold", update.UnderutilThreshold != nil)
	check("overcommit_threshold", update.OvercommitThreshold != nil)
	check("allocatable_factor", update.AllocatableFactor != nil)
	check("stranded_imbalance_threshold", update.StrandedImbalanceThreshold != nil)
	check("ema_alpha", update.EMAAlpha != nil)
	check("cost_target_utilization", update.CostTargetUtilization != nil)
	check("perf_target_utilization", update.PerfTargetUtilization != nil)
	check("perf_consolidation_headroom_multiplier", update.PerfConsolidationHeadroomMultiplier != nil)
	check("trend_min_days", update.TrendMinDays != nil)
	return locked
}

func lockedGPUFieldsInUpdate(update GPUThresholdSettingsUpdate) []string {
	lockMap := gpuEnvLockMap()
	var locked []string
	check := func(field string, set bool) {
		if set && isFieldLockedInMap(lockMap, field) {
			locked = append(locked, field)
		}
	}
	check("idle_threshold", update.IdleThreshold != nil)
	check("underutilized_sm_threshold", update.UnderutilizedSMThreshold != nil)
	check("underutilized_tensor_threshold", update.UnderutilizedTensorThreshold != nil)
	check("membound_dram_threshold", update.MemBoundDRAMThreshold != nil)
	check("membound_tensor_threshold", update.MemBoundTensorThreshold != nil)
	check("fb_headroom_factor", update.FBHeadroomFactor != nil)
	check("compute_bound_dram_threshold", update.ComputeBoundDRAMThreshold != nil)
	check("mig_fb_percentile", update.MIGFBPercentile != nil)
	check("confidence_days_tier1", update.ConfidenceDaysTier1 != nil)
	check("confidence_days_tier2", update.ConfidenceDaysTier2 != nil)
	check("confidence_days_tier3", update.ConfidenceDaysTier3 != nil)
	check("spike_ratio_threshold", update.SpikeRatioThreshold != nil)
	check("spike_confidence_penalty", update.SpikeConfidencePenalty != nil)
	check("no_profiling_confidence_factor", update.NoProfilingConfidenceFactor != nil)
	check("timeslicing_majority_threshold", update.TimeslicingMajorityThreshold != nil)
	check("timeslicing_min_replicas", update.TimeslicingMinReplicas != nil)
	check("timeslicing_max_replicas", update.TimeslicingMaxReplicas != nil)
	check("timeslicing_base_penalty", update.TimeslicingBasePenalty != nil)
	check("timeslicing_impacted_weight", update.TimeslicingImpactedWeight != nil)
	check("node_freshness_days", update.NodeFreshnessDays != nil)
	return locked
}

func lockedPVCFieldsInUpdate(update PVCThresholdSettingsUpdate) []string {
	lockMap := pvcEnvLockMap()
	var locked []string
	check := func(field string, set bool) {
		if set && isFieldLockedInMap(lockMap, field) {
			locked = append(locked, field)
		}
	}
	check("oversized_threshold", update.OversizedThreshold != nil)
	check("near_full_threshold", update.NearFullThreshold != nil)
	check("min_trend_days", update.MinTrendDays != nil)
	check("recommended_size_multiplier", update.RecommendedSizeMultiplier != nil)
	check("min_recommended_gib", update.MinRecommendedGiB != nil)
	check("days_to_full_alert", update.DaysToFullAlert != nil)
	return locked
}

// NodeRecConfigFromThresholds converts resolved node threshold settings to NodeRecConfig.
func NodeRecConfigFromThresholds(th NodeThresholdSettings) NodeRecConfig {
	return NodeRecConfig{
		UnderutilThreshold:         th.UnderutilThreshold,
		OvercommitThreshold:        th.OvercommitThreshold,
		AllocatableFactor:          th.AllocatableFactor,
		StrandedImbalanceThreshold: th.StrandedImbalanceThreshold,
		EMAAlpha:                   th.EMAAlpha,
	}
}

// NodeEnginesFromThresholds builds per-engine target utilization from resolved settings.
func NodeEnginesFromThresholds(th NodeThresholdSettings) []NodeEngineConfig {
	return []NodeEngineConfig{
		{Name: "cost", TargetUtilization: th.CostTargetUtilization},
		{Name: "performance", TargetUtilization: th.PerfTargetUtilization},
	}
}

// SnapshotInventoryFreshHours returns the admin-configured recent-ingest window for snapshot inventory.
func SnapshotInventoryFreshHours() int {
	h := config.GetConfig().SnapshotInventoryFreshHours
	if h <= 0 {
		return 6
	}
	return h
}
