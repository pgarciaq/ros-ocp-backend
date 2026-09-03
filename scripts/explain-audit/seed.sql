-- Realistic scale seed for EXPLAIN ANALYZE audit.
-- Orgs: org-small (1k containers), org-medium (50k), org-large (200k)

SET client_min_messages TO WARNING;

DO $$
DECLARE
    month_start DATE;
    month_end   DATE;
    part_name   TEXT;
    parent      TEXT;
BEGIN
    FOREACH parent IN ARRAY ARRAY['daily_container_digests', 'daily_namespace_digests', 'daily_node_digests', 'gpu_container_digests', 'recommendation_history', 'recommendation_quality']
    LOOP
        FOR i IN -1..2 LOOP
            month_start := date_trunc('month', DATE '2026-05-01') + (i || ' months')::interval;
            month_end   := month_start + '1 month'::interval;
            part_name   := parent || '_' || to_char(month_start, 'YYYYMM');
            IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
                EXECUTE format(
                    'CREATE TABLE %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                    part_name, parent, month_start, month_end
                );
            END IF;
        END LOOP;
    END LOOP;
END $$;

TRUNCATE TABLE
    recommendation_sets,
    namespace_recommendation_sets,
    node_recommendations,
    pvc_recommendation_sets,
    daily_container_digests,
    daily_namespace_digests,
    daily_node_digests,
    gpu_container_digests,
    snapshot_recommendation_sets,
    snapshot_inventory,
    business_hours_schedules,
    recommendation_history,
    recommendation_quality,
    org_recommendation_stats,
    recommendation_thresholds,
    clusters,
    rh_accounts
RESTART IDENTITY CASCADE;

INSERT INTO rh_accounts (id, org_id, account) VALUES
    (1, 'org-small',  '10001'),
    (2, 'org-medium', '10002'),
    (3, 'org-large',  '10003');

INSERT INTO clusters (tenant_id, source_id, cluster_uuid, cluster_alias, last_reported_at)
SELECT
    ra.id,
    'src-' || ra.org_id || '-' || c.n,
    ('00000000-0000-4000-8000-' || lpad((ra.id * 100 + c.n)::text, 12, '0'))::uuid,
    'cluster-' || c.n,
    TIMESTAMPTZ '2026-05-24 12:00:00+00'
FROM rh_accounts ra
CROSS JOIN generate_series(1, CASE ra.org_id WHEN 'org-small' THEN 1 WHEN 'org-medium' THEN 3 ELSE 5 END) AS c(n);

CREATE TEMP TABLE org_scale (org_id text PRIMARY KEY, containers int, namespaces int, nodes_per_cluster int, pvcs int) ON COMMIT DROP;
INSERT INTO org_scale VALUES
    ('org-small',  1000,   50,  20,  100),
    ('org-medium', 50000,  500, 50,  1000),
    ('org-large',  200000, 2000, 100, 5000);

CREATE TEMP TABLE cluster_map AS
SELECT ra.org_id, c.cluster_uuid, c.id AS cluster_id,
       COUNT(*) OVER (PARTITION BY ra.org_id) AS cluster_count,
       ROW_NUMBER() OVER (PARTITION BY ra.org_id ORDER BY c.id) - 1 AS cluster_idx
FROM clusters c
JOIN rh_accounts ra ON ra.id = c.tenant_id;

