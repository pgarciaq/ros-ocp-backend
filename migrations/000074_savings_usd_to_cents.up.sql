-- Store monetary savings as integer cents (BIGINT) instead of REAL to avoid float truncation.

ALTER TABLE recommendation_sets
    ALTER COLUMN estimated_monthly_savings_usd TYPE BIGINT
    USING (ROUND(COALESCE(estimated_monthly_savings_usd, 0) * 100)::BIGINT);

ALTER TABLE recommendation_history
    ALTER COLUMN estimated_monthly_savings_usd TYPE BIGINT
    USING (ROUND(COALESCE(estimated_monthly_savings_usd, 0) * 100)::BIGINT);

ALTER TABLE node_recommendations
    ALTER COLUMN estimated_monthly_savings_usd TYPE BIGINT
    USING (ROUND(COALESCE(estimated_monthly_savings_usd, 0) * 100)::BIGINT);

ALTER TABLE pvc_recommendation_sets
    ALTER COLUMN estimated_monthly_savings_usd TYPE BIGINT
    USING (ROUND(COALESCE(estimated_monthly_savings_usd, 0) * 100)::BIGINT);
