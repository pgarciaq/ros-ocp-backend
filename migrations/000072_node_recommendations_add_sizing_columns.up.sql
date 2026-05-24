ALTER TABLE node_recommendations
    ADD COLUMN IF NOT EXISTS recommended_cpu_cores REAL,
    ADD COLUMN IF NOT EXISTS recommended_memory_gib REAL,
    ADD COLUMN IF NOT EXISTS node_count_reduction INTEGER NOT NULL DEFAULT 0;
