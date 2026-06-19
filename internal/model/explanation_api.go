package model

// ContainerExplanationAPI is the nested explanation object for container/namespace CPU/memory recommendations.
type ContainerExplanationAPI struct {
	ConfidenceLevel           *float32 `json:"confidence_level,omitempty"`
	DataDays                  *int     `json:"data_days,omitempty"`
	DecayHalfLifeHours        *float64 `json:"decay_half_life_hours,omitempty"`
	CPUCostPercentileMC       *int64   `json:"cpu_cost_percentile_millicores,omitempty"`
	CPUPerfPercentileMC       *int64   `json:"cpu_perf_percentile_millicores,omitempty"`
	CPUUsageP95MC             *int64   `json:"cpu_usage_p95_millicores,omitempty"`
	CPUUsageP50MC             *int64   `json:"cpu_usage_p50_millicores,omitempty"`
	CPUUsageMeanMC            *int64   `json:"cpu_usage_mean_millicores,omitempty"`
	CPUAdaptiveMarginBP       *int32   `json:"cpu_adaptive_margin_basis_points,omitempty"`
	CPUTrendSlope             *float64 `json:"cpu_trend_slope,omitempty"`
	MemCostPercentileKiB      *int64   `json:"mem_cost_percentile_kib,omitempty"`
	MemPerfPercentileKiB      *int64   `json:"mem_perf_percentile_kib,omitempty"`
	MemUsageP95KiB            *int64   `json:"mem_usage_p95_kib,omitempty"`
	MemUsageP50KiB            *int64   `json:"mem_usage_p50_kib,omitempty"`
	MemUsageMeanKiB           *int64   `json:"mem_usage_mean_kib,omitempty"`
	MemAdaptiveMarginBP       *int32   `json:"mem_adaptive_margin_basis_points,omitempty"`
	MemTrendSlope             *float64 `json:"mem_trend_slope,omitempty"`
	OOMCountSum               *int64   `json:"oom_count_sum,omitempty"`
	OOMBumpApplied            *bool    `json:"oom_bump_applied,omitempty"`
	CPUFloorApplied           *bool    `json:"cpu_floor_applied,omitempty"`
	MemFloorApplied           *bool    `json:"mem_floor_applied,omitempty"`
	IsIdle                    *bool    `json:"is_idle,omitempty"`
}

// GPUExplanationAPI is the nested explanation object for GPU recommendations.
type GPUExplanationAPI struct {
	SMActiveAvgBP      *int32  `json:"gpu_sm_active_avg_basis_points,omitempty"`
	TensorActiveAvgBP  *int32  `json:"gpu_tensor_active_avg_basis_points,omitempty"`
	DRAMActiveAvgBP    *int32  `json:"gpu_dram_active_avg_basis_points,omitempty"`
	FBUsageMaxMiB      *int32  `json:"gpu_fb_usage_max_mib,omitempty"`
	FBP98MiB           *int32  `json:"gpu_fb_p98_mib,omitempty"`
	RecommendedProfile *string `json:"gpu_recommended_profile,omitempty"`
	CurrentProfile     *string `json:"gpu_current_profile,omitempty"`
	HasProfilingData   *bool   `json:"gpu_has_profiling_data,omitempty"`
	MemoryBound        *bool   `json:"gpu_memory_bound,omitempty"`
}

// NodeExplanationAPI is the nested explanation object for node CPU/memory recommendations.
type NodeExplanationAPI struct {
	ConfidenceLevel              *float32 `json:"confidence_level,omitempty"`
	DataDays                     *int     `json:"data_days,omitempty"`
	TargetUtilizationBP          *int32   `json:"target_utilization_basis_points,omitempty"`
	CurrentCPUMC                 *int64   `json:"current_cpu_millicores,omitempty"`
	CurrentMemKiB                *int64   `json:"current_mem_kib,omitempty"`
	MaxCPUUsageP95MC             *int64   `json:"max_cpu_usage_p95_millicores,omitempty"`
	MaxMemUsageP95KiB            *int64   `json:"max_mem_usage_p95_kib,omitempty"`
	PodSchedulingHeadroomBP      *int32   `json:"pod_scheduling_headroom_basis_points,omitempty"`
	EMAImbalanceBP               *int32   `json:"ema_imbalance_basis_points,omitempty"`
	ConsolidationApplied         *bool    `json:"consolidation_applied,omitempty"`
	SizingFormula                *string  `json:"sizing_formula,omitempty"`
}

