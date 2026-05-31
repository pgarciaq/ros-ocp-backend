CREATE TABLE IF NOT EXISTS cluster_instance_types (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    name TEXT NOT NULL,
    series TEXT NOT NULL DEFAULT '',
    vcpu INTEGER NOT NULL,
    memory_gib INTEGER NOT NULL,
    gpus INTEGER NOT NULL DEFAULT 0,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(org_id, cluster_uuid, name)
);

CREATE INDEX IF NOT EXISTS idx_cluster_instance_types_org_cluster
    ON cluster_instance_types(org_id, cluster_uuid);
