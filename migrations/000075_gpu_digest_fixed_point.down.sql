ALTER TABLE gpu_container_digests
    ALTER COLUMN fb_usage_min_mib TYPE REAL USING COALESCE(fb_usage_min_mib, 0)::REAL,
    ALTER COLUMN fb_usage_max_mib TYPE REAL USING COALESCE(fb_usage_max_mib, 0)::REAL,
    ALTER COLUMN fb_usage_avg_mib TYPE REAL USING COALESCE(fb_usage_avg_mib, 0)::REAL,
    ALTER COLUMN tensor_pipe_active_min TYPE REAL USING (COALESCE(tensor_pipe_active_min, 0)::REAL / 10000.0),
    ALTER COLUMN tensor_pipe_active_max TYPE REAL USING (COALESCE(tensor_pipe_active_max, 0)::REAL / 10000.0),
    ALTER COLUMN tensor_pipe_active_avg TYPE REAL USING (COALESCE(tensor_pipe_active_avg, 0)::REAL / 10000.0),
    ALTER COLUMN dram_active_min TYPE REAL USING (COALESCE(dram_active_min, 0)::REAL / 10000.0),
    ALTER COLUMN dram_active_max TYPE REAL USING (COALESCE(dram_active_max, 0)::REAL / 10000.0),
    ALTER COLUMN dram_active_avg TYPE REAL USING (COALESCE(dram_active_avg, 0)::REAL / 10000.0),
    ALTER COLUMN sm_active_min TYPE REAL USING (COALESCE(sm_active_min, 0)::REAL / 10000.0),
    ALTER COLUMN sm_active_max TYPE REAL USING (COALESCE(sm_active_max, 0)::REAL / 10000.0),
    ALTER COLUMN sm_active_avg TYPE REAL USING (COALESCE(sm_active_avg, 0)::REAL / 10000.0);
