-- #445 slice B: SET NOT NULL on clusters.org_id and keep the covering index
-- from 000191.
--
-- Re-runs the 000191 backfill (idempotent) so rows that gained an rh_accounts
-- match after slice A are stamped before the constraint. Does NOT DELETE
-- leftover NULLs. Orphans whose rh_accounts.org_id is NULL or empty fail
-- VALIDATE / SET NOT NULL.
--
-- PG16 CHECK pattern: ADD CHECK NOT VALID, VALIDATE CONSTRAINT, SET NOT NULL,
-- drop the CHECK. golang-migrate wraps this file in one transaction.
--
-- Directory SQL and alias joins switch to c.org_id in the same slice (binary
-- + migration ship together). Empty-string org_id is allowed by NOT NULL;
-- ingest writers reject "".

UPDATE clusters c
SET org_id = a.org_id
FROM rh_accounts a
WHERE c.tenant_id = a.id
  AND c.org_id IS NULL
  AND a.org_id IS NOT NULL
  AND a.org_id <> '';

ALTER TABLE clusters
    ADD CONSTRAINT clusters_org_id_not_null
    CHECK (org_id IS NOT NULL) NOT VALID;

ALTER TABLE clusters
    VALIDATE CONSTRAINT clusters_org_id_not_null;

ALTER TABLE clusters
    ALTER COLUMN org_id SET NOT NULL;

ALTER TABLE clusters
    DROP CONSTRAINT clusters_org_id_not_null;
