-- Migration 000032: Add exact memory percentile columns to daily_namespace_digests
-- for consistency with CPU percentile coverage (P60, P98, P99).

ALTER TABLE daily_namespace_digests
  ADD COLUMN IF NOT EXISTS memory_request_p60_kib  BIGINT,
  ADD COLUMN IF NOT EXISTS memory_request_p98_kib  BIGINT,
  ADD COLUMN IF NOT EXISTS memory_request_p99_kib  BIGINT,
  ADD COLUMN IF NOT EXISTS memory_usage_p60_kib    BIGINT,
  ADD COLUMN IF NOT EXISTS memory_usage_p98_kib    BIGINT,
  ADD COLUMN IF NOT EXISTS memory_usage_p99_kib    BIGINT;
