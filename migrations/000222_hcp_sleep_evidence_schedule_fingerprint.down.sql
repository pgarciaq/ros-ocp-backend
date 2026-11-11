-- #831 rollback.
ALTER TABLE hcp_sleep_evidence DROP COLUMN IF EXISTS schedule_fingerprint;
