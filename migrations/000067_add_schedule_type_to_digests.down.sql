-- Remove business-hours digest rows before narrowing primary keys (rollback safety).
DELETE FROM daily_container_digests WHERE schedule_type = 'business_hours';
DELETE FROM daily_namespace_digests WHERE schedule_type = 'business_hours';

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
