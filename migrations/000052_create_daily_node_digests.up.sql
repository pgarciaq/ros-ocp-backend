CREATE TABLE IF NOT EXISTS daily_node_digests (
    bucket_date             DATE NOT NULL,
    org_id                  TEXT NOT NULL,
    cluster_uuid            UUID NOT NULL,
    node                    TEXT NOT NULL,
    cpu_usage_p50_mc        BIGINT,
    cpu_usage_p95_mc        BIGINT,
    mem_usage_p50_kib       BIGINT,
    mem_usage_p95_kib       BIGINT,
    max_cpu_allocatable_mc  BIGINT,
    max_mem_allocatable_kib BIGINT,
    max_cpu_requests_mc     BIGINT,
    max_mem_requests_kib    BIGINT,
    max_pod_count           BIGINT,
    instance_type           TEXT,
    machineset_name         TEXT,
    sample_count            BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, node, bucket_date)
) PARTITION BY RANGE (bucket_date);
