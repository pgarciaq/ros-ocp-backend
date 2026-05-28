ALTER TABLE daily_namespace_digests
    DROP COLUMN IF EXISTS cpu_request_hard_millicores,
    DROP COLUMN IF EXISTS cpu_limit_hard_millicores,
    DROP COLUMN IF EXISTS memory_request_hard_bytes,
    DROP COLUMN IF EXISTS memory_limit_hard_bytes,
    DROP COLUMN IF EXISTS cpu_request_used_millicores,
    DROP COLUMN IF EXISTS cpu_limit_used_millicores,
    DROP COLUMN IF EXISTS memory_request_used_bytes,
    DROP COLUMN IF EXISTS memory_limit_used_bytes;