-- recommendation_sets: 6 rows per container
INSERT INTO recommendation_sets (
    org_id, cluster_uuid, namespace, workload, workload_type, container_name,
    term, engine, stale,
    rec_cpu_request_millicores, rec_cpu_limit_millicores,
    rec_memory_request_kib, rec_memory_limit_kib,
    current_cpu_request_millicores, current_cpu_limit_millicores,
    current_memory_request_kib, current_memory_limit_kib,
    variation_cpu_request_pct, variation_cpu_limit_pct,
    variation_memory_request_pct, variation_memory_limit_pct,
    confidence_level, notification_codes,
    estimated_savings_cents,
    monitoring_start_time, monitoring_end_time,
    recommendations, updated_at, container_id
)
SELECT
    os.org_id,
    cm.cluster_uuid,
    'ns-' || lpad(((gs.cid - 1) % os.namespaces + 1)::text, 4, '0'),
    'wl-' || lpad(gs.cid::text, 7, '0'),
    (ARRAY['deployment', 'statefulset', 'daemonset', 'deploymentconfig'])[1 + (gs.cid % 4)],
    'main',
    te.term,
    en.engine,
    false,
    100 + (gs.cid % 500),
    200 + (gs.cid % 800),
    131072 + (gs.cid % 524288),
    262144 + (gs.cid % 524288),
    150 + (gs.cid % 500),
    250 + (gs.cid % 800),
    196608 + (gs.cid % 524288),
    393216 + (gs.cid % 524288),
    -10 - (gs.cid % 30),
    -5 - (gs.cid % 20),
    -8 - (gs.cid % 25),
    -4 - (gs.cid % 15),
    0.85,
    ARRAY[1::smallint, 2::smallint],
    (gs.cid % 5000 + 100)::bigint,
    TIMESTAMPTZ '2026-04-01',
    TIMESTAMPTZ '2026-05-24',
    '{}'::jsonb,
    TIMESTAMPTZ '2026-05-24 12:00:00+00' - ((gs.cid % 720) || ' hours')::interval,
    gen_random_uuid()
FROM org_scale os
JOIN generate_series(1, (SELECT MAX(containers) FROM org_scale)) gs(cid) ON gs.cid <= os.containers
JOIN cluster_map cm ON cm.org_id = os.org_id AND (gs.cid - 1) % cm.cluster_count = cm.cluster_idx
CROSS JOIN (VALUES ('short'), ('medium'), ('long')) AS te(term)
CROSS JOIN (VALUES ('cost'), ('performance')) AS en(engine);

-- namespace_recommendation_sets
INSERT INTO namespace_recommendation_sets (
    org_id, cluster_uuid, namespace_name, term, engine, stale,
    rec_cpu_request_millicores, rec_cpu_limit_millicores,
    rec_memory_request_kib, rec_memory_limit_kib,
    current_cpu_request_millicores, current_cpu_limit_millicores,
    current_memory_request_kib, current_memory_limit_kib,
    variation_cpu_request_pct, variation_cpu_limit_pct,
    variation_memory_request_pct, variation_memory_limit_pct,
    confidence_level, notification_codes, updated_at,
    monitoring_start_time, monitoring_end_time, recommendations
)
SELECT
    os.org_id,
    cm.cluster_uuid,
    'ns-' || lpad(ns.n::text, 4, '0'),
    te.term,
    en.engine,
    false,
    4000, 8000, 8388608, 16777216,
    5000, 10000, 10485760, 20971520,
    -15, -10, -12, -8,
    0.9,
    ARRAY[1::smallint],
    TIMESTAMPTZ '2026-05-24 12:00:00+00',
    TIMESTAMPTZ '2026-04-01',
    TIMESTAMPTZ '2026-05-24',
    '{}'::jsonb
FROM org_scale os
JOIN generate_series(1, (SELECT MAX(namespaces) FROM org_scale)) ns(n) ON ns.n <= os.namespaces
JOIN cluster_map cm ON cm.org_id = os.org_id AND (ns.n - 1) % cm.cluster_count = cm.cluster_idx
CROSS JOIN (VALUES ('short'), ('medium'), ('long')) AS te(term)
CROSS JOIN (VALUES ('cost'), ('performance')) AS en(engine);

-- node_recommendations
INSERT INTO node_recommendations (
    org_id, cluster_uuid, node, term, engine,
    cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
    cpu_overcommit_ratio, category,
    pod_count, trend_slope, notification_codes,
    estimated_savings_cents, updated_at
)
SELECT
    ra.org_id,
    c.cluster_uuid,
    'node-' || lpad(n.n::text, 3, '0'),
    te.term,
    en.engine,
    0.25 + (n.n % 50) / 100.0,
    0.45 + (n.n % 40) / 100.0,
    0.30 + (n.n % 45) / 100.0,
    0.55 + (n.n % 35) / 100.0,
    1.2,
    CASE
      WHEN n.n % 5 = 0 THEN 'overcommitted'
      WHEN n.n % 3 = 0 THEN 'underutilized'
      ELSE 'optimized'
    END,
    20 + (n.n % 80),
    -0.01 + (n.n % 10) * 0.001,
    ARRAY[1::smallint],
    (5000 + n.n * 10)::bigint,
    TIMESTAMPTZ '2026-05-24 12:00:00+00'
