CREATE TABLE IF NOT EXISTS cluster_vm_preferences_meta (
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    preferences JSONB NOT NULL DEFAULT '[]',
    vm_preferences JSONB NOT NULL DEFAULT '{}',
    collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, cluster_uuid)
);
