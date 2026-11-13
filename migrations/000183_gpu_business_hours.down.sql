-- Downgrade policy (migrations/README.md, "Narrowing down-migrations"):
-- the business-hours stream is 183-born data the predecessor unique
-- index cannot represent; the deletion is deterministic, so the guard
-- names the exact cleanup and refuses until the operator runs it
-- deliberately (#846 review).
DO $$
DECLARE bh_rows BIGINT;
BEGIN
    SELECT COUNT(*) INTO bh_rows FROM gpu_container_digests WHERE schedule_type = 'business_hours';
    IF bh_rows > 0 THEN
        RAISE EXCEPTION 'downgrade 000183 blocked: % business-hours GPU digest row(s) exist. They are 183-born derived data the predecessor unique index cannot represent; your business-hours schedule configuration is NOT affected. To proceed, run deliberately: DELETE FROM gpu_container_digests WHERE schedule_type = ''business_hours''; then force the source version (migrate force 183 — a failed downgrade leaves the database dirty at 182, having executed nothing) and re-run', bh_rows;
    END IF;
END $$;

DROP INDEX IF EXISTS gpu_container_digests_natural_key;

ALTER TABLE gpu_container_digests
    DROP COLUMN schedule_type;

CREATE UNIQUE INDEX gpu_container_digests_natural_key
    ON gpu_container_digests (
        cluster_uuid, namespace, workload, container_name,
        gpu_model_name, interval_start
    );

DELETE FROM notification_code_definitions WHERE code = 80;
