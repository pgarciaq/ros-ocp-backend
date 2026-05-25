CREATE TABLE IF NOT EXISTS org_container_keys (
    org_id         TEXT NOT NULL,
    cluster_uuid   UUID NOT NULL,
    namespace      TEXT NOT NULL,
    workload       TEXT NOT NULL,
    workload_type  TEXT NOT NULL DEFAULT '',
    container_name TEXT NOT NULL,
    last_reported  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_tags  JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (org_id, namespace, workload, container_name)
);

CREATE INDEX IF NOT EXISTS idx_ock_org_sorted
    ON org_container_keys (org_id, namespace, workload, container_name);

CREATE INDEX IF NOT EXISTS idx_ock_tags
    ON org_container_keys USING GIN (resolved_tags);

CREATE INDEX IF NOT EXISTS idx_ock_org_cluster
    ON org_container_keys (org_id, cluster_uuid);
