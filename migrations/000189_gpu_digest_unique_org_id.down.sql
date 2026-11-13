-- Recreate the cluster-scoped unique. Downgrade policy (migrations/
-- README.md, "Narrowing down-migrations"): refuse over indeterminate
-- survivors — never delete silently. Cross-org rows are legal under
-- the org-scoped key this migration introduced; which org's rows
-- survive a cluster-scoped contract is not determinable.
DO $$
DECLARE collisions BIGINT;
BEGIN
    SELECT COUNT(*) INTO collisions FROM (
        SELECT 1 FROM gpu_container_digests
        GROUP BY cluster_uuid, namespace, workload, container_name,
                 gpu_model_name, interval_start, schedule_type
        HAVING COUNT(*) > 1
    ) d;
    IF collisions > 0 THEN
        RAISE EXCEPTION 'downgrade 000189 blocked: % cluster-scoped GPU identit(ies) have rows from multiple orgs; the cluster-scoped unique cannot be restored over them. Deduplicate deliberately (decide which org''s rows survive); then force the source version (migrate force 189 — a failed downgrade leaves the database dirty at 188, having executed nothing) and re-run', collisions;
    END IF;
END $$;

DROP INDEX IF EXISTS gpu_container_digests_natural_key;

CREATE UNIQUE INDEX gpu_container_digests_natural_key
    ON gpu_container_digests (
        cluster_uuid, namespace, workload, container_name,
        gpu_model_name, interval_start, schedule_type
    );
