CREATE TABLE IF NOT EXISTS quota_recommendation_sets (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    namespace TEXT NOT NULL,

    cpu_request_hard_millicores BIGINT,
    cpu_limit_hard_millicores BIGINT,
    memory_request_hard_bytes BIGINT,
    memory_limit_hard_bytes BIGINT,

    cpu_request_used_millicores BIGINT,
    cpu_limit_used_millicores BIGINT,
    memory_request_used_bytes BIGINT,
    memory_limit_used_bytes BIGINT,

    cpu_request_recommended_millicores BIGINT,
    cpu_limit_recommended_millicores BIGINT,
    memory_request_recommended_bytes BIGINT,
    memory_limit_recommended_bytes BIGINT,

    headroom_basis_points INT NOT NULL DEFAULT 12000,

    cpu_request_utilization_bp INT,
    cpu_limit_utilization_bp INT,
    memory_request_utilization_bp INT,
    memory_limit_utilization_bp INT,

    cpu_freed_millicores BIGINT,
    memory_freed_bytes BIGINT,
    estimated_savings_cents BIGINT,
    currency TEXT NOT NULL DEFAULT 'USD',

    recommendation_type TEXT NOT NULL DEFAULT 'none',
    risk_level TEXT NOT NULL DEFAULT 'none',

    last_observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (org_id, cluster_uuid, namespace)
);

CREATE INDEX IF NOT EXISTS idx_quota_recs_org_cluster ON quota_recommendation_sets (org_id, cluster_uuid);
CREATE INDEX IF NOT EXISTS idx_quota_recs_org_namespace ON quota_recommendation_sets (org_id, namespace);
CREATE INDEX IF NOT EXISTS idx_quota_recs_type ON quota_recommendation_sets (org_id, recommendation_type) WHERE recommendation_type != 'none';