// PVCExplanationAPI is the nested explanation object for PVC/storage recommendations.
type PVCExplanationAPI struct {
	ConfidenceLevel           *float32 `json:"confidence_level,omitempty"`
	DataDays                  *int     `json:"data_days,omitempty"`
	UsageRatio                *float64 `json:"usage_ratio,omitempty"`
	GrowthBytesPerDay         *int64   `json:"growth_bytes_per_day,omitempty"`
	OversizedThresholdBP      *int32   `json:"oversized_threshold_basis_points,omitempty"`
	NearFullThresholdBP       *int32   `json:"near_full_threshold_basis_points,omitempty"`
	RecommendedSizeMultiplier *int32   `json:"recommended_size_multiplier,omitempty"`
	MinRecommendedGiB         *int32   `json:"min_recommended_gib,omitempty"`
	ClassificationReason      *string  `json:"classification_reason,omitempty"`
}

// QuotaExplanationAPI is the nested explanation object for namespace quota recommendations.
type QuotaExplanationAPI struct {
	HeadroomBP            *int32  `json:"headroom_basis_points,omitempty"`
	ContainerCPUSumMC     *int64  `json:"container_cpu_sum_millicores,omitempty"`
	ContainerMemSumBytes  *int64  `json:"container_mem_sum_bytes,omitempty"`
	SignalCCPUUsedMC      *int64  `json:"signal_c_cpu_used_millicores,omitempty"`
	MaxUtilizationBP      *int32  `json:"max_utilization_basis_points,omitempty"`
	RiskLevel             *string `json:"risk_level,omitempty"`
	RecommendationReason  *string `json:"recommendation_reason,omitempty"`
}

// ClusterQuotaExplanationAPI is the nested explanation object for cluster quota recommendations.
type ClusterQuotaExplanationAPI struct {
	HeadroomBP           *int32  `json:"headroom_basis_points,omitempty"`
	NSQuotaCPUSumMC      *int64  `json:"ns_quota_cpu_sum_millicores,omitempty"`
	NSQuotaMemSumBytes   *int64  `json:"ns_quota_mem_sum_bytes,omitempty"`
	BaseCPUMC            *int64  `json:"base_cpu_millicores,omitempty"`
	MaxUtilizationBP     *int32  `json:"max_utilization_basis_points,omitempty"`
	RecommendationReason *string `json:"recommendation_reason,omitempty"`
}

// VMExplanationAPI is the nested explanation object for VM recommendations.
type VMExplanationAPI struct {
	DataDays               *int    `json:"data_days,omitempty"`
	MaxCPUUsageMC          *int64  `json:"max_cpu_usage_millicores,omitempty"`
	MaxMemUsageKiB         *int64  `json:"max_mem_usage_kib,omitempty"`
	CPUMarginBP            *int32  `json:"cpu_margin_basis_points,omitempty"`
	MemMarginBP            *int32  `json:"mem_margin_basis_points,omitempty"`
	RawRecommendedVCPU     *int32  `json:"raw_recommended_vcpu,omitempty"`
	RawRecommendedMemGiB   *int32  `json:"raw_recommended_mem_gib,omitempty"`
	DownsizeHysteresisHeld *bool   `json:"downsize_hysteresis_held,omitempty"`
	GuestAgentUsed         *bool   `json:"guest_agent_used,omitempty"`
	IdleDetected           *bool   `json:"idle_detected,omitempty"`
	AbandonedDetected      *bool   `json:"abandoned_detected,omitempty"`
	PowerOffCandidate      *bool   `json:"power_off_candidate,omitempty"`
	SizingBranch           *string `json:"sizing_branch,omitempty"`
	GPUAction              *string `json:"gpu_action,omitempty"`
	GPURationale           *string `json:"gpu_rationale,omitempty"`
}

// SnapshotExplanationAPI is the nested explanation object for snapshot recommendations.
type SnapshotExplanationAPI struct {
	AgeDays              *int    `json:"age_days,omitempty"`
	SourcePVCExists      *bool   `json:"source_pvc_exists,omitempty"`
	RestoredPVCCount     *int    `json:"restored_pvc_count,omitempty"`
	ManagedBy            *string `json:"managed_by,omitempty"`
	RecommendationType   *string `json:"recommendation_type,omitempty"`
	ThresholdUsed        *int    `json:"threshold_used,omitempty"`
	ThresholdName        *string `json:"threshold_name,omitempty"`
	ClassificationRule   *string `json:"classification_rule,omitempty"`
}

