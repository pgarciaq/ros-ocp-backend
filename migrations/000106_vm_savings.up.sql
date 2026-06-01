ALTER TABLE vm_recommendations
    ADD COLUMN IF NOT EXISTS savings_amount NUMERIC(15, 2),
    ADD COLUMN IF NOT EXISTS savings_currency VARCHAR(3);
