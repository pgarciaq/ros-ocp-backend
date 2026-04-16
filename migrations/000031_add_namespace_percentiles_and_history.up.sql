-- Migration 000031: Add exact percentile columns to namespace digests,
-- and native relational columns to historical_namespace_recommendation_sets.

-- 1. Add missing percentile columns to daily_namespace_digests so the engine
--    can use exact P60/P98/P99 instead of approximating from P50/P95/Max.
ALTER TABLE daily_namespace_digests
  ADD COLUMN IF NOT EXISTS cpu_request_p60_mc  BIGINT,
  ADD COLUMN IF NOT EXISTS cpu_request_p99_mc  BIGINT,
  ADD COLUMN IF NOT EXISTS cpu_usage_p60_mc    BIGINT,
  ADD COLUMN IF NOT EXISTS cpu_usage_p98_mc    BIGINT,
  ADD COLUMN IF NOT EXISTS cpu_usage_p99_mc    BIGINT;

-- 2. Add native relational columns to historical_namespace_recommendation_sets.
--    Native rows are identified by term IS NOT NULL; legacy rows keep term NULL.
ALTER TABLE historical_namespace_recommendation_sets
  ADD COLUMN IF NOT EXISTS cluster_uuid                  UUID,
  ADD COLUMN IF NOT EXISTS namespace_id                  TEXT,
  ADD COLUMN IF NOT EXISTS term                          TEXT,
  ADD COLUMN IF NOT EXISTS engine                        TEXT,
  ADD COLUMN IF NOT EXISTS rec_cpu_request_millicores    BIGINT,
  ADD COLUMN IF NOT EXISTS rec_cpu_limit_millicores      BIGINT,
  ADD COLUMN IF NOT EXISTS rec_memory_request_kib        BIGINT,
  ADD COLUMN IF NOT EXISTS rec_memory_limit_kib          BIGINT,
  ADD COLUMN IF NOT EXISTS current_cpu_request_millicores BIGINT,
  ADD COLUMN IF NOT EXISTS current_cpu_limit_millicores  BIGINT,
  ADD COLUMN IF NOT EXISTS current_memory_request_kib    BIGINT,
  ADD COLUMN IF NOT EXISTS current_memory_limit_kib      BIGINT,
  ADD COLUMN IF NOT EXISTS variation_cpu_request_pct     REAL,
  ADD COLUMN IF NOT EXISTS variation_memory_request_pct  REAL,
  ADD COLUMN IF NOT EXISTS notification_codes            SMALLINT[],
  ADD COLUMN IF NOT EXISTS confidence_level              REAL;

-- Allow native rows to skip the JSONB recommendations column entirely.
ALTER TABLE historical_namespace_recommendation_sets
  ALTER COLUMN recommendations DROP NOT NULL;

-- Allow native rows to skip the legacy workload_id FK.
ALTER TABLE historical_namespace_recommendation_sets
  ALTER COLUMN workload_id DROP NOT NULL;

-- Allow native rows without the legacy monitoring_start_time constraint.
ALTER TABLE historical_namespace_recommendation_sets
  ALTER COLUMN monitoring_start_time DROP NOT NULL;

-- Unique index for native history rows (prevents duplicates per snapshot).
CREATE UNIQUE INDEX IF NOT EXISTS idx_hist_ns_recs_native_key
  ON historical_namespace_recommendation_sets (org_id, cluster_uuid, namespace_name, term, engine, created_at)
  WHERE term IS NOT NULL;
