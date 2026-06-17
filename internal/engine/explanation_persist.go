package engine

import (
	"strconv"
)

// containerExplSQLColumns lists expl_* column names for container/namespace INSERT/UPDATE.
const containerExplSQLColumns = `
				expl_data_days, expl_decay_half_life_hours,
				expl_cpu_cost_pct_mc, expl_cpu_perf_pct_mc,
				expl_cpu_usage_p95_mc, expl_cpu_usage_p50_mc, expl_cpu_usage_mean_mc,
				expl_cpu_adaptive_margin_bp, expl_cpu_trend_slope,
				expl_mem_cost_pct_kib, expl_mem_perf_pct_kib,
				expl_mem_usage_p95_kib, expl_mem_usage_p50_kib, expl_mem_usage_mean_kib,
				expl_mem_adaptive_margin_bp, expl_mem_trend_slope,
				expl_oom_count_sum, expl_oom_bump_applied, expl_cpu_floor_applied, expl_is_idle`

// containerExplUpdateSet is the ON CONFLICT DO UPDATE fragment for container expl columns.
const containerExplUpdateSet = `
				expl_data_days = EXCLUDED.expl_data_days,
				expl_decay_half_life_hours = EXCLUDED.expl_decay_half_life_hours,
				expl_cpu_cost_pct_mc = EXCLUDED.expl_cpu_cost_pct_mc,
				expl_cpu_perf_pct_mc = EXCLUDED.expl_cpu_perf_pct_mc,
				expl_cpu_usage_p95_mc = EXCLUDED.expl_cpu_usage_p95_mc,
				expl_cpu_usage_p50_mc = EXCLUDED.expl_cpu_usage_p50_mc,
				expl_cpu_usage_mean_mc = EXCLUDED.expl_cpu_usage_mean_mc,
				expl_cpu_adaptive_margin_bp = EXCLUDED.expl_cpu_adaptive_margin_bp,
				expl_cpu_trend_slope = EXCLUDED.expl_cpu_trend_slope,
				expl_mem_cost_pct_kib = EXCLUDED.expl_mem_cost_pct_kib,
				expl_mem_perf_pct_kib = EXCLUDED.expl_mem_perf_pct_kib,
				expl_mem_usage_p95_kib = EXCLUDED.expl_mem_usage_p95_kib,
				expl_mem_usage_p50_kib = EXCLUDED.expl_mem_usage_p50_kib,
				expl_mem_usage_mean_kib = EXCLUDED.expl_mem_usage_mean_kib,
				expl_mem_adaptive_margin_bp = EXCLUDED.expl_mem_adaptive_margin_bp,
				expl_mem_trend_slope = EXCLUDED.expl_mem_trend_slope,
				expl_oom_count_sum = EXCLUDED.expl_oom_count_sum,
				expl_oom_bump_applied = EXCLUDED.expl_oom_bump_applied,
				expl_cpu_floor_applied = EXCLUDED.expl_cpu_floor_applied,
				expl_is_idle = EXCLUDED.expl_is_idle`

// containerExplValuePlaceholders returns $N placeholders for containerExplSQLColumns (20 columns).
func containerExplValuePlaceholders(start int) string {
	// 20 placeholders starting at start
	s := ""
	for i := 0; i < 20; i++ {
		if i > 0 {
			s += ","
		}
		s += "$" + strconv.Itoa(start+i)
	}
	return s
}

func appendContainerExplArgs(args []any, e ContainerExplanationFactors) []any {
	return append(args,
		nullIntExpl(e.DataDays),
		nullFloatExpl(e.DecayHalfLifeHours),
		nullInt64Expl(e.CPUCostPctMC),
		nullInt64Expl(e.CPUPerfPctMC),
		nullInt64Expl(e.CPUUsageP95MC),
		nullInt64Expl(e.CPUUsageP50MC),
		nullInt64Expl(e.CPUUsageMeanMC),
		nullInt32Expl(e.CPUAdaptiveMarginBP),
		nullFloatExpl(e.CPUTrendSlope),
		nullInt64Expl(e.MemCostPctKiB),
		nullInt64Expl(e.MemPerfPctKiB),
		nullInt64Expl(e.MemUsageP95KiB),
		nullInt64Expl(e.MemUsageP50KiB),
		nullInt64Expl(e.MemUsageMeanKiB),
		nullInt32Expl(e.MemAdaptiveMarginBP),
		nullFloatExpl(e.MemTrendSlope),
		nullInt64Expl(e.OOMCountSum),
		e.OOMBumpApplied,
		e.CPUFloorApplied,
		e.IsIdle,
	)
}

