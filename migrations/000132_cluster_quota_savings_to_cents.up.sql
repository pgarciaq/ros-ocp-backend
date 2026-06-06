-- Store cluster-quota estimated savings as integer cents (BIGINT) for consistency with other recommendation types.

ALTER TABLE cluster_quota_recommendation_sets
    ALTER COLUMN savings_dollars_monthly TYPE BIGINT
    USING (COALESCE(savings_dollars_monthly, 0) * 100);

ALTER TABLE cluster_quota_recommendation_sets
    RENAME COLUMN savings_dollars_monthly TO estimated_savings_cents;
