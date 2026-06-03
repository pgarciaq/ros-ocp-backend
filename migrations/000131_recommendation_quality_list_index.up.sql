-- Speed up GET /recommendations/openshift/quality list queries.
-- Typical filters: org_id + engine + measured_at range; default sort: measured_at DESC.
CREATE INDEX IF NOT EXISTS idx_recommendation_quality_org_engine_measured
    ON recommendation_quality (org_id, engine, measured_at DESC);
