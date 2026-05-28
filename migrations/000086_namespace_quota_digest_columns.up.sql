-- Latest ResourceQuota hard/used snapshot per namespace-day (from operator namespace CSV).
ALTER TABLE daily_namespace_digests
    ADD COLUMN IF NOT EXISTS cpu_request_hard_millicores BIGINT,
    ADD COLUMN IF NOT EXISTS cpu_limit_hard_millicores BIGINT,
    ADD COLUMN IF NOT EXISTS memory_request_hard_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS memory_limit_hard_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS cpu_request_used_millicores BIGINT,
    ADD COLUMN IF NOT EXISTS cpu_limit_used_millicores BIGINT,
    ADD COLUMN IF NOT EXISTS memory_request_used_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS memory_limit_used_bytes BIGINT;
