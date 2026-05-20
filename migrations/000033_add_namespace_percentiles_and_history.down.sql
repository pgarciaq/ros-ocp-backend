-- Rollback migration 000033: remove namespace percentiles and native history columns.

-- Drop namespace percentile columns.
ALTER TABLE daily_namespace_digests
  DROP COLUMN IF EXISTS cpu_request_p60_mc,
  DROP COLUMN IF EXISTS cpu_request_p99_mc,
  DROP COLUMN IF EXISTS cpu_usage_p60_mc,
  DROP COLUMN IF EXISTS cpu_usage_p98_mc,
  DROP COLUMN IF EXISTS cpu_usage_p99_mc;

-- Drop native history index.
DROP INDEX IF EXISTS idx_hist_ns_recs_native_key;

-- Delete native rows (term IS NOT NULL) before re-applying NOT NULL
-- constraints, because native rows may have NULL workload_id/recommendations.
DELETE FROM historical_namespace_recommendation_sets WHERE term IS NOT NULL;

-- Re-apply NOT NULL constraints (safe now that native rows are gone).
ALTER TABLE historical_namespace_recommendation_sets
  ALTER COLUMN recommendations SET NOT NULL;
ALTER TABLE historical_namespace_recommendation_sets
  ALTER COLUMN workload_id SET NOT NULL;
ALTER TABLE historical_namespace_recommendation_sets
  ALTER COLUMN monitoring_start_time SET NOT NULL;

-- Drop native history columns.
ALTER TABLE historical_namespace_recommendation_sets
  DROP COLUMN IF EXISTS cluster_uuid,
  DROP COLUMN IF EXISTS namespace_id,
  DROP COLUMN IF EXISTS term,
  DROP COLUMN IF EXISTS engine,
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
  DROP COLUMN IF EXISTS confidence_level;