// NodeGPUTimeslicingExplanationAPI is the nested explanation object for node GPU time-slicing recommendations.
type NodeGPUTimeslicingExplanationAPI struct {
	DataDays           *int    `json:"data_days,omitempty"`
	CandidateCount     *int    `json:"candidate_count,omitempty"`
	ImpactedCount      *int    `json:"impacted_count,omitempty"`
	ClassificationRule *string `json:"classification_rule,omitempty"`
}

type containerExplSource interface {
	containerExplFields() containerExplFields
}

type containerExplFields struct {
	confidenceLevel      *float32
	dataDays             *int
	decayHalfLifeHours   *float64
	cpuCostPctMC         *int64
	cpuPerfPctMC         *int64
	cpuUsageP95MC        *int64
	cpuUsageP50MC        *int64
	cpuUsageMeanMC       *int64
	cpuAdaptiveMarginBP  *int32
	cpuTrendSlope        *float64
	memCostPctKiB        *int64
	memPerfPctKiB        *int64
	memUsageP95KiB       *int64
	memUsageP50KiB       *int64
	memUsageMeanKiB      *int64
	memAdaptiveMarginBP  *int32
	memTrendSlope        *float64
	oomCountSum          *int64
	oomBumpApplied       *bool
	cpuFloorApplied      *bool
	memFloorApplied      *bool
	isIdle               *bool
}

func (r NativeRecommendationRow) containerExplFields() containerExplFields {
	return containerExplFields{
		confidenceLevel: r.ConfidenceLevel, dataDays: r.ExplDataDays, decayHalfLifeHours: r.ExplDecayHalfLifeHours,
		cpuCostPctMC: r.ExplCPUCostPctMC, cpuPerfPctMC: r.ExplCPUPerfPctMC,
		cpuUsageP95MC: r.ExplCPUUsageP95MC, cpuUsageP50MC: r.ExplCPUUsageP50MC, cpuUsageMeanMC: r.ExplCPUUsageMeanMC,
		cpuAdaptiveMarginBP: r.ExplCPUAdaptiveMarginBP, cpuTrendSlope: r.ExplCPUTrendSlope,
		memCostPctKiB: r.ExplMemCostPctKiB, memPerfPctKiB: r.ExplMemPerfPctKiB,
		memUsageP95KiB: r.ExplMemUsageP95KiB, memUsageP50KiB: r.ExplMemUsageP50KiB, memUsageMeanKiB: r.ExplMemUsageMeanKiB,
		memAdaptiveMarginBP: r.ExplMemAdaptiveMarginBP, memTrendSlope: r.ExplMemTrendSlope,
		oomCountSum: r.ExplOOMCountSum, oomBumpApplied: r.ExplOOMBumpApplied,
		cpuFloorApplied: r.ExplCPUFloorApplied, memFloorApplied: r.ExplMemFloorApplied, isIdle: r.ExplIsIdle,
	}
}

func (r NativeNamespaceRow) containerExplFields() containerExplFields {
	return containerExplFields{
		confidenceLevel: r.ConfidenceLevel, dataDays: r.ExplDataDays, decayHalfLifeHours: r.ExplDecayHalfLifeHours,
		cpuCostPctMC: r.ExplCPUCostPctMC, cpuPerfPctMC: r.ExplCPUPerfPctMC,
		cpuUsageP95MC: r.ExplCPUUsageP95MC, cpuUsageP50MC: r.ExplCPUUsageP50MC, cpuUsageMeanMC: r.ExplCPUUsageMeanMC,
		cpuAdaptiveMarginBP: r.ExplCPUAdaptiveMarginBP, cpuTrendSlope: r.ExplCPUTrendSlope,
		memCostPctKiB: r.ExplMemCostPctKiB, memPerfPctKiB: r.ExplMemPerfPctKiB,
		memUsageP95KiB: r.ExplMemUsageP95KiB, memUsageP50KiB: r.ExplMemUsageP50KiB, memUsageMeanKiB: r.ExplMemUsageMeanKiB,
		memAdaptiveMarginBP: r.ExplMemAdaptiveMarginBP, memTrendSlope: r.ExplMemTrendSlope,
		oomCountSum: r.ExplOOMCountSum, oomBumpApplied: r.ExplOOMBumpApplied,
		cpuFloorApplied: r.ExplCPUFloorApplied, memFloorApplied: r.ExplMemFloorApplied, isIdle: r.ExplIsIdle,
	}
}

