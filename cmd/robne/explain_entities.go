package main

import (
	"fmt"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
	"github.com/redhatinsights/ros-ocp-backend/librobne/snapshot"
	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
)

type namespaceExplainOut struct {
	Namespace             string  `json:"namespace"`
	Term                  string  `json:"term"`
	Engine                string  `json:"engine"`
	Schedule              string  `json:"schedule"`
	RecCPURequestMC       int64   `json:"rec_cpu_request_mc"`
	RecCPULimitMC         int64   `json:"rec_cpu_limit_mc"`
	RecMemRequestKiB      int64   `json:"rec_mem_request_kib"`
	RecMemLimitKiB        int64   `json:"rec_mem_limit_kib"`
	CurrentCPURequestMC   int64   `json:"current_cpu_request_mc"`
	CurrentMemRequestKiB  int64   `json:"current_mem_request_kib"`
	EstimatedSavingsCents *int64  `json:"estimated_savings_cents"`
	Stale                 bool    `json:"stale"`
	Category              string  `json:"category"`
	DataDays              int     `json:"data_days"`
	DecayHalfLifeHours    float64 `json:"decay_half_life_hours"`
	CPUCostPctMC          int64   `json:"cpu_cost_pct_mc"`
	CPUPerfPctMC          int64   `json:"cpu_perf_pct_mc"`
	CPUUsageP95MC         int64   `json:"cpu_usage_p95_mc"`
	CPUUsageP50MC         int64   `json:"cpu_usage_p50_mc"`
	CPUUsageMeanMC        int64   `json:"cpu_usage_mean_mc"`
	CPUAdaptiveMarginBP   int32   `json:"cpu_adaptive_margin_bp"`
	CPUTrendSlope         float64 `json:"cpu_trend_slope"`
	MemCostPctKiB         int64   `json:"mem_cost_pct_kib"`
	MemPerfPctKiB         int64   `json:"mem_perf_pct_kib"`
	MemUsageP95KiB        int64   `json:"mem_usage_p95_kib"`
	MemUsageP50KiB        int64   `json:"mem_usage_p50_kib"`
	MemUsageMeanKiB       int64   `json:"mem_usage_mean_kib"`
	MemAdaptiveMarginBP   int32   `json:"mem_adaptive_margin_bp"`
	MemTrendSlope         float64 `json:"mem_trend_slope"`
	OOMCountSum           int64   `json:"oom_count_sum"`
	OOMBumpApplied        bool    `json:"oom_bump_applied"`
	CPUFloorApplied       bool    `json:"cpu_floor_applied"`
	MemFloorApplied       bool    `json:"mem_floor_applied"`
	IsIdle                bool    `json:"is_idle"`
}

func toNamespaceExplainOut(r namespace.NamespaceRec, schedule string) namespaceExplainOut {
	row := toNamespaceOut(r)
	e := r.Expl
	return namespaceExplainOut{
		Namespace:             row.Namespace,
		Term:                  row.Term,
		Engine:                row.Engine,
		Schedule:              schedule,
		RecCPURequestMC:       row.RecCPURequestMC,
		RecCPULimitMC:         row.RecCPULimitMC,
		RecMemRequestKiB:      row.RecMemRequestKiB,
		RecMemLimitKiB:        row.RecMemLimitKiB,
		CurrentCPURequestMC:   row.CurrentCPURequestMC,
		CurrentMemRequestKiB:  row.CurrentMemRequestKiB,
		EstimatedSavingsCents: row.EstimatedSavingsCents,
		Stale:                 row.Stale,
		Category:              row.Category,
		DataDays:              e.DataDays,
		DecayHalfLifeHours:    e.DecayHalfLifeHours,
		CPUCostPctMC:          e.CPUCostPctMC,
		CPUPerfPctMC:          e.CPUPerfPctMC,
		CPUUsageP95MC:         e.CPUUsageP95MC,
		CPUUsageP50MC:         e.CPUUsageP50MC,
		CPUUsageMeanMC:        e.CPUUsageMeanMC,
		CPUAdaptiveMarginBP:   e.CPUAdaptiveMarginBP,
		CPUTrendSlope:         e.CPUTrendSlope,
		MemCostPctKiB:         e.MemCostPctKiB,
		MemPerfPctKiB:         e.MemPerfPctKiB,
		MemUsageP95KiB:        e.MemUsageP95KiB,
		MemUsageP50KiB:        e.MemUsageP50KiB,
		MemUsageMeanKiB:       e.MemUsageMeanKiB,
		MemAdaptiveMarginBP:   e.MemAdaptiveMarginBP,
		MemTrendSlope:         e.MemTrendSlope,
		OOMCountSum:           e.OOMCountSum,
		OOMBumpApplied:        e.OOMBumpApplied,
		CPUFloorApplied:       e.CPUFloorApplied,
		MemFloorApplied:       e.MemFloorApplied,
		IsIdle:                e.IsIdle,
	}
}

