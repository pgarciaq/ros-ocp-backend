-- #445 slice A: nullable org_id on clusters (directory lookups still join
-- rh_accounts until slice B).
--
-- SET NOT NULL is 000192 (slice B). Unique stays
-- (tenant_id, source_id, cluster_uuid, cluster_alias). Do not drop tenant_id.
-- cluster_uuid is not globally unique.
--
-- Backfill copies rh_accounts.org_id via tenant_id. Rows whose account has
-- NULL or empty org_id stay NULL until dual-write or slice B re-backfill.
-- Ingest writers reject empty org_id so new rows are not NULL.
--
-- BEFORE INSERT/UPDATE trigger fills org_id from rh_accounts when omitted so
-- test inserts and any missed writer stay consistent with tenant_id. Production
-- writers still stamp org_id (CreateCluster, EnsureAccountCluster).
--
-- clusters is small. Plain CREATE INDEX IF NOT EXISTS (not on the large-table
-- lint list). golang-migrate wraps this file in a transaction.

ALTER TABLE clusters
    ADD COLUMN IF NOT EXISTS org_id TEXT;

UPDATE clusters c
SET org_id = a.org_id
FROM rh_accounts a
WHERE c.tenant_id = a.id
  AND c.org_id IS NULL
  AND a.org_id IS NOT NULL
  AND a.org_id <> '';

CREATE OR REPLACE FUNCTION clusters_fill_org_id() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.org_id IS NULL OR NEW.org_id = '' THEN
        SELECT a.org_id INTO NEW.org_id
        FROM rh_accounts a
        WHERE a.id = NEW.tenant_id;
    END IF;
    IF NEW.org_id IS NULL OR NEW.org_id = '' THEN
        RAISE EXCEPTION 'clusters.org_id is required';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_clusters_fill_org_id ON clusters;
CREATE TRIGGER trg_clusters_fill_org_id
    BEFORE INSERT OR UPDATE OF tenant_id, org_id ON clusters
    FOR EACH ROW
    EXECUTE FUNCTION clusters_fill_org_id();

CREATE INDEX IF NOT EXISTS idx_clusters_org_id_uuid
    ON clusters (org_id, cluster_uuid);
