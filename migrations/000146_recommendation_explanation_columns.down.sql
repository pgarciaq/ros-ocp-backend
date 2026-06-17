ALTER TABLE node_gpu_timeslicing_recommendation_history
    DROP COLUMN IF EXISTS expl_classification_rule,
    DROP COLUMN IF EXISTS expl_impacted_count,
    DROP COLUMN IF EXISTS expl_candidate_count,
    DROP COLUMN IF EXISTS expl_data_days;

ALTER TABLE node_gpu_timeslicing_recommendations
    DROP COLUMN IF EXISTS expl_classification_rule,
    DROP COLUMN IF EXISTS expl_impacted_count,
    DROP COLUMN IF EXISTS expl_candidate_count,
    DROP COLUMN IF EXISTS expl_data_days;

ALTER TABLE snapshot_recommendation_sets
    DROP COLUMN IF EXISTS expl_classification_rule,
    DROP COLUMN IF EXISTS expl_threshold_name,
    DROP COLUMN IF EXISTS expl_threshold_used;

ALTER TABLE vm_recommendation_history
    DROP COLUMN IF EXISTS expl_gpu_rationale,
    DROP COLUMN IF EXISTS expl_gpu_action,
    DROP COLUMN IF EXISTS expl_sizing_branch,
    DROP COLUMN IF EXISTS expl_power_off_candidate,
    DROP COLUMN IF EXISTS expl_abandoned_detected,
    DROP COLUMN IF EXISTS expl_idle_detected,
    DROP COLUMN IF EXISTS expl_guest_agent_used,
    DROP COLUMN IF EXISTS expl_downsize_hysteresis_held,
    DROP COLUMN IF EXISTS expl_raw_recommended_mem_gib,
    DROP COLUMN IF EXISTS expl_raw_recommended_vcpu,
    DROP COLUMN IF EXISTS expl_mem_margin_bp,
    DROP COLUMN IF EXISTS expl_cpu_margin_bp,
    DROP COLUMN IF EXISTS expl_max_mem_usage_kib,
    DROP COLUMN IF EXISTS expl_max_cpu_usage_mc,
    DROP COLUMN IF EXISTS expl_data_days;

ALTER TABLE vm_recommendations
    DROP COLUMN IF EXISTS expl_gpu_rationale,
    DROP COLUMN IF EXISTS expl_gpu_action,
    DROP COLUMN IF EXISTS expl_sizing_branch,
    DROP COLUMN IF EXISTS expl_power_off_candidate,
    DROP COLUMN IF EXISTS expl_abandoned_detected,
    DROP COLUMN IF EXISTS expl_idle_detected,
    DROP COLUMN IF EXISTS expl_guest_agent_used,
    DROP COLUMN IF EXISTS expl_downsize_hysteresis_held,
    DROP COLUMN IF EXISTS expl_raw_recommended_mem_gib,
    DROP COLUMN IF EXISTS expl_raw_recommended_vcpu,
    DROP COLUMN IF EXISTS expl_mem_margin_bp,
    DROP COLUMN IF EXISTS expl_cpu_margin_bp,
    DROP COLUMN IF EXISTS expl_max_mem_usage_kib,
    DROP COLUMN IF EXISTS expl_max_cpu_usage_mc,
    DROP COLUMN IF EXISTS expl_data_days;

ALTER TABLE cluster_quota_recommendation_history
    DROP COLUMN IF EXISTS expl_recommendation_reason,
    DROP COLUMN IF EXISTS expl_max_utilization_bp,
    DROP COLUMN IF EXISTS expl_base_cpu_mc,
    DROP COLUMN IF EXISTS expl_ns_quota_mem_sum_bytes,
    DROP COLUMN IF EXISTS expl_ns_quota_cpu_sum_mc,
    DROP COLUMN IF EXISTS expl_headroom_bp;

ALTER TABLE cluster_quota_recommendation_sets
    DROP COLUMN IF EXISTS expl_recommendation_reason,
    DROP COLUMN IF EXISTS expl_max_utilization_bp,
    DROP COLUMN IF EXISTS expl_base_cpu_mc,
    DROP COLUMN IF EXISTS expl_ns_quota_mem_sum_bytes,
    DROP COLUMN IF EXISTS expl_ns_quota_cpu_sum_mc,
    DROP COLUMN IF EXISTS expl_headroom_bp;

ALTER TABLE quota_recommendation_history
    DROP COLUMN IF EXISTS expl_recommendation_reason,
    DROP COLUMN IF EXISTS expl_risk_level,
    DROP COLUMN IF EXISTS expl_max_utilization_bp,
    DROP COLUMN IF EXISTS expl_signal_c_cpu_used_mc,
    DROP COLUMN IF EXISTS expl_container_mem_sum_bytes,
    DROP COLUMN IF EXISTS expl_container_cpu_sum_mc,
    DROP COLUMN IF EXISTS expl_headroom_bp;

