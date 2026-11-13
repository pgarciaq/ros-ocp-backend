-- Downgrade policy (migrations/README.md, "Narrowing down-migrations"):
-- the business-hours stream is 67-born data the predecessor primary
-- keys cannot represent; the deletion is deterministic, so the guard
-- names the exact cleanup and refuses until the operator runs it
-- deliberately (#846 review: uniform fail-loud, replacing the silent
-- delete).
DO $$
DECLARE bh_rows BIGINT;
BEGIN
    SELECT
        (SELECT COUNT(*) FROM daily_container_digests WHERE schedule_type = 'business_hours') +
        (SELECT COUNT(*) FROM daily_namespace_digests WHERE schedule_type = 'business_hours')
    INTO bh_rows;
    IF bh_rows > 0 THEN
        RAISE EXCEPTION 'downgrade 000067 blocked: % business-hours digest row(s) exist (daily_container_digests and/or daily_namespace_digests). They are 67-born derived data the predecessor primary keys cannot represent; your business-hours schedule configuration is NOT affected. To proceed, run deliberately: DELETE FROM daily_container_digests WHERE schedule_type = ''business_hours''; DELETE FROM daily_namespace_digests WHERE schedule_type = ''business_hours''; then force the source version (migrate force 67 — a failed downgrade leaves the database dirty at 66, having executed nothing) and re-run', bh_rows;
    END IF;
END $$;

ALTER TABLE daily_container_digests
    DROP CONSTRAINT daily_container_digests_pkey,
    DROP COLUMN schedule_type,
    ADD PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type,
                     container_name, bucket_date);

ALTER TABLE daily_namespace_digests
    DROP CONSTRAINT daily_namespace_digests_pkey,
    DROP COLUMN schedule_type,
    ADD PRIMARY KEY (org_id, cluster_uuid, namespace, bucket_date);

DROP TYPE IF EXISTS digest_schedule_type;
