DROP INDEX IF EXISTS idx_ock_org_stale;
ALTER TABLE org_container_keys DROP COLUMN IF EXISTS is_stale;
