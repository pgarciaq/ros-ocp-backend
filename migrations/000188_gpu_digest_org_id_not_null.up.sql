-- #512 PR-2: SET NOT NULL on gpu_container_digests.org_id.
--
-- Does NOT change gpu_container_digests_natural_key (PR-3) and does NOT
-- rewrite GPU SELECT paths (PR-4). Unique stays cluster-scoped.
--
-- Re-runs the 000187 backfill (idempotent) so rows that gained a clusters
-- match after PR-1 are stamped before the constraint. Does NOT DELETE
-- leftover NULLs. Orphans with no clusters match fail VALIDATE / SET NOT NULL.
--
-- PG16 CHECK pattern: ADD CHECK NOT VALID (fast), VALIDATE CONSTRAINT
-- (SHARE UPDATE EXCLUSIVE scan; fails if any org_id IS NULL), then
-- SET NOT NULL (skips a second scan when the CHECK is valid), then drop
-- the CHECK. golang-migrate wraps this file in one transaction, so
-- ACCESS EXCLUSIVE from SET NOT NULL is held until commit.
--
-- Advisory: gpu_container_digests is on the large-table lint list.
-- VALIDATE scans every partition. Same class of pain as 000186 / 000187.
-- Empty-string org_id is allowed by NOT NULL; ingest writers reject "".

UPDATE gpu_container_digests g
SET org_id = src.org_id
FROM (
    SELECT DISTINCT ON (c.cluster_uuid)
        c.cluster_uuid,
        a.org_id
    FROM clusters c
    JOIN rh_accounts a ON a.id = c.tenant_id
    WHERE a.org_id IS NOT NULL AND a.org_id <> ''
    ORDER BY c.cluster_uuid, a.id
) src
WHERE g.cluster_uuid = src.cluster_uuid
  AND g.org_id IS NULL;

ALTER TABLE gpu_container_digests
    ADD CONSTRAINT gpu_container_digests_org_id_not_null
    CHECK (org_id IS NOT NULL) NOT VALID;

ALTER TABLE gpu_container_digests
    VALIDATE CONSTRAINT gpu_container_digests_org_id_not_null;

ALTER TABLE gpu_container_digests
    ALTER COLUMN org_id SET NOT NULL;

ALTER TABLE gpu_container_digests
    DROP CONSTRAINT gpu_container_digests_org_id_not_null;
