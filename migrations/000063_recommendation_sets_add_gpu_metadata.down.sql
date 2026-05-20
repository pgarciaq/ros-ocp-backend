DROP INDEX IF EXISTS idx_recommendation_sets_gpu_classification;
DROP INDEX IF EXISTS idx_recommendation_sets_gpu_model_name;
ALTER TABLE recommendation_sets DROP COLUMN IF EXISTS gpu_classification;
ALTER TABLE recommendation_sets DROP COLUMN IF EXISTS gpu_model_name;
