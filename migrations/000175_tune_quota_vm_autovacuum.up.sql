-- Tune autovacuum for UPSERT-heavy tables.
-- These tables receive full-table UPSERTs every reconcile cycle, causing
-- high dead-tuple churn. Lowering vacuum/analyze scale factors triggers
-- cleanup sooner, and fillfactor 85 leaves space for HOT updates.

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'quota_recommendation_sets') THEN
    ALTER TABLE quota_recommendation_sets SET (
        autovacuum_vacuum_scale_factor = 0.05,
        autovacuum_analyze_scale_factor = 0.02,
        fillfactor = 85
    );
  END IF;
END $$;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'cluster_quota_recommendation_sets') THEN
    ALTER TABLE cluster_quota_recommendation_sets SET (
        autovacuum_vacuum_scale_factor = 0.05,
        autovacuum_analyze_scale_factor = 0.02,
        fillfactor = 85
    );
  END IF;
END $$;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'vm_recommendations') THEN
    ALTER TABLE vm_recommendations SET (
        autovacuum_vacuum_scale_factor = 0.05,
        autovacuum_analyze_scale_factor = 0.02,
        fillfactor = 85
    );
  END IF;
END $$;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'node_recommendations') THEN
    ALTER TABLE node_recommendations SET (
        autovacuum_vacuum_scale_factor = 0.05,
        autovacuum_analyze_scale_factor = 0.02,
        fillfactor = 85
    );
  END IF;
END $$;