FROM rh_accounts ra
JOIN org_scale os ON os.org_id = ra.org_id
JOIN clusters c ON c.tenant_id = ra.id
JOIN generate_series(1, (SELECT MAX(nodes_per_cluster) FROM org_scale)) n(n) ON n.n <= os.nodes_per_cluster
CROSS JOIN (VALUES ('short'), ('medium'), ('long')) AS te(term)
CROSS JOIN (VALUES ('cost'), ('performance')) AS en(engine);

-- pvc_recommendation_sets
INSERT INTO pvc_recommendation_sets (
    org_id, cluster_uuid, namespace, persistentvolumeclaim, persistentvolume, storageclass,
    capacity_bytes, usage_bytes_max, usage_ratio, recommendation_type,
    recommended_bytes, days_to_full, growth_bytes_per_day,
    notification_codes, data_days, term, estimated_savings_cents, updated_at
)
SELECT
    os.org_id,
    cm.cluster_uuid,
    'ns-' || lpad(((p.p - 1) % os.namespaces + 1)::text, 4, '0'),
    'pvc-' || lpad(p.p::text, 5, '0'),
    'pv-' || p.p::text,
    'gp3',
    (100 + (p.p % 900))::bigint * 1073741824,
    (10 + (p.p % 80))::bigint * 1073741824,
    0.1 + (p.p % 80) / 100.0,
    CASE WHEN p.p % 2 = 0 THEN 'rightsize' ELSE 'monitor' END,
    (50 + (p.p % 400))::bigint * 1073741824,
    30 + (p.p % 60),
    (1024 * 1024 * (1 + p.p % 100))::bigint,
    ARRAY[1::smallint],
    15,
    'medium',
    (2000 + p.p)::bigint,
    TIMESTAMPTZ '2026-05-24 12:00:00+00'
FROM org_scale os
JOIN generate_series(1, (SELECT MAX(pvcs) FROM org_scale)) p(p) ON p.p <= os.pvcs
JOIN cluster_map cm ON cm.org_id = os.org_id AND (p.p - 1) % cm.cluster_count = cm.cluster_idx;

-- daily_container_digests (15 days, both schedule types)
INSERT INTO daily_container_digests (
    bucket_date, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
    schedule_type,
    cpu_request_p50_mc, cpu_request_p95_mc,
    cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_max_mc,
    cpu_throttle_p95_mc, cpu_throttle_max_mc,
    memory_request_p50_kib, memory_request_p95_kib,
    memory_usage_p50_kib, memory_usage_p95_kib, memory_usage_max_kib,
    memory_rss_p95_kib, memory_rss_max_kib,
    oom_count_sum, cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
)
SELECT
    d.dt,
    rs.org_id,
    rs.cluster_uuid,
    rs.namespace,
    rs.workload,
    rs.workload_type,
    rs.container_name,
    st.schedule_type::digest_schedule_type,
    rs.rec_cpu_request_millicores,
    rs.rec_cpu_limit_millicores,
    rs.rec_cpu_request_millicores - 20,
    rs.rec_cpu_request_millicores + 50,
    rs.rec_cpu_limit_millicores,
    5, 10,
    rs.rec_memory_request_kib,
    rs.rec_memory_limit_kib,
    rs.rec_memory_request_kib - 1024,
    rs.rec_memory_request_kib + 4096,
    rs.rec_memory_limit_kib,
    rs.rec_memory_request_kib,
    rs.rec_memory_limit_kib,
    0,
    rs.rec_cpu_request_millicores,
    rs.rec_memory_request_kib,
    96
