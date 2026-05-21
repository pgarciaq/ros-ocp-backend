-- Add current request columns to recommendation_sets for sorting (mirrors namespace_recommendation_sets).
-- Existing rows remain NULL; the poller fills values for new records.
-- List queries use ORDER BY ... DESC NULLS LAST (see listoptions.SQLOrderByFragment).
-- NOTE: Originally numbered 000024; renumbered to 000056 to resolve duplicate. Uses IF NOT EXISTS for idempotency.
ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS cpu_request_current NUMERIC(10, 4),
    ADD COLUMN IF NOT EXISTS memory_request_current NUMERIC(20, 4);
