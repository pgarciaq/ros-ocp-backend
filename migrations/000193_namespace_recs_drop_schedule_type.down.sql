-- Restore 000110 rec-table schedule_type (empty BH rows).

ALTER TABLE namespace_recommendation_sets
  ADD COLUMN IF NOT EXISTS schedule_type digest_schedule_type NOT NULL DEFAULT 'all_hours';

ALTER TABLE historical_namespace_recommendation_sets
  ADD COLUMN IF NOT EXISTS schedule_type digest_schedule_type NOT NULL DEFAULT 'all_hours';

DROP INDEX IF EXISTS idx_ns_recs_native_key;
CREATE UNIQUE INDEX idx_ns_recs_native_key
  ON namespace_recommendation_sets (org_id, cluster_uuid, namespace_name, term, engine, schedule_type)
  WHERE term IS NOT NULL;

DROP INDEX IF EXISTS idx_hist_ns_recs_native_key;
CREATE UNIQUE INDEX idx_hist_ns_recs_native_key
  ON historical_namespace_recommendation_sets (org_id, cluster_uuid, namespace_name, term, engine, schedule_type, created_at)
  WHERE term IS NOT NULL;
