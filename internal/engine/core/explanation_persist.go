package core

import (
	"strconv"
	"strings"
)

// ContainerExplSQLColumns lists expl_* column names for container/namespace INSERT/UPDATE.
const ContainerExplSQLColumns = `
				expl_data_days, expl_decay_half_life_hours,
				expl_cpu_cost_pct_mc, expl_cpu_perf_pct_mc,
				expl_cpu_usage_p95_mc, expl_cpu_usage_p50_mc, expl_cpu_usage_mean_mc,
				expl_cpu_adaptive_margin_bp, expl_cpu_trend_slope,
				expl_mem_cost_pct_kib, expl_mem_perf_pct_kib,
				expl_mem_usage_p95_kib, expl_mem_usage_p50_kib, expl_mem_usage_mean_kib,
				expl_mem_adaptive_margin_bp, expl_mem_trend_slope,
				expl_oom_count_sum, expl_oom_bump_applied, expl_cpu_floor_applied, expl_mem_floor_applied, expl_is_idle`

// containerExplColCount is the number of columns in ContainerExplSQLColumns.
const containerExplColCount = 21

// ContainerExplUpdateSet is the ON CONFLICT DO UPDATE fragment for container expl columns.
const ContainerExplUpdateSet = `
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
				expl_mem_floor_applied = EXCLUDED.expl_mem_floor_applied,
				expl_is_idle = EXCLUDED.expl_is_idle`

// explPlaceholderCache stores pre-computed placeholder strings keyed by start index.
// Populated at init time for the known call sites; computed on-demand for unexpected values.
var explPlaceholderCache map[int]string

func init() {
	explPlaceholderCache = make(map[int]string, 4)
	for _, start := range []int{18, 25, 31, 47} {
		explPlaceholderCache[start] = buildExplPlaceholders(start)
	}
}

func buildExplPlaceholders(start int) string {
	var b strings.Builder
	b.Grow(containerExplColCount * 4) // "$NN," ≤ 4 bytes each
	for i := 0; i < containerExplColCount; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('$')
		b.WriteString(strconv.Itoa(start + i))
	}
	return b.String()
}

// ContainerExplValuePlaceholders returns $N placeholders for ContainerExplSQLColumns (21 columns).
// Results are pre-computed at init time for the known start offsets (18, 25, 31, 47).
func ContainerExplValuePlaceholders(start int) string {
	if s, ok := explPlaceholderCache[start]; ok {
		return s
	}
	return buildExplPlaceholders(start)
}

func AppendContainerExplArgs(args []any, e ContainerExplanationFactors) []any {
	return append(args,
		NullIntExpl(e.DataDays),
		NullFloatExpl(e.DecayHalfLifeHours),
		NullInt64Expl(e.CPUCostPctMC),
		NullInt64Expl(e.CPUPerfPctMC),
		NullInt64Expl(e.CPUUsageP95MC),
		NullInt64Expl(e.CPUUsageP50MC),
		NullInt64Expl(e.CPUUsageMeanMC),
		NullInt32Expl(e.CPUAdaptiveMarginBP),
		NullFloatExpl(e.CPUTrendSlope),
		NullInt64Expl(e.MemCostPctKiB),
		NullInt64Expl(e.MemPerfPctKiB),
		NullInt64Expl(e.MemUsageP95KiB),
		NullInt64Expl(e.MemUsageP50KiB),
		NullInt64Expl(e.MemUsageMeanKiB),
		NullInt32Expl(e.MemAdaptiveMarginBP),
		NullFloatExpl(e.MemTrendSlope),
		NullInt64Expl(e.OOMCountSum),
		e.OOMBumpApplied,
		e.CPUFloorApplied,
		e.MemFloorApplied,
		e.IsIdle,
	)
}

