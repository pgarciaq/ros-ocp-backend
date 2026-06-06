-- Store snapshot holding cost as integer cents (BIGINT) for consistency with other recommendation types.

ALTER TABLE snapshot_recommendation_sets
    ALTER COLUMN estimated_monthly_cost_usd TYPE BIGINT
    USING (ROUND(COALESCE(estimated_monthly_cost_usd, 0) * 100))::BIGINT;

ALTER TABLE snapshot_recommendation_sets
    RENAME COLUMN estimated_monthly_cost_usd TO estimated_cost_cents;
