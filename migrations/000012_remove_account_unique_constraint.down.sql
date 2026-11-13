-- Downgrade policy (migrations/README.md, "Narrowing down-migrations"):
-- refuse over indeterminate survivors — never delete silently. NULL
-- values do not collide (Postgres UNIQUE treats NULLs as distinct), so
-- the guard counts non-null values only.
DO $$
DECLARE collisions BIGINT;
BEGIN
    SELECT COUNT(*) INTO collisions FROM (
        SELECT 1 FROM rh_accounts
        WHERE account IS NOT NULL
        GROUP BY account
        HAVING COUNT(*) > 1
    ) d;
    IF collisions > 0 THEN
        RAISE EXCEPTION 'downgrade 000012 blocked: % duplicate account value(s) exist; the removed constraint cannot be restored over them. Deduplicate deliberately (decide which row survives); then force the source version (migrate force 12 — a failed downgrade leaves the database dirty at 11, having executed nothing) and re-run', collisions;
    END IF;
END $$;

ALTER TABLE rh_accounts ADD CONSTRAINT rh_accounts_account_key UNIQUE (account);
