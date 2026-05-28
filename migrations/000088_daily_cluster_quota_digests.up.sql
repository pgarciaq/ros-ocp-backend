CREATE TABLE IF NOT EXISTS daily_cluster_quota_digests (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    cluster_quota_name TEXT NOT NULL,
    report_date DATE NOT NULL,
    cpu_request_hard BIGINT,
    cpu_request_used BIGINT,
    cpu_limit_hard BIGINT,
    cpu_limit_used BIGINT,
    memory_request_hard BIGINT,
    memory_request_used BIGINT,
    memory_limit_hard BIGINT,
    memory_limit_used BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(org_id, cluster_uuid, cluster_quota_name, report_date)
);
