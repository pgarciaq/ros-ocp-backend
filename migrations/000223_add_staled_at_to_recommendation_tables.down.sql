ALTER TABLE recommendation_sets DROP COLUMN IF EXISTS staled_at;

ALTER TABLE namespace_recommendation_sets DROP COLUMN IF EXISTS staled_at;
