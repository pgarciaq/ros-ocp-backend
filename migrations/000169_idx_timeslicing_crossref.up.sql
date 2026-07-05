-- DB-008: Partial index for GPU timeslicing cross-ref lookups.
-- LoadPersistedGPUTimeslicingCrossRefs scans recommendation_sets filtering on
-- has_gpu=true AND time_slicing_node<>''. Without this index, the query does a
-- full sequential scan of the table (millions of rows in SaaS). The partial
-- predicate excludes 99%+ of rows, making the index tiny and writes nearly free.
CREATE INDEX IF NOT EXISTS idx_rec_sets_timeslicing_crossref
    ON recommendation_sets (org_id, cluster_uuid)
    WHERE has_gpu = true AND time_slicing_node <> '';