func selectNamespaceRec(recs []namespace.NamespaceRec, f explainFlags) (namespace.NamespaceRec, error) {
	var matches []namespace.NamespaceRec
	for _, rec := range recs {
		if rec.Namespace != f.namespace || rec.Term != f.term || rec.Engine != f.engine {
			continue
		}
		matches = append(matches, rec)
	}
	if len(matches) == 0 {
		return namespace.NamespaceRec{}, fmt.Errorf(
			"no matching namespace recommendation for namespace=%s term=%s engine=%s",
			f.namespace, f.term, f.engine,
		)
	}
	if len(matches) > 1 {
		return namespace.NamespaceRec{}, fmt.Errorf("match is not unique for namespace=%s term=%s engine=%s", f.namespace, f.term, f.engine)
	}
	return matches[0], nil
}

type nodeExplainOut struct {
	Node                    string `json:"node"`
	Term                    string `json:"term"`
	Engine                  string `json:"engine"`
	Category                string `json:"category"`
	IdleState               string `json:"idle_state"`
	RecommendedCPUMC        int64  `json:"recommended_cpu_mc"`
	RecommendedMemKiB       int64  `json:"recommended_mem_kib"`
	CurrentCPUMC            int64  `json:"current_cpu_mc"`
	CurrentMemKiB           int64  `json:"current_mem_kib"`
	NodeCountReduction      int    `json:"node_count_reduction"`
	EstimatedSavingsCents   *int64 `json:"estimated_savings_cents"`
	InstanceType            string `json:"instance_type"`
	SuggestedInstanceType   string `json:"suggested_instance_type"`
	DataDays                int    `json:"data_days"`
	TargetUtilizationBP     int32  `json:"target_utilization_bp"`
	MaxCPUUsageP95MC        int64  `json:"max_cpu_usage_p95_mc"`
	MaxMemUsageP95KiB       int64  `json:"max_mem_usage_p95_kib"`
	PodSchedulingHeadroomBP int32  `json:"pod_scheduling_headroom_bp"`
	EMAImbalanceBP          int32  `json:"ema_imbalance_bp"`
	ConsolidationApplied    bool   `json:"consolidation_applied"`
	SizingFormula           string `json:"sizing_formula"`
}

func toNodeExplainOut(r node.Rec) nodeExplainOut {
	row := toNodeOut(r)
	e := r.Expl
	return nodeExplainOut{
		Node:                    row.Node,
		Term:                    row.Term,
		Engine:                  row.Engine,
		Category:                row.Category,
		IdleState:               row.IdleState,
		RecommendedCPUMC:        row.RecommendedCPUMC,
		RecommendedMemKiB:       row.RecommendedMemKiB,
		CurrentCPUMC:            row.CurrentCPUMC,
		CurrentMemKiB:           row.CurrentMemKiB,
		NodeCountReduction:      row.NodeCountReduction,
		EstimatedSavingsCents:   row.EstimatedSavingsCents,
		InstanceType:            row.InstanceType,
		SuggestedInstanceType:   row.SuggestedInstanceType,
		DataDays:                e.DataDays,
		TargetUtilizationBP:     e.TargetUtilizationBP,
		MaxCPUUsageP95MC:        e.MaxCPUUsageP95MC,
		MaxMemUsageP95KiB:       e.MaxMemUsageP95KiB,
		PodSchedulingHeadroomBP: e.PodSchedulingHeadroomBP,
		EMAImbalanceBP:          e.EMAImbalanceBP,
		ConsolidationApplied:    e.ConsolidationApplied,
		SizingFormula:           e.SizingFormula,
	}
}

