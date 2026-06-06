ALTER TABLE pvc_recommendation_sets
    RENAME COLUMN estimated_savings_cents TO estimated_monthly_savings_usd;

ALTER TABLE node_recommendations
    RENAME COLUMN estimated_savings_cents TO estimated_monthly_savings_usd;

ALTER TABLE recommendation_history
    RENAME COLUMN estimated_savings_cents TO estimated_monthly_savings_usd;

ALTER TABLE recommendation_sets
    RENAME COLUMN estimated_savings_cents TO estimated_monthly_savings_usd;
