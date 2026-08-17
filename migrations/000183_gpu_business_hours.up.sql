-- #485: dual-stream gpu_container_digests (all_hours + business_hours).
-- Existing rows keep all_hours via DEFAULT. Partition key (interval_start)
-- stays in the unique index. Do not add schedule_type to persist rec tables.

ALTER TABLE gpu_container_digests
    ADD COLUMN schedule_type digest_schedule_type NOT NULL DEFAULT 'all_hours';

DROP INDEX IF EXISTS gpu_container_digests_natural_key;
CREATE UNIQUE INDEX gpu_container_digests_natural_key
    ON gpu_container_digests (
        cluster_uuid, namespace, workload, container_name,
        gpu_model_name, interval_start, schedule_type
    );

INSERT INTO notification_code_definitions (code, name, severity, description) VALUES
    (80, 'GPU_BH_OFFICE_WINDOW', 'WARNING',
     'Business-hours GPU sizing uses the namespace office window — overnight training and off-hours bursts are excluded')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description;