func selectNodeRec(recs []node.Rec, f explainFlags) (node.Rec, error) {
	var matches []node.Rec
	for _, rec := range recs {
		if rec.Node != f.node || rec.Term != f.term || rec.Engine != f.engine {
			continue
		}
		matches = append(matches, rec)
	}
	if len(matches) == 0 {
		return node.Rec{}, fmt.Errorf("no matching node recommendation for node=%s term=%s engine=%s", f.node, f.term, f.engine)
	}
	if len(matches) > 1 {
		return node.Rec{}, fmt.Errorf("match is not unique for node=%s term=%s engine=%s", f.node, f.term, f.engine)
	}
	return matches[0], nil
}

type gpuExplainOut struct {
	Namespace                string `json:"namespace"`
	Workload                 string `json:"workload"`
	ContainerName            string `json:"container_name"`
	Term                     string `json:"term"`
	GPUModelName             string `json:"gpu_model_name"`
	CurrentGPUProfile        string `json:"current_gpu_profile"`
	RecommendedGPUProfile    string `json:"recommended_gpu_profile"`
	Classification           string `json:"classification"`
	GPUCount                 int    `json:"gpu_count"`
	EstimatedGPUSavingsCents *int64 `json:"estimated_gpu_savings_cents"`
	SMActiveAvgBP            int32  `json:"sm_active_avg_bp"`
	TensorActiveAvgBP        int32  `json:"tensor_active_avg_bp"`
	DRAMActiveAvgBP          int32  `json:"dram_active_avg_bp"`
	FBUsageMaxMiB            int32  `json:"fb_usage_max_mib"`
	FBP98MiB                 int32  `json:"fb_p98_mib"`
	HasProfilingData         bool   `json:"has_profiling_data"`
	MemoryBound              bool   `json:"memory_bound"`
}

func toGPUExplainOut(r gpuRecRow) gpuExplainOut {
	row := toGPUOut(r)
	e := gpuExplFromRec(r.Rec)
	return gpuExplainOut{
		Namespace:                row.Namespace,
		Workload:                 row.Workload,
		ContainerName:            row.ContainerName,
		Term:                     row.Term,
		GPUModelName:             row.GPUModelName,
		CurrentGPUProfile:        row.CurrentGPUProfile,
		RecommendedGPUProfile:    row.RecommendedGPUProfile,
		Classification:           row.Classification,
		GPUCount:                 row.GPUCount,
		EstimatedGPUSavingsCents: row.EstimatedGPUSavingsCents,
		SMActiveAvgBP:            e.SMActiveAvgBP,
		TensorActiveAvgBP:        e.TensorActiveAvgBP,
		DRAMActiveAvgBP:          e.DRAMActiveAvgBP,
		FBUsageMaxMiB:            e.FBUsageMaxMiB,
		FBP98MiB:                 e.FBP98MiB,
		HasProfilingData:         e.HasProfilingData,
		MemoryBound:              e.MemoryBound,
	}
}

func selectGPURec(recs []gpuRecRow, f explainFlags) (gpuRecRow, error) {
	var matches []gpuRecRow
	for _, rec := range recs {
		if rec.Namespace != f.namespace || rec.Workload != f.workload || rec.ContainerName != f.container {
			continue
		}
		if rec.Rec.Term != f.term {
			continue
		}
		if f.gpuModel != "" && rec.Rec.GPUModelName != f.gpuModel {
			continue
		}
		matches = append(matches, rec)
	}
	if len(matches) == 0 {
		return gpuRecRow{}, fmt.Errorf(
			"no matching gpu recommendation for namespace=%s workload=%s container=%s term=%s",
			f.namespace, f.workload, f.container, f.term,
		)
	}
	if len(matches) > 1 {
		models := make([]string, 0, len(matches))
		for _, rec := range matches {
			models = append(models, rec.Rec.GPUModelName)
		}
		return gpuRecRow{}, fmt.Errorf("match is not unique; pass --gpu-model (got %s)", strings.Join(models, ", "))
	}
	return matches[0], nil
}

type gpuTimeslicingExplainOut struct {
	Node                string `json:"node"`
	GPUModel            string `json:"gpu_model"`
	Term                string `json:"term"`
	RecommendedReplicas int    `json:"recommended_replicas"`
	DataDays            int    `json:"data_days"`
	CandidateCount      int    `json:"candidate_count"`
	ImpactedCount       int    `json:"impacted_count"`
	ClassificationRule  string `json:"classification_rule"`
}