ALTER TABLE quota_recommendation_sets
    DROP COLUMN IF EXISTS expl_recommendation_reason,
    DROP COLUMN IF EXISTS expl_risk_level,
    DROP COLUMN IF EXISTS expl_max_utilization_bp,
    DROP COLUMN IF EXISTS expl_signal_c_cpu_used_mc,
    DROP COLUMN IF EXISTS expl_container_mem_sum_bytes,
    DROP COLUMN IF EXISTS expl_container_cpu_sum_mc,
    DROP COLUMN IF EXISTS expl_headroom_bp;

ALTER TABLE pvc_recommendation_sets
    DROP COLUMN IF EXISTS expl_classification_reason,
    DROP COLUMN IF EXISTS expl_min_recommended_gib,
    DROP COLUMN IF EXISTS expl_recommended_size_multiplier,
    DROP COLUMN IF EXISTS expl_near_full_threshold_bp,
    DROP COLUMN IF EXISTS expl_oversized_threshold_bp,
    DROP COLUMN IF EXISTS expl_data_days;

ALTER TABLE node_recommendations
    DROP COLUMN IF EXISTS expl_sizing_formula,
    DROP COLUMN IF EXISTS expl_consolidation_applied,
    DROP COLUMN IF EXISTS expl_ema_imbalance_bp,
    DROP COLUMN IF EXISTS expl_pod_scheduling_headroom_bp,
    DROP COLUMN IF EXISTS expl_max_mem_usage_p95_kib,
    DROP COLUMN IF EXISTS expl_max_cpu_usage_p95_mc,
    DROP COLUMN IF EXISTS expl_current_mem_kib,
    DROP COLUMN IF EXISTS expl_current_cpu_mc,
    DROP COLUMN IF EXISTS expl_target_utilization_bp,
    DROP COLUMN IF EXISTS expl_data_days;

ALTER TABLE historical_namespace_recommendation_sets
    DROP COLUMN IF EXISTS expl_is_idle,
    DROP COLUMN IF EXISTS expl_cpu_floor_applied,
    DROP COLUMN IF EXISTS expl_oom_bump_applied,
    DROP COLUMN IF EXISTS expl_oom_count_sum,
    DROP COLUMN IF EXISTS expl_mem_trend_slope,
    DROP COLUMN IF EXISTS expl_mem_adaptive_margin_bp,
    DROP COLUMN IF EXISTS expl_mem_usage_mean_kib,
    DROP COLUMN IF EXISTS expl_mem_usage_p50_kib,
    DROP COLUMN IF EXISTS expl_mem_usage_p95_kib,
    DROP COLUMN IF EXISTS expl_mem_perf_pct_kib,
    DROP COLUMN IF EXISTS expl_mem_cost_pct_kib,
    DROP COLUMN IF EXISTS expl_cpu_trend_slope,
    DROP COLUMN IF EXISTS expl_cpu_adaptive_margin_bp,
    DROP COLUMN IF EXISTS expl_cpu_usage_mean_mc,
    DROP COLUMN IF EXISTS expl_cpu_usage_p50_mc,
    DROP COLUMN IF EXISTS expl_cpu_usage_p95_mc,
    DROP COLUMN IF EXISTS expl_cpu_perf_pct_mc,
    DROP COLUMN IF EXISTS expl_cpu_cost_pct_mc,
    DROP COLUMN IF EXISTS expl_decay_half_life_hours,
    DROP COLUMN IF EXISTS expl_data_days;

ALTER TABLE namespace_recommendation_sets
    DROP COLUMN IF EXISTS expl_is_idle,
    DROP COLUMN IF EXISTS expl_cpu_floor_applied,
    DROP COLUMN IF EXISTS expl_oom_bump_applied,
    DROP COLUMN IF EXISTS expl_oom_count_sum,
    DROP COLUMN IF EXISTS expl_mem_trend_slope,
    DROP COLUMN IF EXISTS expl_mem_adaptive_margin_bp,
    DROP COLUMN IF EXISTS expl_mem_usage_mean_kib,
    DROP COLUMN IF EXISTS expl_mem_usage_p50_kib,
    DROP COLUMN IF EXISTS expl_mem_usage_p95_kib,
    DROP COLUMN IF EXISTS expl_mem_perf_pct_kib,
    DROP COLUMN IF EXISTS expl_mem_cost_pct_kib,
    DROP COLUMN IF EXISTS expl_cpu_trend_slope,
    DROP COLUMN IF EXISTS expl_cpu_adaptive_margin_bp,
    DROP COLUMN IF EXISTS expl_cpu_usage_mean_mc,
    DROP COLUMN IF EXISTS expl_cpu_usage_p50_mc,
    DROP COLUMN IF EXISTS expl_cpu_usage_p95_mc,
    DROP COLUMN IF EXISTS expl_cpu_perf_pct_mc,
    DROP COLUMN IF EXISTS expl_cpu_cost_pct_mc,
    DROP COLUMN IF EXISTS expl_decay_half_life_hours,
    DROP COLUMN IF EXISTS expl_data_days;

