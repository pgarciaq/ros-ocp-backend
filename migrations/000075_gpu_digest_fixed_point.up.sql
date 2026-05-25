-- GPU digest metrics: utilization as basis points (0-10000), frame buffer as MiB integers.

ALTER TABLE gpu_container_digests
    ALTER COLUMN fb_usage_min_mib TYPE INTEGER USING (ROUND(COALESCE(fb_usage_min_mib, 0)))::INTEGER,
    ALTER COLUMN fb_usage_max_mib TYPE INTEGER USING (ROUND(COALESCE(fb_usage_max_mib, 0)))::INTEGER,
    ALTER COLUMN fb_usage_avg_mib TYPE INTEGER USING (ROUND(COALESCE(fb_usage_avg_mib, 0)))::INTEGER,
    ALTER COLUMN tensor_pipe_active_min TYPE INTEGER USING (ROUND(COALESCE(tensor_pipe_active_min, 0) * 10000))::INTEGER,
    ALTER COLUMN tensor_pipe_active_max TYPE INTEGER USING (ROUND(COALESCE(tensor_pipe_active_max, 0) * 10000))::INTEGER,
    ALTER COLUMN tensor_pipe_active_avg TYPE INTEGER USING (ROUND(COALESCE(tensor_pipe_active_avg, 0) * 10000))::INTEGER,
    ALTER COLUMN dram_active_min TYPE INTEGER USING (ROUND(COALESCE(dram_active_min, 0) * 10000))::INTEGER,
    ALTER COLUMN dram_active_max TYPE INTEGER USING (ROUND(COALESCE(dram_active_max, 0) * 10000))::INTEGER,
    ALTER COLUMN dram_active_avg TYPE INTEGER USING (ROUND(COALESCE(dram_active_avg, 0) * 10000))::INTEGER,
    ALTER COLUMN sm_active_min TYPE INTEGER USING (ROUND(COALESCE(sm_active_min, 0) * 10000))::INTEGER,
    ALTER COLUMN sm_active_max TYPE INTEGER USING (ROUND(COALESCE(sm_active_max, 0) * 10000))::INTEGER,
    ALTER COLUMN sm_active_avg TYPE INTEGER USING (ROUND(COALESCE(sm_active_avg, 0) * 10000))::INTEGER;
