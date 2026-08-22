DELETE FROM daily_vm_digests WHERE schedule_type = 'business_hours';

DROP INDEX IF EXISTS daily_vm_digests_natural_key;

ALTER TABLE daily_vm_digests
    DROP COLUMN IF EXISTS schedule_type;

-- PostgreSQL will truncate this identifier to 63 bytes (..._buck_key).
ALTER TABLE daily_vm_digests
    ADD CONSTRAINT daily_vm_digests_org_id_cluster_uuid_vm_name_namespace_bucket_date_key
    UNIQUE (org_id, cluster_uuid, vm_name, namespace, bucket_date);

DELETE FROM notification_code_definitions WHERE code = 82;