func toGPUTimeslicingExplainOut(r gpu.TimeslicingRec) gpuTimeslicingExplainOut {
	row := toGPUTimeslicingOut(r)
	e := r.Expl
	return gpuTimeslicingExplainOut{
		Node:                row.Node,
		GPUModel:            row.GPUModel,
		Term:                row.Term,
		RecommendedReplicas: row.RecommendedReplicas,
		DataDays:            e.DataDays,
		CandidateCount:      e.CandidateCount,
		ImpactedCount:       e.ImpactedCount,
		ClassificationRule:  e.ClassificationRule,
	}
}

func selectGPUTimeslicingRec(recs []gpu.TimeslicingRec, f explainFlags) (gpu.TimeslicingRec, error) {
	var matches []gpu.TimeslicingRec
	for _, rec := range recs {
		if rec.NodeName != f.node || rec.GPUModel != f.gpuModel || rec.Term != f.term {
			continue
		}
		matches = append(matches, rec)
	}
	if len(matches) == 0 {
		return gpu.TimeslicingRec{}, fmt.Errorf(
			"no matching gpu timeslicing recommendation for node=%s gpu_model=%s term=%s",
			f.node, f.gpuModel, f.term,
		)
	}
	if len(matches) > 1 {
		return gpu.TimeslicingRec{}, fmt.Errorf("match is not unique for node=%s gpu_model=%s term=%s", f.node, f.gpuModel, f.term)
	}
	return matches[0], nil
}

type pvcExplainOut struct {
	Namespace                 string  `json:"namespace"`
	PVC                       string  `json:"pvc"`
	Term                      string  `json:"term"`
	RecommendationType        string  `json:"recommendation_type"`
	CapacityBytes             int64   `json:"capacity_bytes"`
	RequestBytes              int64   `json:"request_bytes"`
	UsageBytesMax             int64   `json:"usage_bytes_max"`
	RecommendedBytes          *int64  `json:"recommended_bytes"`
	DaysToFull                *int    `json:"days_to_full"`
	EstimatedSavingsCents     *int64  `json:"estimated_savings_cents"`
	StorageClass              string  `json:"storage_class"`
	VMName                    string  `json:"vm_name"`
	UsageRatio                float64 `json:"usage_ratio"`
	GrowthBytesPerDay         int64   `json:"growth_bytes_per_day"`
	DataDays                  int     `json:"data_days"`
	OversizedThresholdBP      int32   `json:"oversized_threshold_bp"`
	NearFullThresholdBP       int32   `json:"near_full_threshold_bp"`
	RecommendedSizeMultiplier int32   `json:"recommended_size_multiplier"`
	MinRecommendedGiB         int32   `json:"min_recommended_gib"`
	ClassificationReason      string  `json:"classification_reason"`
}

func toPVCExplainOut(r pvc.PVCRec) pvcExplainOut {
	row := toPVCOut(r)
	e := r.Expl
	return pvcExplainOut{
		Namespace:                 row.Namespace,
		PVC:                       row.PVC,
		Term:                      row.Term,
		RecommendationType:        row.RecommendationType,
		CapacityBytes:             row.CapacityBytes,
		RequestBytes:              row.RequestBytes,
		UsageBytesMax:             row.UsageBytesMax,
		RecommendedBytes:          row.RecommendedBytes,
		DaysToFull:                row.DaysToFull,
		EstimatedSavingsCents:     row.EstimatedSavingsCents,
		StorageClass:              row.StorageClass,
		VMName:                    row.VMName,
		UsageRatio:                r.UsageRatio,
		GrowthBytesPerDay:         r.GrowthBytesPerDay,
		DataDays:                  e.DataDays,
		OversizedThresholdBP:      e.OversizedThresholdBP,
		NearFullThresholdBP:       e.NearFullThresholdBP,
		RecommendedSizeMultiplier: e.RecommendedSizeMultiplier,
		MinRecommendedGiB:         e.MinRecommendedGiB,
		ClassificationReason:      e.ClassificationReason,
	}
}

