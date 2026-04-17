DROP INDEX IF EXISTS idx_recommendation_sets_container_id;
ALTER TABLE recommendation_sets DROP COLUMN IF EXISTS container_id;