func NullIntExpl(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func NullInt64Expl(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func NullInt32Expl(v int32) any {
	if v == 0 {
		return nil
	}
	return v
}

func NullFloatExpl(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

func NullStringExpl(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func AppendGPUExplArgs(args []any, e GPUExplanationFactors) []any {
	return append(args,
		NullInt32Expl(e.SMActiveAvgBP),
		NullInt32Expl(e.TensorActiveAvgBP),
		NullInt32Expl(e.DRAMActiveAvgBP),
		NullInt32Expl(e.FBUsageMaxMiB),
		NullInt32Expl(e.FBP98MiB),
		NullStringExpl(e.RecommendedProfile),
		NullStringExpl(e.CurrentProfile),
		e.HasProfilingData,
		e.MemoryBound,
	)
}

const GPUExplUpdateSet = `
				expl_gpu_sm_active_avg_bp = EXCLUDED.expl_gpu_sm_active_avg_bp,
				expl_gpu_tensor_active_avg_bp = EXCLUDED.expl_gpu_tensor_active_avg_bp,
				expl_gpu_dram_active_avg_bp = EXCLUDED.expl_gpu_dram_active_avg_bp,
				expl_gpu_fb_usage_max_mib = EXCLUDED.expl_gpu_fb_usage_max_mib,
				expl_gpu_fb_p98_mib = EXCLUDED.expl_gpu_fb_p98_mib,
				expl_gpu_recommended_profile = EXCLUDED.expl_gpu_recommended_profile,
				expl_gpu_current_profile = EXCLUDED.expl_gpu_current_profile,
				expl_gpu_has_profiling_data = EXCLUDED.expl_gpu_has_profiling_data,
				expl_gpu_memory_bound = EXCLUDED.expl_gpu_memory_bound`

func AppendQuotaExplArgs(args []any, e QuotaExplanationFactors) []any {
	return append(args,
		NullInt32Expl(e.HeadroomBP),
		NullInt64Expl(e.ContainerCPUSumMC),
		NullInt64Expl(e.ContainerMemSumBytes),
		NullInt64Expl(e.SignalCCPUUsedMC),
		NullInt32Expl(e.MaxUtilizationBP),
		NullStringExpl(e.RiskLevel),
		NullStringExpl(e.RecommendationReason),
	)
}

func AppendClusterQuotaExplArgs(args []any, e ClusterQuotaExplanationFactors) []any {
	return append(args,
		NullInt32Expl(e.HeadroomBP),
		NullInt64Expl(e.NSQuotaCPUSumMC),
		NullInt64Expl(e.NSQuotaMemSumBytes),
		NullInt64Expl(e.BaseCPUMC),
		NullInt32Expl(e.MaxUtilizationBP),
		NullStringExpl(e.RecommendationReason),
	)
}

const QuotaExplSQLColumns = `
				expl_headroom_bp, expl_container_cpu_sum_mc, expl_container_mem_sum_bytes,
				expl_signal_c_cpu_used_mc, expl_max_utilization_bp, expl_risk_level, expl_recommendation_reason`

const QuotaExplUpdateSet = `
				expl_headroom_bp = EXCLUDED.expl_headroom_bp,
				expl_container_cpu_sum_mc = EXCLUDED.expl_container_cpu_sum_mc,
				expl_container_mem_sum_bytes = EXCLUDED.expl_container_mem_sum_bytes,
				expl_signal_c_cpu_used_mc = EXCLUDED.expl_signal_c_cpu_used_mc,
				expl_max_utilization_bp = EXCLUDED.expl_max_utilization_bp,
				expl_risk_level = EXCLUDED.expl_risk_level,
				expl_recommendation_reason = EXCLUDED.expl_recommendation_reason`

const ClusterQuotaExplSQLColumns = `
				expl_headroom_bp, expl_ns_quota_cpu_sum_mc, expl_ns_quota_mem_sum_bytes,
				expl_base_cpu_mc, expl_max_utilization_bp, expl_recommendation_reason`

const ClusterQuotaExplUpdateSet = `
				expl_headroom_bp = EXCLUDED.expl_headroom_bp,
				expl_ns_quota_cpu_sum_mc = EXCLUDED.expl_ns_quota_cpu_sum_mc,
				expl_ns_quota_mem_sum_bytes = EXCLUDED.expl_ns_quota_mem_sum_bytes,
				expl_base_cpu_mc = EXCLUDED.expl_base_cpu_mc,
				expl_max_utilization_bp = EXCLUDED.expl_max_utilization_bp,
				expl_recommendation_reason = EXCLUDED.expl_recommendation_reason`

const SnapshotExplSQLColumns = `expl_threshold_used, expl_threshold_name, expl_classification_rule`

const SnapshotExplUpdateSet = `
				expl_threshold_used = EXCLUDED.expl_threshold_used,
				expl_threshold_name = EXCLUDED.expl_threshold_name,
				expl_classification_rule = EXCLUDED.expl_classification_rule`

func AppendSnapshotExplArgs(args []any, e SnapshotExplanationFactors) []any {
	return append(args,
		NullIntExpl(e.ThresholdUsed),
		NullStringExpl(e.ThresholdName),
		NullStringExpl(e.ClassificationRule),
	)
}

const NodeGPUTimeslicingExplSQLColumns = `
				expl_data_days, expl_candidate_count, expl_impacted_count, expl_classification_rule`

const NodeGPUTimeslicingExplUpdateSet = `
				expl_data_days = EXCLUDED.expl_data_days,
				expl_candidate_count = EXCLUDED.expl_candidate_count,
				expl_impacted_count = EXCLUDED.expl_impacted_count,
				expl_classification_rule = EXCLUDED.expl_classification_rule`

func AppendNodeGPUTimeslicingExplArgs(args []any, e NodeGPUTimeslicingExplanationFactors) []any {
	return append(args,
		NullIntExpl(e.DataDays),
		NullIntExpl(e.CandidateCount),
		NullIntExpl(e.ImpactedCount),
		NullStringExpl(e.ClassificationRule),
	)
}

const VMExplSQLColumns = `
				expl_data_days, expl_max_cpu_usage_mc, expl_max_mem_usage_kib,
				expl_cpu_margin_bp, expl_mem_margin_bp,
				expl_raw_recommended_vcpu, expl_raw_recommended_mem_gib,
				expl_downsize_hysteresis_held, expl_guest_agent_used,
				expl_idle_detected, expl_abandoned_detected, expl_power_off_candidate,
				expl_sizing_branch, expl_gpu_action, expl_gpu_rationale`

const VMExplUpdateSet = `
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

func AppendVMExplArgs(args []any, e VMExplanationFactors) []any {
	return append(args,
		NullIntExpl(e.DataDays),
		NullInt64Expl(e.MaxCPUUsageMC),
		NullInt64Expl(e.MaxMemUsageKiB),
		NullInt32Expl(e.CPUMarginBP),
		NullInt32Expl(e.MemMarginBP),
		NullInt32Expl(e.RawRecommendedVCPU),
		NullInt32Expl(e.RawRecommendedMemGiB),
		e.DownsizeHysteresisHeld,
		e.GuestAgentUsed,
		e.IdleDetected,
		e.AbandonedDetected,
		e.PowerOffCandidate,
		NullStringExpl(e.SizingBranch),
		NullStringExpl(e.GPUAction),
		NullStringExpl(e.GPURationale),
	)
}
