DELETE FROM historical_namespace_recommendation_sets WHERE schedule_type = 'business_hours';
DELETE FROM namespace_recommendation_sets WHERE schedule_type = 'business_hours';

DROP INDEX IF EXISTS idx_hist_ns_recs_native_key;
CREATE UNIQUE INDEX idx_hist_ns_recs_native_key
  ON historical_namespace_recommendation_sets (org_id, cluster_uuid, namespace_name, term, engine, created_at)
  WHERE term IS NOT NULL;

DROP INDEX IF EXISTS idx_ns_recs_native_key;
CREATE UNIQUE INDEX idx_ns_recs_native_key
  ON namespace_recommendation_sets (org_id, cluster_uuid, namespace_name, term, engine)
  WHERE term IS NOT NULL;

ALTER TABLE historical_namespace_recommendation_sets DROP COLUMN IF EXISTS schedule_type;
ALTER TABLE namespace_recommendation_sets DROP COLUMN IF EXISTS schedule_type;
