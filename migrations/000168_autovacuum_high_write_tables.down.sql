-- Revert autovacuum tuning for high-write tables.

ALTER TABLE gpu_mig_recommendation_sets RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor, fillfactor);
ALTER TABLE node_gpu_timeslicing_recommendations RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor, fillfactor);

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
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor, fillfactor)',
            part
        );
    END LOOP;
END $$;

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
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor, fillfactor)',
            part
        );
    END LOOP;
END $$;

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
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor)',
            part
        );
    END LOOP;
END $$;

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
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor)',
            part
        );
    END LOOP;
END $$;

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
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor)',
            part
        );
    END LOOP;
END $$;

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
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor)',
            part
        );
    END LOOP;
END $$;

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
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor)',
            part
        );
    END LOOP;
END $$;
