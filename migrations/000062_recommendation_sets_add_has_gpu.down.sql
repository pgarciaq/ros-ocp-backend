DROP INDEX IF EXISTS idx_recommendation_sets_has_gpu;
ALTER TABLE recommendation_sets DROP COLUMN IF EXISTS has_gpu;
