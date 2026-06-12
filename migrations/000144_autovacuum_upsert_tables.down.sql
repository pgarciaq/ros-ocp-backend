ALTER TABLE recommendation_sets RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor, fillfactor);

DO $$
DECLARE
    part regclass;
BEGIN
    FOR part IN
        SELECT inhrelid::regclass
        FROM pg_inherits
        WHERE inhparent = 'container_usage_samples'::regclass
    LOOP
        EXECUTE format(
            'ALTER TABLE %s RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor, fillfactor)',
            part
        );
    END LOOP;
END $$;
