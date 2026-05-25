ALTER TABLE recommendation_sets
    ALTER COLUMN estimated_monthly_savings_usd TYPE REAL
    USING (COALESCE(estimated_monthly_savings_usd, 0)::REAL / 100.0);

ALTER TABLE recommendation_history
    ALTER COLUMN estimated_monthly_savings_usd TYPE REAL
    USING (COALESCE(estimated_monthly_savings_usd, 0)::REAL / 100.0);

ALTER TABLE node_recommendations
    ALTER COLUMN estimated_monthly_savings_usd TYPE REAL
    USING (COALESCE(estimated_monthly_savings_usd, 0)::REAL / 100.0);

ALTER TABLE pvc_recommendation_sets
    ALTER COLUMN estimated_monthly_savings_usd TYPE REAL
    USING (COALESCE(estimated_monthly_savings_usd, 0)::REAL / 100.0);
