-- #486: dual-stream daily_vm_digests (all_hours + business_hours).
-- Existing rows keep all_hours via DEFAULT. Heap table unique key grows
-- schedule_type. Do not add schedule_type to persist rec tables.
-- Child FKs (vm_gpu_device_digests, vm_pvc_digests) stay on parent id.

ALTER TABLE daily_vm_digests
    ADD COLUMN IF NOT EXISTS schedule_type digest_schedule_type NOT NULL DEFAULT 'all_hours';

-- PostgreSQL NAMEDATALEN truncates identifiers to 63 bytes. The UNIQUE from
-- 000089 is stored as ..._buck_key, not the untruncated ..._bucket_date_key.
-- Leaving the old unique in place blocks all_hours + business_hours dual-write
-- (same VM-day, two schedule_type values).
ALTER TABLE daily_vm_digests
    DROP CONSTRAINT IF EXISTS daily_vm_digests_org_id_cluster_uuid_vm_name_namespace_bucket_date_key;
ALTER TABLE daily_vm_digests
    DROP CONSTRAINT IF EXISTS daily_vm_digests_org_id_cluster_uuid_vm_name_namespace_buck_key;

CREATE UNIQUE INDEX IF NOT EXISTS daily_vm_digests_natural_key
    ON daily_vm_digests (
        org_id, cluster_uuid, vm_name, namespace, bucket_date, schedule_type
    );

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (82, 'VM_BH_OFFICE_WINDOW', 'WARNING',
     'Business-hours VM sizing uses the namespace office window — overnight batch and off-hours bursts are excluded')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
