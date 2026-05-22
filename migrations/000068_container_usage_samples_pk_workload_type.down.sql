ALTER TABLE container_usage_samples
    DROP CONSTRAINT container_usage_samples_pkey,
    ADD PRIMARY KEY (org_id, cluster_uuid, namespace, workload, container_name, sample_time);