func selectPVCRec(recs []pvc.PVCRec, f explainFlags) (pvc.PVCRec, error) {
	var matches []pvc.PVCRec
	for _, rec := range recs {
		if rec.Namespace != f.namespace || rec.PVC != f.pvc || rec.Term != f.term {
			continue
		}
		matches = append(matches, rec)
	}
	if len(matches) == 0 {
		return pvc.PVCRec{}, fmt.Errorf("no matching pvc recommendation for namespace=%s pvc=%s term=%s", f.namespace, f.pvc, f.term)
	}
	if len(matches) > 1 {
		return pvc.PVCRec{}, fmt.Errorf("match is not unique for namespace=%s pvc=%s term=%s", f.namespace, f.pvc, f.term)
	}
	return matches[0], nil
}

type vmExplainOut struct {
	Namespace                 string `json:"namespace"`
	VMName                    string `json:"vm_name"`
	Term                      string `json:"term"`
	Engine                    string `json:"engine"`
	Category                  string `json:"category"`
	CurrentVCPU               int32  `json:"current_vcpu"`
	CurrentMemoryGiB          int32  `json:"current_memory_gib"`
	RecommendedVCPU           int32  `json:"recommended_vcpu"`
	RecommendedMemoryGiB      int32  `json:"recommended_memory_gib"`
	RecommendedInstanceType   string `json:"recommended_instance_type"`
	GuestOS                   string `json:"guest_os"`
	EstimatedSavingsCents     *int64 `json:"estimated_savings_cents"`
	RecommendedTimeSliceCount int32  `json:"recommended_time_slice_count"`
	DataDays                  int    `json:"data_days"`
	MaxCPUUsageMC             int64  `json:"max_cpu_usage_mc"`
	MaxMemUsageKiB            int64  `json:"max_mem_usage_kib"`
	CPUMarginBP               int32  `json:"cpu_margin_bp"`
	MemMarginBP               int32  `json:"mem_margin_bp"`
	RawRecommendedVCPU        int32  `json:"raw_recommended_vcpu"`
	RawRecommendedMemGiB      int32  `json:"raw_recommended_mem_gib"`
	DownsizeHysteresisHeld    bool   `json:"downsize_hysteresis_held"`
	GuestAgentUsed            bool   `json:"guest_agent_used"`
	IdleDetected              bool   `json:"idle_detected"`
	AbandonedDetected         bool   `json:"abandoned_detected"`
	PowerOffCandidate         bool   `json:"power_off_candidate"`
	SizingBranch              string `json:"sizing_branch"`
	GPUAction                 string `json:"gpu_action"`
	GPURationale              string `json:"gpu_rationale"`
}

func toVMExplainOut(r vm.VMRecommendation) vmExplainOut {
	row := toVMOut(r)
	e := vm.VMExplFromRecommendation(r)
	return vmExplainOut{
		Namespace:                 row.Namespace,
		VMName:                    row.VMName,
		Term:                      row.Term,
		Engine:                    row.Engine,
		Category:                  row.Category,
		CurrentVCPU:               row.CurrentVCPU,
		CurrentMemoryGiB:          row.CurrentMemoryGiB,
		RecommendedVCPU:           row.RecommendedVCPU,
		RecommendedMemoryGiB:      row.RecommendedMemoryGiB,
		RecommendedInstanceType:   row.RecommendedInstanceType,
		GuestOS:                   row.GuestOS,
		EstimatedSavingsCents:     row.EstimatedSavingsCents,
		RecommendedTimeSliceCount: row.RecommendedTimeSliceCount,
		DataDays:                  e.DataDays,
		MaxCPUUsageMC:             e.MaxCPUUsageMC,
		MaxMemUsageKiB:            e.MaxMemUsageKiB,
		CPUMarginBP:               e.CPUMarginBP,
		MemMarginBP:               e.MemMarginBP,
		RawRecommendedVCPU:        e.RawRecommendedVCPU,
		RawRecommendedMemGiB:      e.RawRecommendedMemGiB,
		DownsizeHysteresisHeld:    e.DownsizeHysteresisHeld,
		GuestAgentUsed:            e.GuestAgentUsed,
		IdleDetected:              e.IdleDetected,
		AbandonedDetected:         e.AbandonedDetected,
		PowerOffCandidate:         e.PowerOffCandidate,
		SizingBranch:              e.SizingBranch,
		GPUAction:                 e.GPUAction,
		GPURationale:              e.GPURationale,
	}
}

