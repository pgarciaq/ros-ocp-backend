-- Rollback migration 000032.

ALTER TABLE daily_namespace_digests
  DROP COLUMN IF EXISTS memory_request_p60_kib,
  DROP COLUMN IF EXISTS memory_request_p98_kib,
  DROP COLUMN IF EXISTS memory_request_p99_kib,
  DROP COLUMN IF EXISTS memory_usage_p60_kib,
  DROP COLUMN IF EXISTS memory_usage_p98_kib,
  DROP COLUMN IF EXISTS memory_usage_p99_kib;
