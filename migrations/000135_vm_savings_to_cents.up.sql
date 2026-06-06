-- Store VM savings as integer cents (BIGINT) for consistency with other recommendation types.

ALTER TABLE vm_recommendations
    ALTER COLUMN savings_amount TYPE BIGINT
    USING (ROUND(COALESCE(savings_amount, 0) * 100))::BIGINT;

ALTER TABLE vm_recommendations
    RENAME COLUMN savings_amount TO estimated_savings_cents;
