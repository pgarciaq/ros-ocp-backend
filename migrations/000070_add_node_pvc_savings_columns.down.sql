ALTER TABLE node_recommendations
    DROP COLUMN IF EXISTS estimated_monthly_savings_usd;

ALTER TABLE pvc_recommendation_sets
    DROP COLUMN IF EXISTS estimated_monthly_savings_usd;