func selectVMRec(recs []vm.VMRecommendation, f explainFlags) (vm.VMRecommendation, error) {
	var matches []vm.VMRecommendation
	for _, rec := range recs {
		if rec.Namespace != f.namespace || rec.VMName != f.vmName || rec.Term != f.term || rec.Engine != f.engine {
			continue
		}
		matches = append(matches, rec)
	}
	if len(matches) == 0 {
		return vm.VMRecommendation{}, fmt.Errorf(
			"no matching vm recommendation for namespace=%s vm_name=%s term=%s engine=%s",
			f.namespace, f.vmName, f.term, f.engine,
		)
	}
	if len(matches) > 1 {
		return vm.VMRecommendation{}, fmt.Errorf("match is not unique for namespace=%s vm_name=%s term=%s engine=%s", f.namespace, f.vmName, f.term, f.engine)
	}
	return matches[0], nil
}

type quotaExplainOut struct {
	Namespace                      string `json:"namespace"`
	QuotaName                      string `json:"quota_name"`
	RecommendationType             string `json:"recommendation_type"`
	RiskLevel                      string `json:"risk_level"`
	CPURequestHardMC               int64  `json:"cpu_request_hard_mc"`
	CPULimitHardMC                 int64  `json:"cpu_limit_hard_mc"`
	MemoryRequestHardBytes         int64  `json:"memory_request_hard_bytes"`
	MemoryLimitHardBytes           int64  `json:"memory_limit_hard_bytes"`
	CPURequestRecommendedMC        int64  `json:"cpu_request_recommended_mc"`
	CPULimitRecommendedMC          int64  `json:"cpu_limit_recommended_mc"`
	MemoryRequestRecommendedBytes  int64  `json:"memory_request_recommended_bytes"`
	MemoryLimitRecommendedBytes    int64  `json:"memory_limit_recommended_bytes"`
	StorageRequestHardBytes        int64  `json:"storage_request_hard_bytes"`
	StorageRequestRecommendedBytes int64  `json:"storage_request_recommended_bytes"`
	PodsHard                       int64  `json:"pods_hard"`
	PodsRecommended                int64  `json:"pods_recommended"`
	EstimatedSavingsCents          *int64 `json:"estimated_savings_cents"`
	HeadroomBP                     int32  `json:"headroom_bp"`
	ContainerCPUSumMC              int64  `json:"container_cpu_sum_mc"`
	ContainerMemSumBytes           int64  `json:"container_mem_sum_bytes"`
	SignalCCPUUsedMC               int64  `json:"signal_c_cpu_used_mc"`
	MaxUtilizationBP               int32  `json:"max_utilization_bp"`
	RecommendationReason           string `json:"recommendation_reason"`
}

func toQuotaExplainOut(r quota.QuotaRec) quotaExplainOut {
	row := toQuotaOut(r)
	e := r.Expl
	return quotaExplainOut{
		Namespace:                      row.Namespace,
		QuotaName:                      row.QuotaName,
		RecommendationType:             row.RecommendationType,
		RiskLevel:                      row.RiskLevel,
		CPURequestHardMC:               row.CPURequestHardMC,
		CPULimitHardMC:                 row.CPULimitHardMC,
		MemoryRequestHardBytes:         row.MemoryRequestHardBytes,
		MemoryLimitHardBytes:           row.MemoryLimitHardBytes,
		CPURequestRecommendedMC:        row.CPURequestRecommendedMC,
		CPULimitRecommendedMC:          row.CPULimitRecommendedMC,
		MemoryRequestRecommendedBytes:  row.MemoryRequestRecommendedBytes,
		MemoryLimitRecommendedBytes:    row.MemoryLimitRecommendedBytes,
		StorageRequestHardBytes:        row.StorageRequestHardBytes,
		StorageRequestRecommendedBytes: row.StorageRequestRecommendedBytes,
		PodsHard:                       row.PodsHard,
		PodsRecommended:                row.PodsRecommended,
		EstimatedSavingsCents:          row.EstimatedSavingsCents,
		HeadroomBP:                     e.HeadroomBP,
		ContainerCPUSumMC:              e.ContainerCPUSumMC,
		ContainerMemSumBytes:           e.ContainerMemSumBytes,
		SignalCCPUUsedMC:               e.SignalCCPUUsedMC,
		MaxUtilizationBP:               e.MaxUtilizationBP,
		RecommendationReason:           e.RecommendationReason,
	}
}

