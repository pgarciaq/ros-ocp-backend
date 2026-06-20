ALTER TABLE recommendation_sets
    DROP COLUMN IF EXISTS estimated_cpu_savings_cents,
    DROP COLUMN IF EXISTS estimated_memory_savings_cents;

-- Restore NOT NULL DEFAULT 0 for estimated_waste_cents.
ALTER TABLE recommendation_sets
    ALTER COLUMN estimated_waste_cents SET DEFAULT 0;
UPDATE recommendation_sets SET estimated_waste_cents = 0 WHERE estimated_waste_cents IS NULL;
ALTER TABLE recommendation_sets
    ALTER COLUMN estimated_waste_cents SET NOT NULL;
