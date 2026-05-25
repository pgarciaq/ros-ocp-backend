CREATE TABLE IF NOT EXISTS org_tag_sync_metadata (
    org_id     TEXT PRIMARY KEY,
    synced_at  TIMESTAMPTZ NOT NULL,
    tag_keys   JSONB NOT NULL DEFAULT '[]',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
