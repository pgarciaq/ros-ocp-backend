-- #827 rollback.
ALTER TABLE hcp_sleep_evidence DROP COLUMN IF EXISTS off_observed_hours;
ALTER TABLE hcp_sleep_evidence DROP COLUMN IF EXISTS bh_observed_hours;
