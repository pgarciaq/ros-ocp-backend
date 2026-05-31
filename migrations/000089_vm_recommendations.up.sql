-- daily_vm_digests: aggregated daily metrics for VMs (15-min samples → daily stats)
CREATE TABLE IF NOT EXISTS daily_vm_digests (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    vm_name TEXT NOT NULL,
    namespace TEXT NOT NULL,
    node_name TEXT NOT NULL DEFAULT '',
    guest_os TEXT NOT NULL DEFAULT '',
    bucket_date DATE NOT NULL,

    -- CPU (millicores)
    cpu_usage_p50_mc BIGINT NOT NULL DEFAULT 0,
    cpu_usage_p95_mc BIGINT NOT NULL DEFAULT 0,
    cpu_usage_p99_mc BIGINT NOT NULL DEFAULT 0,
    cpu_usage_max_mc BIGINT NOT NULL DEFAULT 0,
    cpu_request_mc BIGINT NOT NULL DEFAULT 0,
    cpu_limit_mc BIGINT NOT NULL DEFAULT 0,

    -- Memory (KiB)
    mem_usage_p50_kib BIGINT NOT NULL DEFAULT 0,
    mem_usage_p95_kib BIGINT NOT NULL DEFAULT 0,
    mem_usage_p99_kib BIGINT NOT NULL DEFAULT 0,
    mem_usage_max_kib BIGINT NOT NULL DEFAULT 0,
    mem_request_kib BIGINT NOT NULL DEFAULT 0,

    -- Guest agent memory (nullable)
    mem_available_p50_kib BIGINT,
    mem_available_p95_kib BIGINT,

    -- Disk
    disk_allocated_max_bytes BIGINT NOT NULL DEFAULT 0,

    -- Filesystem (guest agent, nullable)
    filesystem_used_max_bytes BIGINT,
    filesystem_capacity_bytes BIGINT,

    -- I/O
    disk_read_iops_p95 BIGINT,
    disk_write_iops_p95 BIGINT,
    disk_read_bps_p95 BIGINT,
    disk_write_bps_p95 BIGINT,

    sample_count INTEGER NOT NULL DEFAULT 0,

    UNIQUE(org_id, cluster_uuid, vm_name, namespace, bucket_date)
);

CREATE INDEX IF NOT EXISTS idx_daily_vm_digests_org_cluster
    ON daily_vm_digests(org_id, cluster_uuid);

CREATE INDEX IF NOT EXISTS idx_daily_vm_digests_bucket_date
    ON daily_vm_digests(bucket_date);


-- vm_recommendations: current recommendation state per VM
CREATE TABLE IF NOT EXISTS vm_recommendations (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    vm_name TEXT NOT NULL,
    namespace TEXT NOT NULL,
    guest_os TEXT NOT NULL DEFAULT '',

    -- Current allocation
    current_vcpu INTEGER NOT NULL DEFAULT 0,
    current_memory_gib INTEGER NOT NULL DEFAULT 0,
    current_disk_gib INTEGER,
    current_instance_type TEXT,

    -- Recommended
    recommended_vcpu INTEGER NOT NULL DEFAULT 0,
    recommended_memory_gib INTEGER NOT NULL DEFAULT 0,
    recommended_disk_gib INTEGER,
    recommended_instance_type TEXT,
    recommended_series TEXT,

    -- Metadata
    guest_agent_detected BOOLEAN NOT NULL DEFAULT FALSE,
    confidence TEXT NOT NULL DEFAULT 'moderate',
    term TEXT NOT NULL DEFAULT 'medium_term',
    engine TEXT NOT NULL DEFAULT 'cost',

    -- Status flags
    is_idle BOOLEAN NOT NULL DEFAULT FALSE,
    is_oversized BOOLEAN NOT NULL DEFAULT FALSE,

    -- I/O
    io_read_iops_p95 BIGINT,
    io_write_iops_p95 BIGINT,
    io_read_bps_p95 BIGINT,
    io_write_bps_p95 BIGINT,
    io_hint TEXT,

    -- Disk projection
    disk_days_until_full INTEGER,
    disk_growth_gib_per_day DOUBLE PRECISION,
    disk_recommended_expand_gib INTEGER,

    -- Notifications (JSONB)
    notifications JSONB DEFAULT '[]'::jsonb,

    -- Timestamps
    last_recommended_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(org_id, cluster_uuid, vm_name, namespace, term, engine)
);

CREATE INDEX IF NOT EXISTS idx_vm_recommendations_org_cluster
    ON vm_recommendations(org_id, cluster_uuid);

CREATE INDEX IF NOT EXISTS idx_vm_recommendations_namespace
    ON vm_recommendations(org_id, cluster_uuid, namespace);

CREATE INDEX IF NOT EXISTS idx_vm_recommendations_idle
    ON vm_recommendations(org_id, cluster_uuid, is_idle)
    WHERE is_idle = TRUE;

CREATE INDEX IF NOT EXISTS idx_vm_recommendations_oversized
    ON vm_recommendations(org_id, cluster_uuid, is_oversized)
    WHERE is_oversized = TRUE;
