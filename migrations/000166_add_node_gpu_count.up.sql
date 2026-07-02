ALTER TABLE daily_node_digests ADD COLUMN IF NOT EXISTS node_gpu_count INTEGER DEFAULT 0;
