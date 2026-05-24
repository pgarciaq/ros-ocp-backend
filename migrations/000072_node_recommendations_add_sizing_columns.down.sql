ALTER TABLE node_recommendations
    DROP COLUMN IF EXISTS recommended_cpu_cores,
    DROP COLUMN IF EXISTS recommended_memory_gib,
    DROP COLUMN IF EXISTS node_count_reduction;
