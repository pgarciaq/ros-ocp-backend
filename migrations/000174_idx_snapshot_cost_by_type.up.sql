-- Covering index for snapshot cost aggregation by recommendation_type.
-- Avoids heap fetches in GetFleetHeatmap and similar queries that
-- aggregate estimated_cost_cents grouped by org_id + recommendation_type.

CREATE INDEX IF NOT EXISTS idx_snapshot_cost_by_type
    ON snapshot_recommendation_sets (org_id, recommendation_type)
    INCLUDE (estimated_cost_cents);
