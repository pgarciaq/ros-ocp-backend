-- W1.2 association persistence (#632, implements #621).
--
-- 1. manifest_hcp_snapshots stores the report-scoped namespace-to-HostedCluster
--    mapping: one row per manifest x HCP namespace. Unpartitioned: ~100 rows
--    per day per management cluster (~18k rows per 6-month retention window);
--    swept with digest retention housekeeping.
-- 2. recommendation_sets.hosted_cluster_id carries the resolved association
--    (NULL = management/namespace-scoped, unassociated). Partial index mirrors
--    the has_gpu precedent (000062): only non-NULL rows indexed.
-- Plain DDL (no CONCURRENTLY): fresh installs run inside a transaction
-- (golang-migrate wraps this file in one); ADD COLUMN nullable is
-- metadata-only. For large production databases run the equivalent
-- CREATE INDEX CONCURRENTLY statements from migrations/README.md first,
-- same large-table policy as 000061/000062.

CREATE TABLE IF NOT EXISTS manifest_hcp_snapshots (
    manifest_id        TEXT NOT NULL,
    org_id             TEXT NOT NULL,
    cluster_uuid       UUID NOT NULL,
    hcp_namespace      TEXT NOT NULL,
    hosted_cluster_id  TEXT,
    hc_uid             TEXT NOT NULL DEFAULT '',
    hcp_uid            TEXT NOT NULL DEFAULT '',
    namespace_uid      TEXT NOT NULL DEFAULT '',
    namespace_created_at TIMESTAMPTZ,
    observed_at        TIMESTAMPTZ NOT NULL,
    complete           BOOLEAN NOT NULL DEFAULT FALSE,
    diagnostics        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (manifest_id, hcp_namespace)
);

CREATE INDEX IF NOT EXISTS idx_manifest_hcp_snapshots_lookup
    ON manifest_hcp_snapshots (org_id, cluster_uuid, hcp_namespace, observed_at);

ALTER TABLE recommendation_sets ADD COLUMN IF NOT EXISTS hosted_cluster_id TEXT;

-- Partial index for HC-filtered reads (only associated rows, which are sparse
-- until per-HC attribution rolls out fleet-wide).
CREATE INDEX IF NOT EXISTS idx_recommendation_sets_hosted_cluster
    ON recommendation_sets (org_id, cluster_uuid, namespace, workload, container_name)
    WHERE hosted_cluster_id IS NOT NULL;
