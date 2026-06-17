-- ADR-0296: typed explanation columns for all native-engine recommendation tables.

-- Container CPU/memory (recommendation_sets + recommendation_history)
ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS expl_data_days INTEGER,
    ADD COLUMN IF NOT EXISTS expl_decay_half_life_hours REAL,
    ADD COLUMN IF NOT EXISTS expl_cpu_cost_pct_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_perf_pct_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_p95_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_p50_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_mean_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_adaptive_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_cpu_trend_slope REAL,
    ADD COLUMN IF NOT EXISTS expl_mem_cost_pct_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_perf_pct_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_p95_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_p50_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_mean_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_adaptive_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_mem_trend_slope REAL,
    ADD COLUMN IF NOT EXISTS expl_oom_count_sum BIGINT,
    ADD COLUMN IF NOT EXISTS expl_oom_bump_applied BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_cpu_floor_applied BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_is_idle BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_gpu_sm_active_avg_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_gpu_tensor_active_avg_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_gpu_dram_active_avg_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_gpu_fb_usage_max_mib INTEGER,
    ADD COLUMN IF NOT EXISTS expl_gpu_fb_p98_mib INTEGER,
    ADD COLUMN IF NOT EXISTS expl_gpu_recommended_profile TEXT,
    ADD COLUMN IF NOT EXISTS expl_gpu_current_profile TEXT,
    ADD COLUMN IF NOT EXISTS expl_gpu_has_profiling_data BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_gpu_memory_bound BOOLEAN;

ALTER TABLE recommendation_history
    ADD COLUMN IF NOT EXISTS expl_data_days INTEGER,
    ADD COLUMN IF NOT EXISTS expl_decay_half_life_hours REAL,
    ADD COLUMN IF NOT EXISTS expl_cpu_cost_pct_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_perf_pct_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_p95_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_p50_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_mean_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_adaptive_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_cpu_trend_slope REAL,
    ADD COLUMN IF NOT EXISTS expl_mem_cost_pct_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_perf_pct_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_p95_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_p50_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_mean_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_adaptive_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_mem_trend_slope REAL,
    ADD COLUMN IF NOT EXISTS expl_oom_count_sum BIGINT,
    ADD COLUMN IF NOT EXISTS expl_oom_bump_applied BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_cpu_floor_applied BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_is_idle BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_gpu_sm_active_avg_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_gpu_tensor_active_avg_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_gpu_dram_active_avg_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_gpu_fb_usage_max_mib INTEGER,
    ADD COLUMN IF NOT EXISTS expl_gpu_fb_p98_mib INTEGER,
    ADD COLUMN IF NOT EXISTS expl_gpu_recommended_profile TEXT,
    ADD COLUMN IF NOT EXISTS expl_gpu_current_profile TEXT,
    ADD COLUMN IF NOT EXISTS expl_gpu_has_profiling_data BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_gpu_memory_bound BOOLEAN;

-- Namespace CPU/memory
ALTER TABLE namespace_recommendation_sets
    ADD COLUMN IF NOT EXISTS expl_data_days INTEGER,
    ADD COLUMN IF NOT EXISTS expl_decay_half_life_hours REAL,
    ADD COLUMN IF NOT EXISTS expl_cpu_cost_pct_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_perf_pct_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_p95_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_p50_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_mean_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_adaptive_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_cpu_trend_slope REAL,
    ADD COLUMN IF NOT EXISTS expl_mem_cost_pct_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_perf_pct_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_p95_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_p50_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_mean_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_adaptive_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_mem_trend_slope REAL,
    ADD COLUMN IF NOT EXISTS expl_oom_count_sum BIGINT,
    ADD COLUMN IF NOT EXISTS expl_oom_bump_applied BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_cpu_floor_applied BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_is_idle BOOLEAN;

ALTER TABLE historical_namespace_recommendation_sets
    ADD COLUMN IF NOT EXISTS expl_data_days INTEGER,
    ADD COLUMN IF NOT EXISTS expl_decay_half_life_hours REAL,
    ADD COLUMN IF NOT EXISTS expl_cpu_cost_pct_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_perf_pct_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_p95_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_p50_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_usage_mean_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_adaptive_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_cpu_trend_slope REAL,
    ADD COLUMN IF NOT EXISTS expl_mem_cost_pct_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_perf_pct_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_p95_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_p50_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_usage_mean_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_mem_adaptive_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_mem_trend_slope REAL,
    ADD COLUMN IF NOT EXISTS expl_oom_count_sum BIGINT,
    ADD COLUMN IF NOT EXISTS expl_oom_bump_applied BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_cpu_floor_applied BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_is_idle BOOLEAN;

-- Node CPU/memory
ALTER TABLE node_recommendations
    ADD COLUMN IF NOT EXISTS expl_data_days INTEGER,
    ADD COLUMN IF NOT EXISTS expl_target_utilization_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_current_cpu_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_current_mem_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_max_cpu_usage_p95_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_max_mem_usage_p95_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_pod_scheduling_headroom_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_ema_imbalance_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_consolidation_applied BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_sizing_formula TEXT;

