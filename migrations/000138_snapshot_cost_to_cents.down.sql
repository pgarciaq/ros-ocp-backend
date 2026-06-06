ALTER TABLE snapshot_recommendation_sets
    RENAME COLUMN estimated_cost_cents TO estimated_monthly_cost_usd;

ALTER TABLE snapshot_recommendation_sets
    ALTER COLUMN estimated_monthly_cost_usd TYPE REAL
    USING (COALESCE(estimated_monthly_cost_usd, 0)::REAL / 100.0);
