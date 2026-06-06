ALTER TABLE vm_recommendations
    RENAME COLUMN estimated_savings_cents TO savings_amount;

ALTER TABLE vm_recommendations
    ALTER COLUMN savings_amount TYPE NUMERIC(15, 2)
    USING (COALESCE(savings_amount, 0)::NUMERIC / 100.0);
