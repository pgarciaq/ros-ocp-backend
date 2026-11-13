-- Downgrade policy (migrations/README.md, "Narrowing down-migrations"):
-- the business-hours stream is 185-born data the predecessor contract
-- cannot represent, but the deletion is deterministic — so the guard
-- names the exact cleanup and refuses until the operator runs it
-- deliberately. The schedule configuration (business_hours_schedules)
-- is NOT affected; re-applying 185 rebuilds the stream from retained
-- raw report data on re-ingestion.
DO $$
DECLARE bh_rows BIGINT;
BEGIN
    SELECT COUNT(*) INTO bh_rows FROM daily_vm_digests WHERE schedule_type = 'business_hours';
    IF bh_rows > 0 THEN
        RAISE EXCEPTION 'downgrade 000185 blocked: % business-hours digest row(s) exist. They are 185-born derived data the predecessor contract cannot represent; your business-hours schedule configuration is NOT affected. Deleting them also cascades to their vm_gpu_device_digests and vm_pvc_digests children. To proceed, run deliberately: DELETE FROM daily_vm_digests WHERE schedule_type = ''business_hours''; then force the source version (migrate force 185 — a failed downgrade leaves the database dirty at 184, having executed nothing) and re-run. Historical BH digests return only if the raw reports are still retained and a reship is run', bh_rows;
    END IF;
END $$;

DROP INDEX IF EXISTS daily_vm_digests_natural_key;

ALTER TABLE daily_vm_digests
    DROP COLUMN IF EXISTS schedule_type;

-- PostgreSQL will truncate this identifier to 63 bytes (..._buck_key).
ALTER TABLE daily_vm_digests
    ADD CONSTRAINT daily_vm_digests_org_id_cluster_uuid_vm_name_namespace_bucket_date_key
    UNIQUE (org_id, cluster_uuid, vm_name, namespace, bucket_date);

DELETE FROM notification_code_definitions WHERE code = 82;
