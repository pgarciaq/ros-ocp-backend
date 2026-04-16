-- Phase 6: Add native relational columns to namespace_recommendation_sets.
-- Native rows have term IS NOT NULL; legacy Kruize rows have term IS NULL.
ALTER TABLE namespace_recommendation_sets
  ADD COLUMN IF NOT EXISTS cluster_uuid UUID,
  ADD COLUMN IF NOT EXISTS term TEXT,
  ADD COLUMN IF NOT EXISTS engine TEXT,
  ADD COLUMN IF NOT EXISTS namespace_id TEXT,
  ADD COLUMN IF NOT EXISTS rec_cpu_request_millicores BIGINT,
  ADD COLUMN IF NOT EXISTS rec_cpu_limit_millicores BIGINT,
  ADD COLUMN IF NOT EXISTS rec_memory_request_kib BIGINT,
  ADD COLUMN IF NOT EXISTS rec_memory_limit_kib BIGINT,
  ADD COLUMN IF NOT EXISTS current_cpu_request_millicores BIGINT,
  ADD COLUMN IF NOT EXISTS current_cpu_limit_millicores BIGINT,
  ADD COLUMN IF NOT EXISTS current_memory_request_kib BIGINT,
  ADD COLUMN IF NOT EXISTS current_memory_limit_kib BIGINT,
  ADD COLUMN IF NOT EXISTS variation_cpu_request_pct REAL,
  ADD COLUMN IF NOT EXISTS variation_memory_request_pct REAL,
  ADD COLUMN IF NOT EXISTS notification_codes SMALLINT[],
  ADD COLUMN IF NOT EXISTS confidence_level REAL,
  ADD COLUMN IF NOT EXISTS stale BOOLEAN DEFAULT false;

-- Partial unique index: only native rows (term IS NOT NULL) are constrained.
CREATE UNIQUE INDEX IF NOT EXISTS idx_ns_recs_native_key
  ON namespace_recommendation_sets (org_id, cluster_uuid, namespace_name, term, engine)
  WHERE term IS NOT NULL;

-- Index for detail lookup by namespace_id.
CREATE INDEX IF NOT EXISTS idx_ns_recs_namespace_id
  ON namespace_recommendation_sets (namespace_id)
  WHERE namespace_id IS NOT NULL;
