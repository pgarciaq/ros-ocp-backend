-- Autovacuum tuning for high-write tables added in phases 14-15.
-- DB-003: UPSERT/UPDATE-heavy tables get fillfactor=85 (enables HOT updates).
-- DB-009: INSERT-only quality tables get autovacuum tuning only (no fillfactor change).
--
-- PostgreSQL does not allow storage parameters on partitioned table parents
-- (they are routing-only, not physical tables). Settings are applied to each
-- existing child partition. Future partitions inherit settings from the parent's
-- reloptions only for non-partitioned tables; for partitioned tables, partition
-- creation code must include the settings (handled by the DO $$ loops here for
-- existing partitions — new partitions created at runtime by the application
-- inherit from the table definition in the CREATE TABLE ... PARTITION OF statement
-- which does not carry reloptions, so the application's EnsurePartition functions
-- should set these explicitly if needed in the future).

-- === DB-003: UPSERT/UPDATE-heavy tables (autovacuum + fillfactor) ===

-- gpu_mig_recommendation_sets: non-partitioned, heavy UPDATEs every reconcile
ALTER TABLE gpu_mig_recommendation_sets SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02,
    fillfactor = 85
);

-- node_gpu_timeslicing_recommendations: non-partitioned, heavy UPDATEs every reconcile
ALTER TABLE node_gpu_timeslicing_recommendations SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02,
    fillfactor = 85
);

-- hourly_node_digests: partitioned — apply to existing child partitions only
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'hourly_node_digests'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.02, fillfactor = 85)',
            part
        );
    END LOOP;
END $$;

-- hourly_vm_digests: partitioned — apply to existing child partitions only
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'hourly_vm_digests'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.02, fillfactor = 85)',
            part
        );
    END LOOP;
END $$;

-- === DB-009: INSERT-only quality tables (autovacuum only, no fillfactor) ===

-- recommendation_quality: partitioned
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'recommendation_quality'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.02)',
            part
        );
    END LOOP;
END $$;

-- pvc_recommendation_quality: partitioned
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'pvc_recommendation_quality'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.02)',
            part
        );
    END LOOP;
END $$;

-- vm_recommendation_quality: partitioned
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'vm_recommendation_quality'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.02)',
            part
        );
    END LOOP;
END $$;

-- gpu_mig_recommendation_quality: partitioned
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'gpu_mig_recommendation_quality'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.02)',
            part
        );
    END LOOP;
END $$;

-- snapshot_recommendation_quality: partitioned
DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'snapshot_recommendation_quality'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.02)',
            part
        );
    END LOOP;
END $$;
