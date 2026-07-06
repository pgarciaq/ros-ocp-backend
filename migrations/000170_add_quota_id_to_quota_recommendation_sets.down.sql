DROP INDEX IF EXISTS idx_quota_recs_quota_id;
ALTER TABLE quota_recommendation_sets DROP COLUMN IF EXISTS quota_id;