-- PVC/storage
ALTER TABLE pvc_recommendation_sets
    ADD COLUMN IF NOT EXISTS expl_data_days INTEGER,
    ADD COLUMN IF NOT EXISTS expl_oversized_threshold_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_near_full_threshold_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_recommended_size_multiplier INTEGER,
    ADD COLUMN IF NOT EXISTS expl_min_recommended_gib INTEGER,
    ADD COLUMN IF NOT EXISTS expl_classification_reason TEXT;

-- Namespace quota
ALTER TABLE quota_recommendation_sets
    ADD COLUMN IF NOT EXISTS expl_headroom_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_container_cpu_sum_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_container_mem_sum_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS expl_signal_c_cpu_used_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_max_utilization_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_risk_level TEXT,
    ADD COLUMN IF NOT EXISTS expl_recommendation_reason TEXT;

ALTER TABLE quota_recommendation_history
    ADD COLUMN IF NOT EXISTS expl_headroom_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_container_cpu_sum_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_container_mem_sum_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS expl_signal_c_cpu_used_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_max_utilization_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_risk_level TEXT,
    ADD COLUMN IF NOT EXISTS expl_recommendation_reason TEXT;

-- Cluster resource quota
ALTER TABLE cluster_quota_recommendation_sets
    ADD COLUMN IF NOT EXISTS expl_headroom_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_ns_quota_cpu_sum_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_ns_quota_mem_sum_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS expl_base_cpu_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_max_utilization_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_recommendation_reason TEXT;

ALTER TABLE cluster_quota_recommendation_history
    ADD COLUMN IF NOT EXISTS expl_headroom_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_ns_quota_cpu_sum_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_ns_quota_mem_sum_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS expl_base_cpu_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_max_utilization_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_recommendation_reason TEXT;

-- VM
ALTER TABLE vm_recommendations
    ADD COLUMN IF NOT EXISTS expl_data_days INTEGER,
    ADD COLUMN IF NOT EXISTS expl_max_cpu_usage_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_max_mem_usage_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_mem_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_raw_recommended_vcpu INTEGER,
    ADD COLUMN IF NOT EXISTS expl_raw_recommended_mem_gib INTEGER,
    ADD COLUMN IF NOT EXISTS expl_downsize_hysteresis_held BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_guest_agent_used BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_idle_detected BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_abandoned_detected BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_power_off_candidate BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_sizing_branch TEXT,
    ADD COLUMN IF NOT EXISTS expl_gpu_action TEXT,
    ADD COLUMN IF NOT EXISTS expl_gpu_rationale TEXT;

ALTER TABLE vm_recommendation_history
    ADD COLUMN IF NOT EXISTS expl_data_days INTEGER,
    ADD COLUMN IF NOT EXISTS expl_max_cpu_usage_mc BIGINT,
    ADD COLUMN IF NOT EXISTS expl_max_mem_usage_kib BIGINT,
    ADD COLUMN IF NOT EXISTS expl_cpu_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_mem_margin_bp INTEGER,
    ADD COLUMN IF NOT EXISTS expl_raw_recommended_vcpu INTEGER,
    ADD COLUMN IF NOT EXISTS expl_raw_recommended_mem_gib INTEGER,
    ADD COLUMN IF NOT EXISTS expl_downsize_hysteresis_held BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_guest_agent_used BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_idle_detected BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_abandoned_detected BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_power_off_candidate BOOLEAN,
    ADD COLUMN IF NOT EXISTS expl_sizing_branch TEXT,
    ADD COLUMN IF NOT EXISTS expl_gpu_action TEXT,
    ADD COLUMN IF NOT EXISTS expl_gpu_rationale TEXT;

-- Snapshot
ALTER TABLE snapshot_recommendation_sets
    ADD COLUMN IF NOT EXISTS expl_threshold_used INTEGER,
    ADD COLUMN IF NOT EXISTS expl_threshold_name TEXT,
    ADD COLUMN IF NOT EXISTS expl_classification_rule TEXT;

-- Node GPU time-slicing
ALTER TABLE node_gpu_timeslicing_recommendations
    ADD COLUMN IF NOT EXISTS expl_data_days INTEGER,
    ADD COLUMN IF NOT EXISTS expl_candidate_count INTEGER,
    ADD COLUMN IF NOT EXISTS expl_impacted_count INTEGER,
    ADD COLUMN IF NOT EXISTS expl_classification_rule TEXT;

ALTER TABLE node_gpu_timeslicing_recommendation_history
    ADD COLUMN IF NOT EXISTS expl_data_days INTEGER,
    ADD COLUMN IF NOT EXISTS expl_candidate_count INTEGER,
    ADD COLUMN IF NOT EXISTS expl_impacted_count INTEGER,
    ADD COLUMN IF NOT EXISTS expl_classification_rule TEXT;
