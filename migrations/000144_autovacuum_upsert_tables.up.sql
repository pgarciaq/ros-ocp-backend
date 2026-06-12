-- Per-table autovacuum tuning for high-churn UPSERT tables.
-- recommendation_sets is a regular heap table; container_usage_samples is partitioned
-- and reloptions must be applied per child partition (not the parent).
-- node_recommendations, namespace_recommendation_sets, and pvc_recommendation_sets
-- have lower write volume and are not tuned here.

ALTER TABLE recommendation_sets SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02,
    fillfactor = 85
);

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
            'ALTER TABLE %s SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.02, fillfactor = 85)',
            part
        );
    END LOOP;
END $$;
