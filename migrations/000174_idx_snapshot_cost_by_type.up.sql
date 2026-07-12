-- Advisory: This index uses CREATE INDEX (not CONCURRENTLY) because golang-migrate
-- runs each migration inside a transaction, and CREATE INDEX CONCURRENTLY cannot
-- execute within a transaction block. For large production deployments, consider
-- applying this index manually with CONCURRENTLY before running the migration,
-- then making this migration a no-op.
--
-- Covering index for snapshot cost aggregation by recommendation_type.
-- Avoids heap fetches in GetFleetHeatmap and similar queries that
-- aggregate estimated_cost_cents grouped by org_id + recommendation_type.

CREATE INDEX IF NOT EXISTS idx_snapshot_cost_by_type
    ON snapshot_recommendation_sets (org_id, recommendation_type)
    INCLUDE (estimated_cost_cents);
