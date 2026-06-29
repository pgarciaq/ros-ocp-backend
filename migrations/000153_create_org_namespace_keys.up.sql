CREATE TABLE IF NOT EXISTS org_namespace_keys (
    org_id         TEXT NOT NULL,
    cluster_uuid   UUID NOT NULL,
    namespace_name TEXT NOT NULL,
    last_reported  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_tags  JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (org_id, cluster_uuid, namespace_name)
);

CREATE INDEX IF NOT EXISTS idx_onk_org_sorted
    ON org_namespace_keys (org_id, cluster_uuid, namespace_name);

CREATE INDEX IF NOT EXISTS idx_onk_tags
    ON org_namespace_keys USING GIN (resolved_tags);

CREATE INDEX IF NOT EXISTS idx_onk_org_cluster
    ON org_namespace_keys (org_id, cluster_uuid);
