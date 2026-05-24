ALTER TABLE node_recommendations
    ADD COLUMN IF NOT EXISTS estimated_monthly_savings_usd REAL;

ALTER TABLE pvc_recommendation_sets
    ADD COLUMN IF NOT EXISTS estimated_monthly_savings_usd REAL;
