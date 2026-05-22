-- Discriminate all-hours vs business-hours digest rows (dual aggregate streams).
CREATE TYPE digest_schedule_type AS ENUM ('all_hours', 'business_hours');

COMMENT ON TYPE digest_schedule_type IS
    'Digest aggregation window: all_hours (24/7) or business_hours (schedule-filtered)';

ALTER TABLE daily_container_digests
    ADD COLUMN schedule_type digest_schedule_type NOT NULL DEFAULT 'all_hours';

ALTER TABLE daily_container_digests
    DROP CONSTRAINT daily_container_digests_pkey,
    ADD PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type,
                     container_name, bucket_date, schedule_type);

ALTER TABLE daily_namespace_digests
    ADD COLUMN schedule_type digest_schedule_type NOT NULL DEFAULT 'all_hours';

ALTER TABLE daily_namespace_digests
    DROP CONSTRAINT daily_namespace_digests_pkey,
    ADD PRIMARY KEY (org_id, cluster_uuid, namespace, bucket_date, schedule_type);
