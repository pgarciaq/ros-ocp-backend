-- Add memory P60/P98/P99 columns to daily_container_digests for parity with
-- daily_namespace_digests (which gained these in migrations 000031/000032).
-- ComputeDigest already computes these values; they were discarded until now.
ALTER TABLE daily_container_digests
  ADD COLUMN IF NOT EXISTS memory_request_p60_kib  BIGINT,
  ADD COLUMN IF NOT EXISTS memory_request_p98_kib  BIGINT,
  ADD COLUMN IF NOT EXISTS memory_request_p99_kib  BIGINT,
  ADD COLUMN IF NOT EXISTS memory_usage_p60_kib    BIGINT,
  ADD COLUMN IF NOT EXISTS memory_usage_p98_kib    BIGINT,
  ADD COLUMN IF NOT EXISTS memory_usage_p99_kib    BIGINT;