func nullIntExpl(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullInt64Expl(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullInt32Expl(v int32) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullFloatExpl(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

func appendGPUExplArgs(args []any, e GPUExplanationFactors) []any {
	return append(args,
		nullInt32Expl(e.SMActiveAvgBP),
		nullInt32Expl(e.TensorActiveAvgBP),
		nullInt32Expl(e.DRAMActiveAvgBP),
		nullInt32Expl(e.FBUsageMaxMiB),
		nullInt32Expl(e.FBP98MiB),
		nullStringExpl(e.RecommendedProfile),
		nullStringExpl(e.CurrentProfile),
		e.HasProfilingData,
		e.MemoryBound,
	)
}

func nullStringExpl(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// gpuExplFromRec maps GPURec fields to GPUExplanationFactors for persistence.
func gpuExplFromRec(rec GPURec, fbP98MiB int32) GPUExplanationFactors {
	return GPUExplanationFactors{
		SMActiveAvgBP:      int32(rec.SMActiveAvg * float32(BasisPointsScale)),
		TensorActiveAvgBP:  int32(rec.TensorPipeActiveAvg * float32(BasisPointsScale)),
		DRAMActiveAvgBP:    int32(rec.DRAMActiveAvg * float32(BasisPointsScale)),
		FBUsageMaxMiB:      int32(rec.FBUsageMaxMiB),
		FBP98MiB:           fbP98MiB,
		RecommendedProfile: rec.RecommendedGPUProfile,
		CurrentProfile:     rec.CurrentGPUProfile,
		HasProfilingData:   rec.HasProfilingData,
		MemoryBound:        rec.MemoryBoundDetected,
	}
}

const gpuExplUpdateSet = `
				expl_gpu_sm_active_avg_bp = EXCLUDED.expl_gpu_sm_active_avg_bp,
				expl_gpu_tensor_active_avg_bp = EXCLUDED.expl_gpu_tensor_active_avg_bp,
				expl_gpu_dram_active_avg_bp = EXCLUDED.expl_gpu_dram_active_avg_bp,
				expl_gpu_fb_usage_max_mib = EXCLUDED.expl_gpu_fb_usage_max_mib,
				expl_gpu_fb_p98_mib = EXCLUDED.expl_gpu_fb_p98_mib,
				expl_gpu_recommended_profile = EXCLUDED.expl_gpu_recommended_profile,
				expl_gpu_current_profile = EXCLUDED.expl_gpu_current_profile,
				expl_gpu_has_profiling_data = EXCLUDED.expl_gpu_has_profiling_data,
				expl_gpu_memory_bound = EXCLUDED.expl_gpu_memory_bound`

func appendQuotaExplArgs(args []any, e QuotaExplanationFactors) []any {
	return append(args,
		nullInt32Expl(e.HeadroomBP),
		nullInt64Expl(e.ContainerCPUSumMC),
		nullInt64Expl(e.ContainerMemSumBytes),
		nullInt64Expl(e.SignalCCPUUsedMC),
		nullInt32Expl(e.MaxUtilizationBP),
		nullStringExpl(e.RiskLevel),
		nullStringExpl(e.RecommendationReason),
	)
}

func appendClusterQuotaExplArgs(args []any, e ClusterQuotaExplanationFactors) []any {
	return append(args,
		nullInt32Expl(e.HeadroomBP),
		nullInt64Expl(e.NSQuotaCPUSumMC),
		nullInt64Expl(e.NSQuotaMemSumBytes),
		nullInt64Expl(e.BaseCPUMC),
		nullInt32Expl(e.MaxUtilizationBP),
		nullStringExpl(e.RecommendationReason),
	)
}

const quotaExplSQLColumns = `
				expl_headroom_bp, expl_container_cpu_sum_mc, expl_container_mem_sum_bytes,
				expl_signal_c_cpu_used_mc, expl_max_utilization_bp, expl_risk_level, expl_recommendation_reason`

const quotaExplUpdateSet = `
				expl_headroom_bp = EXCLUDED.expl_headroom_bp,
				expl_container_cpu_sum_mc = EXCLUDED.expl_container_cpu_sum_mc,
				expl_container_mem_sum_bytes = EXCLUDED.expl_container_mem_sum_bytes,
				expl_signal_c_cpu_used_mc = EXCLUDED.expl_signal_c_cpu_used_mc,
				expl_max_utilization_bp = EXCLUDED.expl_max_utilization_bp,
				expl_risk_level = EXCLUDED.expl_risk_level,
				expl_recommendation_reason = EXCLUDED.expl_recommendation_reason`

const clusterQuotaExplSQLColumns = `
				expl_headroom_bp, expl_ns_quota_cpu_sum_mc, expl_ns_quota_mem_sum_bytes,
				expl_base_cpu_mc, expl_max_utilization_bp, expl_recommendation_reason`

const clusterQuotaExplUpdateSet = `
				expl_headroom_bp = EXCLUDED.expl_headroom_bp,
				expl_ns_quota_cpu_sum_mc = EXCLUDED.expl_ns_quota_cpu_sum_mc,
				expl_ns_quota_mem_sum_bytes = EXCLUDED.expl_ns_quota_mem_sum_bytes,
				expl_base_cpu_mc = EXCLUDED.expl_base_cpu_mc,
				expl_max_utilization_bp = EXCLUDED.expl_max_utilization_bp,
				expl_recommendation_reason = EXCLUDED.expl_recommendation_reason`

const snapshotExplSQLColumns = `expl_threshold_used, expl_threshold_name, expl_classification_rule`

const snapshotExplUpdateSet = `
				expl_threshold_used = EXCLUDED.expl_threshold_used,
				expl_threshold_name = EXCLUDED.expl_threshold_name,
				expl_classification_rule = EXCLUDED.expl_classification_rule`

func appendSnapshotExplArgs(args []any, e SnapshotExplanationFactors) []any {
	return append(args,
		nullIntExpl(e.ThresholdUsed),
		nullStringExpl(e.ThresholdName),
		nullStringExpl(e.ClassificationRule),
	)
}

const nodeGPUTimeslicingExplSQLColumns = `
				expl_data_days, expl_candidate_count, expl_impacted_count, expl_classification_rule`

const nodeGPUTimeslicingExplUpdateSet = `
				expl_data_days = EXCLUDED.expl_data_days,
				expl_candidate_count = EXCLUDED.expl_candidate_count,
				expl_impacted_count = EXCLUDED.expl_impacted_count,
				expl_classification_rule = EXCLUDED.expl_classification_rule`

func appendNodeGPUTimeslicingExplArgs(args []any, e NodeGPUTimeslicingExplanationFactors) []any {
	return append(args,
		nullIntExpl(e.DataDays),
		nullIntExpl(e.CandidateCount),
		nullIntExpl(e.ImpactedCount),
		nullStringExpl(e.ClassificationRule),
	)
}

const vmExplSQLColumns = `
				expl_data_days, expl_max_cpu_usage_mc, expl_max_mem_usage_kib,
				expl_cpu_margin_bp, expl_mem_margin_bp,
				expl_raw_recommended_vcpu, expl_raw_recommended_mem_gib,
				expl_downsize_hysteresis_held, expl_guest_agent_used,
				expl_idle_detected, expl_abandoned_detected, expl_power_off_candidate,
				expl_sizing_branch, expl_gpu_action, expl_gpu_rationale`

const vmExplUpdateSet = `
				expl_data_days = EXCLUDED.expl_data_days,
				expl_max_cpu_usage_mc = EXCLUDED.expl_max_cpu_usage_mc,
				expl_max_mem_usage_kib = EXCLUDED.expl_max_mem_usage_kib,
				expl_cpu_margin_bp = EXCLUDED.expl_cpu_margin_bp,
				expl_mem_margin_bp = EXCLUDED.expl_mem_margin_bp,
				expl_raw_recommended_vcpu = EXCLUDED.expl_raw_recommended_vcpu,
				expl_raw_recommended_mem_gib = EXCLUDED.expl_raw_recommended_mem_gib,
				expl_downsize_hysteresis_held = EXCLUDED.expl_downsize_hysteresis_held,
				expl_guest_agent_used = EXCLUDED.expl_guest_agent_used,
				expl_idle_detected = EXCLUDED.expl_idle_detected,
				expl_abandoned_detected = EXCLUDED.expl_abandoned_detected,
				expl_power_off_candidate = EXCLUDED.expl_power_off_candidate,
				expl_sizing_branch = EXCLUDED.expl_sizing_branch,
				expl_gpu_action = EXCLUDED.expl_gpu_action,
				expl_gpu_rationale = EXCLUDED.expl_gpu_rationale`

func appendVMExplArgs(args []any, e VMExplanationFactors) []any {
	return append(args,
		nullIntExpl(e.DataDays),
		nullInt64Expl(e.MaxCPUUsageMC),
		nullInt64Expl(e.MaxMemUsageKiB),
		nullInt32Expl(e.CPUMarginBP),
		nullInt32Expl(e.MemMarginBP),
		nullInt32Expl(e.RawRecommendedVCPU),
		nullInt32Expl(e.RawRecommendedMemGiB),
		e.DownsizeHysteresisHeld,
		e.GuestAgentUsed,
		e.IdleDetected,
		e.AbandonedDetected,
		e.PowerOffCandidate,
		nullStringExpl(e.SizingBranch),
		nullStringExpl(e.GPUAction),
		nullStringExpl(e.GPURationale),
	)
}