func buildContainerExplanationAPI(src containerExplSource) *ContainerExplanationAPI {
	f := src.containerExplFields()
	if f.dataDays == nil && f.cpuCostPctMC == nil {
		return nil
	}
	return &ContainerExplanationAPI{
		ConfidenceLevel: f.confidenceLevel, DataDays: f.dataDays, DecayHalfLifeHours: f.decayHalfLifeHours,
		CPUCostPercentileMC: f.cpuCostPctMC, CPUPerfPercentileMC: f.cpuPerfPctMC,
		CPUUsageP95MC: f.cpuUsageP95MC, CPUUsageP50MC: f.cpuUsageP50MC, CPUUsageMeanMC: f.cpuUsageMeanMC,
		CPUAdaptiveMarginBP: f.cpuAdaptiveMarginBP, CPUTrendSlope: f.cpuTrendSlope,
		MemCostPercentileKiB: f.memCostPctKiB, MemPerfPercentileKiB: f.memPerfPctKiB,
		MemUsageP95KiB: f.memUsageP95KiB, MemUsageP50KiB: f.memUsageP50KiB, MemUsageMeanKiB: f.memUsageMeanKiB,
		MemAdaptiveMarginBP: f.memAdaptiveMarginBP, MemTrendSlope: f.memTrendSlope,
		OOMCountSum: f.oomCountSum, OOMBumpApplied: f.oomBumpApplied,
		CPUFloorApplied: f.cpuFloorApplied, MemFloorApplied: f.memFloorApplied, IsIdle: f.isIdle,
	}
}

// BuildContainerExplanationAPI assembles explanation JSON from native container row columns.
func BuildContainerExplanationAPI(row NativeRecommendationRow) *ContainerExplanationAPI {
	return buildContainerExplanationAPI(row)
}

// BuildNamespaceExplanationAPI assembles explanation JSON from native namespace row columns.
func BuildNamespaceExplanationAPI(row NativeNamespaceRow) *ContainerExplanationAPI {
	return buildContainerExplanationAPI(row)
}

// BuildGPUExplanationAPI assembles explanation JSON from persisted GPU expl columns.
func BuildGPUExplanationAPI(
	smActive, tensorActive, dramActive, fbMax, fbP98 *int32,
	recommendedProfile, currentProfile *string,
	hasProfiling, memoryBound *bool,
) *GPUExplanationAPI {
	if smActive == nil && recommendedProfile == nil && hasProfiling == nil {
		return nil
	}
	return &GPUExplanationAPI{
		SMActiveAvgBP: smActive, TensorActiveAvgBP: tensorActive, DRAMActiveAvgBP: dramActive,
		FBUsageMaxMiB: fbMax, FBP98MiB: fbP98,
		RecommendedProfile: recommendedProfile, CurrentProfile: currentProfile,
		HasProfilingData: hasProfiling, MemoryBound: memoryBound,
	}
}

// BuildNodeExplanationAPI assembles explanation JSON from node expl columns.
func BuildNodeExplanationAPI(
	confidence *float32, dataDays *int,
	targetUtil, headroom, emaImbalance *int32,
	currentCPU, maxCPUP95 *int64,
	currentMem, maxMemP95 *int64,
	consolidation *bool, formula *string,
) *NodeExplanationAPI {
	if dataDays == nil && targetUtil == nil && formula == nil {
		return nil
	}
	return &NodeExplanationAPI{
		ConfidenceLevel: confidence, DataDays: dataDays, TargetUtilizationBP: targetUtil,
		CurrentCPUMC: currentCPU, CurrentMemKiB: currentMem,
		MaxCPUUsageP95MC: maxCPUP95, MaxMemUsageP95KiB: maxMemP95,
		PodSchedulingHeadroomBP: headroom, EMAImbalanceBP: emaImbalance,
		ConsolidationApplied: consolidation, SizingFormula: formula,
	}
}