FROM recommendation_sets rs
CROSS JOIN generate_series(DATE '2026-05-10', DATE '2026-05-24', '1 day') d(dt)
CROSS JOIN (VALUES ('all_hours'), ('business_hours')) st(schedule_type)
WHERE rs.term = 'medium' AND rs.engine = 'cost';

-- daily_namespace_digests
INSERT INTO daily_namespace_digests (
    bucket_date, org_id, cluster_uuid, namespace, schedule_type,
    cpu_request_p50_mc, cpu_request_p95_mc, cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_max_mc,
    memory_request_p50_kib, memory_request_p95_kib,
    memory_usage_p50_kib, memory_usage_p95_kib, memory_usage_max_kib,
    cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
)
SELECT
    d.dt,
    ns.org_id,
    ns.cluster_uuid,
    ns.namespace_name,
    st.schedule_type::digest_schedule_type,
    8000, 16000, 4000, 12000, 20000,
    16777216, 33554432, 8388608, 25165824, 41943040,
    5000, 10485760, 500
FROM namespace_recommendation_sets ns
CROSS JOIN generate_series(DATE '2026-05-10', DATE '2026-05-24', '1 day') d(dt)
CROSS JOIN (VALUES ('all_hours'), ('business_hours')) st(schedule_type)
WHERE ns.term = 'medium' AND ns.engine = 'cost';

-- recommendation_history (30 days, 1% sample of org-large containers)
INSERT INTO recommendation_history (
    recorded_at, org_id, cluster_uuid, namespace, workload, workload_type,
    container_name, term, engine,
    rec_cpu_request_millicores, rec_cpu_limit_millicores,
    rec_memory_request_kib, rec_memory_limit_kib,
    notification_codes, confidence_level, estimated_savings_cents, source_binary
)
SELECT
    (d.dt + TIME '12:00') AT TIME ZONE 'UTC',
    rs.org_id,
    rs.cluster_uuid,
    rs.namespace,
    rs.workload,
    rs.workload_type,
    rs.container_name,
    rs.term,
    rs.engine,
    rs.rec_cpu_request_millicores,
    rs.rec_cpu_limit_millicores,
    rs.rec_memory_request_kib,
    rs.rec_memory_limit_kib,
    rs.notification_codes,
    rs.confidence_level,
    rs.estimated_savings_cents,
    'native'
FROM recommendation_sets rs
CROSS JOIN generate_series(DATE '2026-04-25', DATE '2026-05-24', '1 day') d(dt)
WHERE rs.org_id = 'org-large'
  AND (abs(hashtext(rs.namespace || rs.workload || rs.container_name)) % 100) = 0;

INSERT INTO recommendation_quality (
    measured_at, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
    oom_events_after_rec, stability_pct, adoption_detected, recommendation_age_hours
)
SELECT
    TIMESTAMPTZ '2026-05-24 12:00:00+00',
    rs.org_id,
    rs.cluster_uuid,
    rs.namespace,
    rs.workload,
    rs.workload_type,
    rs.container_name,
    (abs(hashtext(rs.workload)) % 5),
    0.95,
    (abs(hashtext(rs.container_name)) % 10 = 0),
    168
FROM recommendation_sets rs
WHERE rs.org_id = 'org-large' AND rs.term = 'medium' AND rs.engine = 'cost'
  AND (abs(hashtext(rs.namespace || rs.workload)) % 50) = 0;

INSERT INTO recommendation_thresholds (org_id, recommendation_type, thresholds) VALUES
    ('org-large', 'container', '{"cpu_cost_percentile": 85, "mem_cost_percentile": 90}'::jsonb),
    ('org-large', 'namespace', '{"cpu_cost_percentile": 80}'::jsonb),
    ('org-medium', 'container', '{"cpu_cost_percentile": 75}'::jsonb);

INSERT INTO org_recommendation_stats (org_id, container_count, namespace_count, updated_at)
SELECT
    org_id,
    COUNT(DISTINCT (namespace, workload, container_name)),
    COUNT(DISTINCT namespace),
    NOW()
FROM recommendation_sets
WHERE stale = false
GROUP BY org_id;

