DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'quota_recommendation_sets') THEN
    ALTER TABLE quota_recommendation_sets RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor, fillfactor);
  END IF;
END $$;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'cluster_quota_recommendation_sets') THEN
    ALTER TABLE cluster_quota_recommendation_sets RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor, fillfactor);
  END IF;
END $$;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'vm_recommendations') THEN
    ALTER TABLE vm_recommendations RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor, fillfactor);
  END IF;
END $$;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_class WHERE relname = 'node_recommendations') THEN
    ALTER TABLE node_recommendations RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor, fillfactor);
  END IF;
END $$;