func selectQuotaRec(recs []quota.QuotaRec, f explainFlags) (quota.QuotaRec, error) {
	var matches []quota.QuotaRec
	for _, rec := range recs {
		if rec.Namespace != f.namespace || rec.QuotaName != f.quotaName {
			continue
		}
		if f.recommendationType != "" && rec.RecommendationType != f.recommendationType {
			continue
		}
		matches = append(matches, rec)
	}
	if len(matches) == 0 {
		return quota.QuotaRec{}, fmt.Errorf("no matching quota recommendation for namespace=%s quota_name=%s", f.namespace, f.quotaName)
	}
	if len(matches) > 1 {
		typesSeen := make([]string, 0, len(matches))
		for _, rec := range matches {
			typesSeen = append(typesSeen, rec.RecommendationType)
		}
		return quota.QuotaRec{}, fmt.Errorf("match is not unique; pass --recommendation-type (got %s)", strings.Join(typesSeen, ", "))
	}
	return matches[0], nil
}

type clusterQuotaExplainOut struct {
	ClusterQuotaName               string `json:"cluster_quota_name"`
	Namespaces                     string `json:"namespaces"`
	RecommendationType             string `json:"recommendation_type"`
	RiskLevel                      string `json:"risk_level"`
	CPURequestHardMC               int64  `json:"cpu_request_hard_mc"`
	CPULimitHardMC                 int64  `json:"cpu_limit_hard_mc"`
	MemoryRequestHardBytes         int64  `json:"memory_request_hard_bytes"`
	MemoryLimitHardBytes           int64  `json:"memory_limit_hard_bytes"`
	CPURequestRecommendedMC        int64  `json:"cpu_request_recommended_mc"`
	CPULimitRecommendedMC          int64  `json:"cpu_limit_recommended_mc"`
	MemoryRequestRecommendedBytes  int64  `json:"memory_request_recommended_bytes"`
	MemoryLimitRecommendedBytes    int64  `json:"memory_limit_recommended_bytes"`
	StorageRequestHardBytes        int64  `json:"storage_request_hard_bytes"`
	StorageRequestRecommendedBytes int64  `json:"storage_request_recommended_bytes"`
	PodsHard                       int64  `json:"pods_hard"`
	PodsRecommended                int64  `json:"pods_recommended"`
	EstimatedSavingsCents          *int64 `json:"estimated_savings_cents"`
	HeadroomBP                     int32  `json:"headroom_bp"`
	NSQuotaCPUSumMC                int64  `json:"ns_quota_cpu_sum_mc"`
	NSQuotaMemSumBytes             int64  `json:"ns_quota_mem_sum_bytes"`
	BaseCPUMC                      int64  `json:"base_cpu_mc"`
	MaxUtilizationBP               int32  `json:"max_utilization_bp"`
	RecommendationReason           string `json:"recommendation_reason"`
}

func toClusterQuotaExplainOut(r quota.ClusterQuotaRec) clusterQuotaExplainOut {
	row := toClusterQuotaOut(r)
	e := r.Expl
	return clusterQuotaExplainOut{
		ClusterQuotaName:               row.ClusterQuotaName,
		Namespaces:                     row.Namespaces,
		RecommendationType:             row.RecommendationType,
		RiskLevel:                      row.RiskLevel,
		CPURequestHardMC:               row.CPURequestHardMC,
		CPULimitHardMC:                 row.CPULimitHardMC,
		MemoryRequestHardBytes:         row.MemoryRequestHardBytes,
		MemoryLimitHardBytes:           row.MemoryLimitHardBytes,
		CPURequestRecommendedMC:        row.CPURequestRecommendedMC,
		CPULimitRecommendedMC:          row.CPULimitRecommendedMC,
		MemoryRequestRecommendedBytes:  row.MemoryRequestRecommendedBytes,
		MemoryLimitRecommendedBytes:    row.MemoryLimitRecommendedBytes,
		StorageRequestHardBytes:        row.StorageRequestHardBytes,
		StorageRequestRecommendedBytes: row.StorageRequestRecommendedBytes,
		PodsHard:                       row.PodsHard,
		PodsRecommended:                row.PodsRecommended,
		EstimatedSavingsCents:          row.EstimatedSavingsCents,
		HeadroomBP:                     e.HeadroomBP,
		NSQuotaCPUSumMC:                e.NSQuotaCPUSumMC,
		NSQuotaMemSumBytes:             e.NSQuotaMemSumBytes,
		BaseCPUMC:                      e.BaseCPUMC,
		MaxUtilizationBP:               e.MaxUtilizationBP,
		RecommendationReason:           e.RecommendationReason,
	}
}

