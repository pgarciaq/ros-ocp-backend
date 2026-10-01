-- #646 thin correlator advisories rollback.
DROP INDEX IF EXISTS idx_hcp_correlation_advisories_expiry;
DROP TABLE IF EXISTS hcp_correlation_advisories;
