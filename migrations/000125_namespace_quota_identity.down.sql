-- Downgrade policy (migrations/README.md, "Narrowing down-migrations"):
-- refuse over indeterminate survivors — never delete silently.
DO $$
DECLARE collisions BIGINT;
BEGIN
    SELECT COUNT(*) INTO collisions FROM (
        SELECT 1 FROM quota_recommendation_sets
        GROUP BY org_id, cluster_uuid, namespace
        HAVING COUNT(*) > 1
    ) d;
    IF collisions > 0 THEN
        RAISE EXCEPTION 'downgrade 000125 blocked: % namespace(es) have multiple quota_recommendation_sets rows (quota_name granularity this migration introduced); the 3-column contract cannot be restored over them. Deduplicate deliberately (decide which quota row survives); then force the source version (migrate force 125 — a failed downgrade leaves the database dirty at 124, having executed nothing) and re-run', collisions;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_quota_rec_history_lookup_v2;
ALTER TABLE quota_recommendation_history DROP COLUMN IF EXISTS quota_name;

ALTER TABLE quota_recommendation_sets
    DROP CONSTRAINT IF EXISTS quota_recommendation_sets_org_cluster_namespace_quota_key;

ALTER TABLE quota_recommendation_sets
    ADD CONSTRAINT quota_recommendation_sets_org_id_cluster_uuid_namespace_key
    UNIQUE (org_id, cluster_uuid, namespace);

DROP INDEX IF EXISTS idx_quota_recs_org_namespace_name;

ALTER TABLE quota_recommendation_sets
    DROP COLUMN IF EXISTS utilization_pods_bp,
    DROP COLUMN IF EXISTS utilization_storage_request_bp,
    DROP COLUMN IF EXISTS pods_recommended,
    DROP COLUMN IF EXISTS pods_used,
    DROP COLUMN IF EXISTS pods_hard,
    DROP COLUMN IF EXISTS storage_request_recommended_bytes,
    DROP COLUMN IF EXISTS storage_request_used_bytes,
    DROP COLUMN IF EXISTS storage_request_hard_bytes,
    DROP COLUMN IF EXISTS quota_name;

DROP TABLE IF EXISTS daily_namespace_quota_digests;