func selectClusterQuotaRec(recs []quota.ClusterQuotaRec, f explainFlags) (quota.ClusterQuotaRec, error) {
	var matches []quota.ClusterQuotaRec
	for _, rec := range recs {
		if rec.ClusterQuotaName != f.clusterQuotaName {
			continue
		}
		if f.recommendationType != "" && rec.RecommendationType != f.recommendationType {
			continue
		}
		matches = append(matches, rec)
	}
	if len(matches) == 0 {
		return quota.ClusterQuotaRec{}, fmt.Errorf("no matching cluster_quota recommendation for cluster_quota_name=%s", f.clusterQuotaName)
	}
	if len(matches) > 1 {
		typesSeen := make([]string, 0, len(matches))
		for _, rec := range matches {
			typesSeen = append(typesSeen, rec.RecommendationType)
		}
		return quota.ClusterQuotaRec{}, fmt.Errorf("match is not unique; pass --recommendation-type (got %s)", strings.Join(typesSeen, ", "))
	}
	return matches[0], nil
}

type snapshotExplainOut struct {
	Namespace           string  `json:"namespace"`
	SnapshotName        string  `json:"snapshot_name"`
	SourcePVCName       string  `json:"source_pvc_name"`
	VolumeSnapshotClass string  `json:"volume_snapshot_class"`
	StorageClass        string  `json:"storage_class"`
	CreationTimestamp   string  `json:"creation_timestamp"`
	RestoreSizeBytes    int64   `json:"restore_size_bytes"`
	AgeDays             int     `json:"age_days"`
	SourcePVCExists     bool    `json:"source_pvc_exists"`
	RestoredPVCCount    int     `json:"restored_pvc_count"`
	ManagedBy           string  `json:"managed_by"`
	RecommendationType  string  `json:"recommendation_type"`
	EstimatedCostCents  *int64  `json:"estimated_cost_cents"`
	NotificationCodes   []int16 `json:"notification_codes"`
	ThresholdUsed       int     `json:"threshold_used"`
	ThresholdName       string  `json:"threshold_name"`
	ClassificationRule  string  `json:"classification_rule"`
}

func toSnapshotExplainOut(r snapshot.SnapshotRec) snapshotExplainOut {
	row := toSnapshotOut(r)
	e := r.Expl
	return snapshotExplainOut{
		Namespace:           row.Namespace,
		SnapshotName:        row.SnapshotName,
		SourcePVCName:       row.SourcePVCName,
		VolumeSnapshotClass: row.VolumeSnapshotClass,
		StorageClass:        row.StorageClass,
		CreationTimestamp:   row.CreationTimestamp,
		RestoreSizeBytes:    row.RestoreSizeBytes,
		AgeDays:             row.AgeDays,
		SourcePVCExists:     row.SourcePVCExists,
		RestoredPVCCount:    row.RestoredPVCCount,
		ManagedBy:           row.ManagedBy,
		RecommendationType:  row.RecommendationType,
		EstimatedCostCents:  row.EstimatedCostCents,
		NotificationCodes:   row.NotificationCodes,
		ThresholdUsed:       e.ThresholdUsed,
		ThresholdName:       e.ThresholdName,
		ClassificationRule:  e.ClassificationRule,
	}
}

func selectSnapshotRec(recs []snapshot.SnapshotRec, f explainFlags) (snapshot.SnapshotRec, error) {
	var matches []snapshot.SnapshotRec
	for _, rec := range recs {
		if rec.Namespace != f.namespace || rec.SnapshotName != f.snapshotName {
			continue
		}
		matches = append(matches, rec)
	}
	if len(matches) == 0 {
		return snapshot.SnapshotRec{}, fmt.Errorf("no matching snapshot recommendation for namespace=%s snapshot_name=%s", f.namespace, f.snapshotName)
	}
	if len(matches) > 1 {
		return snapshot.SnapshotRec{}, fmt.Errorf("match is not unique for namespace=%s snapshot_name=%s", f.namespace, f.snapshotName)
	}
	return matches[0], nil
}