// BuildPVCExplanationAPI assembles explanation JSON from PVC columns.
func BuildPVCExplanationAPI(
	confidence *float32, dataDays *int, usageRatio *float64, growth *int64,
	oversized, nearFull, multiplier, minGiB *int32, reason *string,
) *PVCExplanationAPI {
	if dataDays == nil && reason == nil && usageRatio == nil {
		return nil
	}
	return &PVCExplanationAPI{
		ConfidenceLevel: confidence, DataDays: dataDays, UsageRatio: usageRatio, GrowthBytesPerDay: growth,
		OversizedThresholdBP: oversized, NearFullThresholdBP: nearFull,
		RecommendedSizeMultiplier: multiplier, MinRecommendedGiB: minGiB,
		ClassificationReason: reason,
	}
}

// BuildQuotaExplanationAPI assembles explanation JSON from quota expl columns.
func BuildQuotaExplanationAPI(
	headroom, maxUtil *int32,
	cpuSum, memSum, signalC *int64,
	riskLevel, reason *string,
) *QuotaExplanationAPI {
	if headroom == nil && reason == nil && cpuSum == nil {
		return nil
	}
	return &QuotaExplanationAPI{
		HeadroomBP: headroom, ContainerCPUSumMC: cpuSum, ContainerMemSumBytes: memSum,
		SignalCCPUUsedMC: signalC, MaxUtilizationBP: maxUtil,
		RiskLevel: riskLevel, RecommendationReason: reason,
	}
}

// BuildClusterQuotaExplanationAPI assembles explanation JSON from cluster quota expl columns.
func BuildClusterQuotaExplanationAPI(
	headroom, maxUtil *int32,
	cpuSum, memSum, baseCPU *int64,
	reason *string,
) *ClusterQuotaExplanationAPI {
	if headroom == nil && reason == nil && cpuSum == nil {
		return nil
	}
	return &ClusterQuotaExplanationAPI{
		HeadroomBP: headroom, NSQuotaCPUSumMC: cpuSum, NSQuotaMemSumBytes: memSum,
		BaseCPUMC: baseCPU, MaxUtilizationBP: maxUtil, RecommendationReason: reason,
	}
}

// BuildVMExplanationAPI assembles explanation JSON from VM expl columns.
func BuildVMExplanationAPI(rec VMRecommendation) *VMExplanationAPI {
	if rec.ExplDataDays == nil && rec.ExplSizingBranch == nil {
		return nil
	}
	return &VMExplanationAPI{
		DataDays: rec.ExplDataDays, MaxCPUUsageMC: rec.ExplMaxCPUUsageMC, MaxMemUsageKiB: rec.ExplMaxMemUsageKiB,
		CPUMarginBP: rec.ExplCPUMarginBP, MemMarginBP: rec.ExplMemMarginBP,
		RawRecommendedVCPU: rec.ExplRawRecommendedVCPU, RawRecommendedMemGiB: rec.ExplRawRecommendedMemGiB,
		DownsizeHysteresisHeld: rec.ExplDownsizeHysteresisHeld, GuestAgentUsed: rec.ExplGuestAgentUsed,
		IdleDetected: rec.ExplIdleDetected, AbandonedDetected: rec.ExplAbandonedDetected,
		PowerOffCandidate: rec.ExplPowerOffCandidate, SizingBranch: rec.ExplSizingBranch,
		GPUAction: rec.ExplGPUAction, GPURationale: rec.ExplGPURationale,
	}
}

// BuildSnapshotExplanationAPI assembles explanation JSON from snapshot columns.
func BuildSnapshotExplanationAPI(
	ageDays, restoredCount, thresholdUsed *int,
	sourceExists *bool, managedBy, recType, thresholdName, rule *string,
) *SnapshotExplanationAPI {
	if thresholdUsed == nil && rule == nil && ageDays == nil {
		return nil
	}
	return &SnapshotExplanationAPI{
		AgeDays: ageDays, SourcePVCExists: sourceExists, RestoredPVCCount: restoredCount,
		ManagedBy: managedBy, RecommendationType: recType,
		ThresholdUsed: thresholdUsed, ThresholdName: thresholdName, ClassificationRule: rule,
	}
}

// BuildNodeGPUTimeslicingExplanationAPI assembles explanation JSON from node GPU TS expl columns.
func BuildNodeGPUTimeslicingExplanationAPI(
	dataDays, candidateCount, impactedCount *int,
	rule *string,
) *NodeGPUTimeslicingExplanationAPI {
	if dataDays == nil && rule == nil && candidateCount == nil {
		return nil
	}
	return &NodeGPUTimeslicingExplanationAPI{
		DataDays: dataDays, CandidateCount: candidateCount, ImpactedCount: impactedCount,
		ClassificationRule: rule,
	}
}
