ALTER TABLE org_container_keys ADD COLUMN IF NOT EXISTS is_stale BOOLEAN NOT NULL DEFAULT false;

-- Partial index for efficient filter[stale]=only queries. Most containers are
-- non-stale, so a partial index on the minority population avoids bloating the
-- primary B-tree with a column that is almost always false.
CREATE INDEX IF NOT EXISTS idx_ock_org_stale
    ON org_container_keys (org_id) WHERE is_stale = true;
