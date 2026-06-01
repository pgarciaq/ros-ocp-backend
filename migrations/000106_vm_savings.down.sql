ALTER TABLE vm_recommendations
    DROP COLUMN IF EXISTS savings_amount,
    DROP COLUMN IF EXISTS savings_currency;
