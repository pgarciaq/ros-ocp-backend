-- Downgrade policy (migrations/README.md, "Narrowing down-migrations"):
-- refuse over indeterminate survivors — never delete silently. Dropping
-- the term column below makes multi-term rows duplicates of the
-- 4-column identity; the guard must fire BEFORE that happens.
DO $$
DECLARE collisions BIGINT;
BEGIN
    SELECT COUNT(*) INTO collisions FROM (
        SELECT 1 FROM pvc_recommendation_sets
        GROUP BY org_id, cluster_uuid, namespace, persistentvolumeclaim
        HAVING COUNT(*) > 1
    ) d;
    IF collisions > 0 THEN
        RAISE EXCEPTION 'downgrade 000064 blocked: % PVC identit(ies) have multiple term rows; the 4-column contract cannot be restored over them (dropping the term column would collide them). Deduplicate deliberately (decide which term survives); then force the source version (migrate force 64 — a failed downgrade leaves the database dirty at 63, having executed nothing) and re-run', collisions;
    END IF;
END $$;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_cluster_ns_pvc_term_key;

ALTER TABLE pvc_recommendation_sets
    DROP COLUMN IF EXISTS term;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_id_cluster_uuid_namespace_persist_key;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_id_cluster_uuid_namespace_persi_key;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_id_cluster_uuid_namespace_persist_k;

ALTER TABLE pvc_recommendation_sets
    ADD CONSTRAINT pvc_recommendation_sets_org_id_cluster_uuid_namespace_persist_key
    UNIQUE (org_id, cluster_uuid, namespace, persistentvolumeclaim);