-- GPU workloads: ~2.5%% of containers per org, 15 days of hourly-ish digests on GPU nodes
CREATE TEMP TABLE gpu_containers AS
SELECT DISTINCT ON (rs.org_id, rs.cluster_uuid, rs.namespace, rs.workload, rs.container_name)
    rs.org_id,
    rs.cluster_uuid,
    rs.namespace,
    rs.workload,
    rs.workload_type,
    rs.container_name,
    ('gpu-node-' || lpad(((abs(hashtext(rs.namespace || rs.workload)) % 40) + 1)::text, 3, '0')) AS node_name,
    CASE WHEN abs(hashtext(rs.container_name)) % 3 = 0
        THEN 'NVIDIA A100-SXM4-40GB'
        ELSE 'NVIDIA T4'
    END AS gpu_model,
    CASE WHEN abs(hashtext(rs.workload)) % 4 = 0 THEN '1g.5gb'
         WHEN abs(hashtext(rs.workload)) % 4 = 1 THEN '2g.10gb'
         WHEN abs(hashtext(rs.workload)) % 4 = 2 THEN '3g.20gb'
         ELSE 'full_gpu'
    END AS gpu_profile
FROM recommendation_sets rs
JOIN org_scale os ON os.org_id = rs.org_id
WHERE rs.term = 'medium' AND rs.engine = 'cost'
  AND (abs(hashtext(rs.namespace || rs.workload || rs.container_name)) % 40) = 0;

INSERT INTO gpu_container_digests (
    interval_start, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
    gpu_model_name, gpu_profile_name, node_name,
    fb_usage_min_mib, fb_usage_max_mib, fb_usage_avg_mib,
    tensor_pipe_active_min, tensor_pipe_active_max, tensor_pipe_active_avg,
    dram_active_min, dram_active_max, dram_active_avg,
    sm_active_min, sm_active_max, sm_active_avg
)
SELECT
    (d.dt + TIME '12:00') AT TIME ZONE 'UTC',
    gc.org_id,
    gc.cluster_uuid,
    gc.namespace,
    gc.workload,
    gc.workload_type,
    gc.container_name,
    gc.gpu_model,
    gc.gpu_profile,
    gc.node_name,
    512 + (abs(hashtext(gc.container_name || d.dt::text)) % 2048),
    1024 + (abs(hashtext(gc.workload || d.dt::text)) % 4096),
    768 + (abs(hashtext(gc.namespace || d.dt::text)) % 3072),
    0.05 + (abs(hashtext(gc.container_name)) % 20) / 100.0,
    0.15 + (abs(hashtext(gc.workload)) % 25) / 100.0,
    0.10 + (abs(hashtext(gc.namespace)) % 15) / 100.0,
    0.08 + (abs(hashtext(gc.container_name || 'd')) % 12) / 100.0,
    0.20 + (abs(hashtext(gc.workload || 'd')) % 18) / 100.0,
    0.12 + (abs(hashtext(gc.namespace || 'd')) % 10) / 100.0,
    0.04 + (abs(hashtext(gc.container_name || 's')) % 15) / 100.0,
    0.18 + (abs(hashtext(gc.workload || 's')) % 20) / 100.0,
    0.09 + (abs(hashtext(gc.namespace || 's')) % 12) / 100.0
FROM gpu_containers gc
CROSS JOIN generate_series(DATE '2026-05-10', DATE '2026-05-24', '1 day') d(dt);

UPDATE recommendation_sets rs
SET has_gpu = TRUE,
    gpu_model_name = gc.gpu_model,
    gpu_classification = CASE
        WHEN gc.gpu_profile = 'full_gpu' THEN 'underutilized'
        ELSE 'mig_candidate'
    END
FROM gpu_containers gc
WHERE rs.org_id = gc.org_id
  AND rs.cluster_uuid = gc.cluster_uuid
  AND rs.namespace = gc.namespace
  AND rs.workload = gc.workload
  AND rs.container_name = gc.container_name;

