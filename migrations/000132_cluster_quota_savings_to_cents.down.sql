ALTER TABLE cluster_quota_recommendation_sets
    RENAME COLUMN estimated_savings_cents TO savings_dollars_monthly;

ALTER TABLE cluster_quota_recommendation_sets
    ALTER COLUMN savings_dollars_monthly TYPE INT
    USING (COALESCE(savings_dollars_monthly, 0) / 100);
