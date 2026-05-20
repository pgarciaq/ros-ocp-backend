-- Denormalize gpu_model_name and gpu_classification into recommendation_sets
-- for SQL-level filtering, fixing pagination correctness when these filters
-- are used. Previously, gpu_model and gpu_classification were post-query filters
-- that produced incomplete pages and wrong total counts.

ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS gpu_model_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS gpu_classification TEXT NOT NULL DEFAULT '';

-- Index for gpu_model_name filtering (partial: only GPU containers have non-empty model).
CREATE INDEX IF NOT EXISTS idx_recommendation_sets_gpu_model_name
    ON recommendation_sets (org_id, gpu_model_name)
    WHERE gpu_model_name != '';

-- Index for gpu_classification filtering (partial: only classified rows).
CREATE INDEX IF NOT EXISTS idx_recommendation_sets_gpu_classification
    ON recommendation_sets (org_id, gpu_classification)
    WHERE gpu_classification != '';
