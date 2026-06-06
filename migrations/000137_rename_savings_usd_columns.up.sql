-- Rename legacy *_usd column names now that values are stored as integer cents.

ALTER TABLE recommendation_sets
    RENAME COLUMN estimated_monthly_savings_usd TO estimated_savings_cents;

ALTER TABLE recommendation_history
    RENAME COLUMN estimated_monthly_savings_usd TO estimated_savings_cents;

ALTER TABLE node_recommendations
    RENAME COLUMN estimated_monthly_savings_usd TO estimated_savings_cents;

ALTER TABLE pvc_recommendation_sets
    RENAME COLUMN estimated_monthly_savings_usd TO estimated_savings_cents;
