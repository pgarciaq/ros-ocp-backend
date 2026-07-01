ALTER TABLE daily_node_digests ADD COLUMN IF NOT EXISTS cpu_usage_max_mc BIGINT;
ALTER TABLE daily_node_digests ADD COLUMN IF NOT EXISTS mem_usage_max_kib BIGINT;
