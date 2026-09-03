DROP TRIGGER IF EXISTS trg_clusters_fill_org_id ON clusters;
DROP FUNCTION IF EXISTS clusters_fill_org_id();
DROP INDEX IF EXISTS idx_clusters_org_id_uuid;

ALTER TABLE clusters
    DROP COLUMN IF EXISTS org_id;