-- Snapshot recommendations and matching fresh inventory
INSERT INTO snapshot_recommendation_sets (
    org_id, cluster_uuid, namespace, snapshot_name, source_pvc_name,
    volume_snapshot_class, storageclass, creation_timestamp,
    restore_size_bytes, age_days, source_pvc_exists, restored_pvc_count,
    managed_by, recommendation_type, estimated_cost_cents, notification_codes, updated_at
)
SELECT
    os.org_id,
    cm.cluster_uuid,
    'ns-' || lpad(((s.n - 1) % os.namespaces + 1)::text, 4, '0'),
    'snap-' || lpad(s.n::text, 6, '0'),
    'pvc-' || lpad(((s.n - 1) % 500 + 1)::text, 5, '0'),
    'csi-snapclass',
    'gp3',
    TIMESTAMPTZ '2026-05-24 12:00:00+00' - ((s.n % 180) || ' days')::interval,
    (10 + (s.n % 500))::bigint * 1073741824,
    1 + (s.n % 180),
    (s.n % 5 != 0),
    (s.n % 7),
    CASE WHEN s.n % 10 = 0 THEN 'Velero' ELSE '' END,
    (ARRAY['active', 'stale', 'orphaned', 'redundant', 'never_restored'])[1 + (s.n % 5)],
    ROUND(0.05 * (10 + (s.n % 500)) * 100)::bigint,
    ARRAY[(31 + (s.n % 5))::smallint],
    TIMESTAMPTZ '2026-05-24 12:00:00+00'
FROM org_scale os
JOIN generate_series(1, CASE os.org_id WHEN 'org-small' THEN 200 WHEN 'org-medium' THEN 2000 ELSE 10000 END) s(n) ON TRUE
JOIN cluster_map cm ON cm.org_id = os.org_id AND (s.n - 1) % cm.cluster_count = cm.cluster_idx;

INSERT INTO snapshot_inventory (
    ingested_at, org_id, cluster_uuid, namespace, snapshot_name,
    source_pvc_name, volume_snapshot_class, storageclass,
    creation_timestamp, restore_size_bytes, source_pvc_exists, restored_pvc_count, labels
)
SELECT
    TIMESTAMPTZ '2026-05-24 11:00:00+00',
    srs.org_id,
    srs.cluster_uuid,
    srs.namespace,
    srs.snapshot_name,
    srs.source_pvc_name,
    srs.volume_snapshot_class,
    srs.storageclass,
    srs.creation_timestamp,
    srs.restore_size_bytes,
    srs.source_pvc_exists,
    srs.restored_pvc_count,
    '{}'::jsonb
FROM snapshot_recommendation_sets srs
WHERE srs.org_id = 'org-large'
  AND (abs(hashtext(srs.snapshot_name)) % 20) != 0;

-- Business hours schedules (org default + per-cluster overrides for org-large)
INSERT INTO business_hours_schedules (
    org_id, cluster_uuid, namespace, enabled, timezone,
    start_time, end_time, days, off_hours_weight, updated_at
)
SELECT
    'org-large',
    '00000000-0000-0000-0000-000000000000'::uuid,
    '',
    TRUE,
    'America/New_York',
    TIME '09:00',
    TIME '17:00',
    ARRAY['monday', 'tuesday', 'wednesday', 'thursday', 'friday'],
    0.0,
    TIMESTAMPTZ '2026-05-24 12:00:00+00'
UNION ALL
SELECT
    'org-large',
    cm.cluster_uuid,
    '',
    TRUE,
    'America/New_York',
    TIME '08:00',
    TIME '18:00',
    ARRAY['monday', 'tuesday', 'wednesday', 'thursday', 'friday'],
    0.0,
    TIMESTAMPTZ '2026-05-24 12:00:00+00'
FROM cluster_map cm
WHERE cm.org_id = 'org-large';

ANALYZE recommendation_sets;
ANALYZE namespace_recommendation_sets;
ANALYZE node_recommendations;
ANALYZE pvc_recommendation_sets;
ANALYZE daily_container_digests;
ANALYZE daily_namespace_digests;
ANALYZE gpu_container_digests;
ANALYZE snapshot_recommendation_sets;
ANALYZE snapshot_inventory;
ANALYZE recommendation_history;
ANALYZE org_recommendation_stats;
