-- Revert to migration 000168 settings for quality tables.

DO $$
DECLARE
    part regclass;
    parent_table text;
BEGIN
    FOR parent_table IN
        SELECT unnest(ARRAY[
            'recommendation_quality',
            'pvc_recommendation_quality',
            'vm_recommendation_quality',
            'gpu_mig_recommendation_quality',
            'snapshot_recommendation_quality'
        ])
    LOOP
        FOR part IN
            SELECT inhrelid::regclass
            FROM pg_inherits
            WHERE inhparent = parent_table::regclass
        LOOP
            EXECUTE format(
                'ALTER TABLE %s SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.02)',
                part
            );
        END LOOP;
    END LOOP;
END $$;
