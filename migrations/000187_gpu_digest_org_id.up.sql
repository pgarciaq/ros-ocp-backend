-- #512 PR-1: nullable org_id on gpu_container_digests (GPU-ORG-1).
--
-- Does NOT SET NOT NULL (PR-2) and does NOT change gpu_container_digests_natural_key
-- (PR-3). Unique stays cluster-scoped until org_id is NOT NULL.
--
-- Backfill joins clusters → rh_accounts. clusters unique is
-- (tenant_id, source_id, cluster_uuid, cluster_alias), not global UUID.
-- DISTINCT ON (cluster_uuid) ORDER BY cluster_uuid, a.id picks one org when
-- the same UUID exists in multiple tenants. Do not invent a unique cluster UUID.
--
-- Orphan GPU rows with no clusters match stay NULL. Org prune
-- WHERE org_id = $1 misses those. Cluster/namespace prune also filter org_id
-- (parity with other digest tables), so NULL BH rows are not pruned until
-- re-ingest or PR-2. Ingest writers reject empty org_id so new rows are not NULL.
--
-- Keep idx_gpu_container_digests_cluster_sched_start (000186) for cluster-wide
-- GPU reads that do not predicate org_id. This index serves org BH prune:
--   WHERE org_id = $1 AND schedule_type = 'business_hours'
-- and cluster prune:
--   WHERE org_id = $1 AND cluster_uuid = $2 AND schedule_type = 'business_hours'
--
-- golang-migrate wraps each file in a transaction, so this migration uses
-- plain CREATE INDEX IF NOT EXISTS (not CONCURRENTLY). For large production
-- databases, run the equivalent CREATE INDEX CONCURRENTLY statement from
-- migrations/README.md first; IF NOT EXISTS makes this migration a no-op
-- when the index already exists.
--
-- Advisory: gpu_container_digests is on the large-table lint list. Same
-- pattern as 000061 / 000173 / 000174 / 000186.

ALTER TABLE gpu_container_digests
    ADD COLUMN IF NOT EXISTS org_id TEXT;

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

CREATE INDEX IF NOT EXISTS idx_gpu_container_digests_org_cluster_sched_start
    ON gpu_container_digests (
        org_id,
        cluster_uuid,
        schedule_type,
        interval_start
    );
