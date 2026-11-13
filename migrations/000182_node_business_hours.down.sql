-- Downgrade policy (migrations/README.md, "Narrowing down-migrations"):
-- the business-hours stream is 182-born data the predecessor primary
-- key cannot represent; the deletion is deterministic, so the guard
-- names the exact cleanup and refuses until the operator runs it
-- deliberately (#846 review).
DO $$
DECLARE bh_rows BIGINT;
BEGIN
    SELECT COUNT(*) INTO bh_rows FROM daily_node_digests WHERE schedule_type = 'business_hours';
    IF bh_rows > 0 THEN
        RAISE EXCEPTION 'downgrade 000182 blocked: % business-hours node digest row(s) exist. They are 182-born derived data the predecessor primary key cannot represent; your business-hours schedule configuration is NOT affected. To proceed, run deliberately: DELETE FROM daily_node_digests WHERE schedule_type = ''business_hours''; then force the source version (migrate force 182 — a failed downgrade leaves the database dirty at 181, having executed nothing) and re-run', bh_rows;
    END IF;
END $$;

ALTER TABLE daily_node_digests
    DROP CONSTRAINT daily_node_digests_pkey;

ALTER TABLE daily_node_digests
    DROP COLUMN schedule_type;

ALTER TABLE daily_node_digests
    ADD PRIMARY KEY (org_id, cluster_uuid, node, bucket_date);

DELETE FROM notification_code_definitions WHERE code = 79;
