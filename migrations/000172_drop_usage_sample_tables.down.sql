-- Recreate the tables dropped by the up migration.
-- Partitions are not recreated; they would be created at runtime if writes resumed.

CREATE TABLE IF NOT EXISTS container_usage_samples (
    sample_time     TIMESTAMPTZ NOT NULL,
    org_id          TEXT NOT NULL,
    cluster_uuid    UUID NOT NULL,
    namespace       TEXT NOT NULL,
    workload        TEXT NOT NULL,
    workload_type   TEXT NOT NULL,
    container_name  TEXT NOT NULL,
    cpu_usage_mc    BIGINT NOT NULL,
    mem_usage_kib   BIGINT NOT NULL,
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type, container_name, sample_time)
) PARTITION BY RANGE (sample_time);

CREATE TABLE IF NOT EXISTS namespace_usage_samples (
    sample_time     TIMESTAMPTZ NOT NULL,
    org_id          TEXT NOT NULL,
    cluster_uuid    UUID NOT NULL,
    namespace       TEXT NOT NULL,
    cpu_usage_mc    BIGINT NOT NULL,
    mem_usage_kib   BIGINT NOT NULL,
    PRIMARY KEY (org_id, cluster_uuid, namespace, sample_time)
) PARTITION BY RANGE (sample_time);

INSERT INTO ros_partitioned_parent_registry (match_kind, pattern)
VALUES ('exact', 'container_usage_samples'),
       ('exact', 'namespace_usage_samples')
ON CONFLICT (match_kind, pattern) DO NOTHING;
