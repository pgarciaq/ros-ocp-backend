-- Align container_usage_samples PK with ingestion upsert (issue #346).
-- Fresh installs get workload_type in PK from 000031; in-place upgrades may still
-- have the pre-#346 key without workload_type.
ALTER TABLE container_usage_samples
    DROP CONSTRAINT container_usage_samples_pkey,
    ADD PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type,
                     container_name, sample_time);