ALTER TABLE recommendation_history
    DROP COLUMN IF EXISTS expl_gpu_memory_bound,
    DROP COLUMN IF EXISTS expl_gpu_has_profiling_data,
    DROP COLUMN IF EXISTS expl_gpu_current_profile,
    DROP COLUMN IF EXISTS expl_gpu_recommended_profile,
    DROP COLUMN IF EXISTS expl_gpu_fb_p98_mib,
    DROP COLUMN IF EXISTS expl_gpu_fb_usage_max_mib,
    DROP COLUMN IF EXISTS expl_gpu_dram_active_avg_bp,
    DROP COLUMN IF EXISTS expl_gpu_tensor_active_avg_bp,
    DROP COLUMN IF EXISTS expl_gpu_sm_active_avg_bp,
    DROP COLUMN IF EXISTS expl_is_idle,
    DROP COLUMN IF EXISTS expl_cpu_floor_applied,
    DROP COLUMN IF EXISTS expl_oom_bump_applied,
    DROP COLUMN IF EXISTS expl_oom_count_sum,
    DROP COLUMN IF EXISTS expl_mem_trend_slope,
    DROP COLUMN IF EXISTS expl_mem_adaptive_margin_bp,
    DROP COLUMN IF EXISTS expl_mem_usage_mean_kib,
    DROP COLUMN IF EXISTS expl_mem_usage_p50_kib,
    DROP COLUMN IF EXISTS expl_mem_usage_p95_kib,
    DROP COLUMN IF EXISTS expl_mem_perf_pct_kib,
    DROP COLUMN IF EXISTS expl_mem_cost_pct_kib,
    DROP COLUMN IF EXISTS expl_cpu_trend_slope,
    DROP COLUMN IF EXISTS expl_cpu_adaptive_margin_bp,
    DROP COLUMN IF EXISTS expl_cpu_usage_mean_mc,
    DROP COLUMN IF EXISTS expl_cpu_usage_p50_mc,
    DROP COLUMN IF EXISTS expl_cpu_usage_p95_mc,
    DROP COLUMN IF EXISTS expl_cpu_perf_pct_mc,
    DROP COLUMN IF EXISTS expl_cpu_cost_pct_mc,
    DROP COLUMN IF EXISTS expl_decay_half_life_hours,
    DROP COLUMN IF EXISTS expl_data_days;

ALTER TABLE recommendation_sets
    DROP COLUMN IF EXISTS expl_gpu_memory_bound,
    DROP COLUMN IF EXISTS expl_gpu_has_profiling_data,
    DROP COLUMN IF EXISTS expl_gpu_current_profile,
    DROP COLUMN IF EXISTS expl_gpu_recommended_profile,
    DROP COLUMN IF EXISTS expl_gpu_fb_p98_mib,
    DROP COLUMN IF EXISTS expl_gpu_fb_usage_max_mib,
    DROP COLUMN IF EXISTS expl_gpu_dram_active_avg_bp,
    DROP COLUMN IF EXISTS expl_gpu_tensor_active_avg_bp,
    DROP COLUMN IF EXISTS expl_gpu_sm_active_avg_bp,
    DROP COLUMN IF EXISTS expl_is_idle,
    DROP COLUMN IF EXISTS expl_cpu_floor_applied,
    DROP COLUMN IF EXISTS expl_oom_bump_applied,
    DROP COLUMN IF EXISTS expl_oom_count_sum,
    DROP COLUMN IF EXISTS expl_mem_trend_slope,
    DROP COLUMN IF EXISTS expl_mem_adaptive_margin_bp,
    DROP COLUMN IF EXISTS expl_mem_usage_mean_kib,
    DROP COLUMN IF EXISTS expl_mem_usage_p50_kib,
    DROP COLUMN IF EXISTS expl_mem_usage_p95_kib,
    DROP COLUMN IF EXISTS expl_mem_perf_pct_kib,
    DROP COLUMN IF EXISTS expl_mem_cost_pct_kib,
    DROP COLUMN IF EXISTS expl_cpu_trend_slope,
    DROP COLUMN IF EXISTS expl_cpu_adaptive_margin_bp,
    DROP COLUMN IF EXISTS expl_cpu_usage_mean_mc,
    DROP COLUMN IF EXISTS expl_cpu_usage_p50_mc,
    DROP COLUMN IF EXISTS expl_cpu_usage_p95_mc,
    DROP COLUMN IF EXISTS expl_cpu_perf_pct_mc,
    DROP COLUMN IF EXISTS expl_cpu_cost_pct_mc,
    DROP COLUMN IF EXISTS expl_decay_half_life_hours,
    DROP COLUMN IF EXISTS expl_data_days;
