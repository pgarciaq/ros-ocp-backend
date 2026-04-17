DROP INDEX IF EXISTS idx_ns_recs_namespace_id;
DROP INDEX IF EXISTS idx_ns_recs_native_key;

ALTER TABLE namespace_recommendation_sets
  DROP COLUMN IF EXISTS cluster_uuid,
  DROP COLUMN IF EXISTS term,
  DROP COLUMN IF EXISTS engine,
  DROP COLUMN IF EXISTS namespace_id,
  DROP COLUMN IF EXISTS rec_cpu_request_millicores,
  DROP COLUMN IF EXISTS rec_cpu_limit_millicores,
  DROP COLUMN IF EXISTS rec_memory_request_kib,
  DROP COLUMN IF EXISTS rec_memory_limit_kib,
  DROP COLUMN IF EXISTS current_cpu_request_millicores,
  DROP COLUMN IF EXISTS current_cpu_limit_millicores,
  DROP COLUMN IF EXISTS current_memory_request_kib,
  DROP COLUMN IF EXISTS current_memory_limit_kib,
  DROP COLUMN IF EXISTS variation_cpu_request_pct,
  DROP COLUMN IF EXISTS variation_memory_request_pct,
  DROP COLUMN IF EXISTS notification_codes,
  DROP COLUMN IF EXISTS confidence_level,
  DROP COLUMN IF EXISTS stale;
