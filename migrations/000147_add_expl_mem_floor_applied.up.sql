-- Add expl_mem_floor_applied column to container and namespace recommendation tables.
-- Mirrors the existing expl_cpu_floor_applied pattern.

ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS expl_mem_floor_applied BOOLEAN;

ALTER TABLE recommendation_history
    ADD COLUMN IF NOT EXISTS expl_mem_floor_applied BOOLEAN;

ALTER TABLE namespace_recommendation_sets
    ADD COLUMN IF NOT EXISTS expl_mem_floor_applied BOOLEAN;

ALTER TABLE historical_namespace_recommendation_sets
    ADD COLUMN IF NOT EXISTS expl_mem_floor_applied BOOLEAN;
