DELETE FROM gpu_container_digests WHERE schedule_type = 'business_hours';

DROP INDEX IF EXISTS gpu_container_digests_natural_key;

ALTER TABLE gpu_container_digests
    DROP COLUMN schedule_type;

CREATE UNIQUE INDEX gpu_container_digests_natural_key
    ON gpu_container_digests (
        cluster_uuid, namespace, workload, container_name,
        gpu_model_name, interval_start
    );

DELETE FROM notification_code_definitions WHERE code = 80;
