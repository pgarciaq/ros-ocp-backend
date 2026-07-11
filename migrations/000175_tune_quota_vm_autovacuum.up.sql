-- Tune autovacuum for UPSERT-heavy tables.
-- These tables receive full-table UPSERTs every reconcile cycle, causing
-- high dead-tuple churn. Lowering vacuum/analyze scale factors triggers
-- cleanup sooner, and fillfactor 85 leaves space for HOT updates.

ALTER TABLE quota_recommendation_sets SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02,
    fillfactor = 85
);

ALTER TABLE cluster_quota_recommendation_sets SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02,
    fillfactor = 85
);

ALTER TABLE vm_recommendations SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02,
    fillfactor = 85
);
